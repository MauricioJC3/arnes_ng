// Package agent runs the inner loop: it feeds history to the provider, executes
// the tools the model asks for (behind the approval gateway), and repeats until
// the model produces a final answer or the step budget runs out.
package agent

import (
	"context"
	"fmt"

	"github.com/andresmjimenez/arnes/internal/approval"
	"github.com/andresmjimenez/arnes/internal/compact"
	"github.com/andresmjimenez/arnes/internal/provider"
	"github.com/andresmjimenez/arnes/internal/tool"
)

// Agent owns one conversation: its provider, tools, approver, and history.
type Agent struct {
	provider  provider.Provider
	tools     *tool.Registry
	approver  approval.Approver
	system    string
	maxTokens int
	maxSteps  int
	history   []provider.Message

	compactor        compact.Strategy
	compactThreshold int         // tokens; 0 disables auto-compaction
	warn             func(error) // non-fatal notifications

	stream  bool
	onDelta func(string) // text deltas when streaming
}

// Option configures an Agent at construction.
type Option func(*Agent)

// WithSystem sets the system prompt.
func WithSystem(s string) Option { return func(a *Agent) { a.system = s } }

// WithMaxTokens caps output tokens per model call.
func WithMaxTokens(n int) Option { return func(a *Agent) { a.maxTokens = n } }

// WithMaxSteps bounds how many tool round-trips one Run may take before it
// gives up. This is the guard against a model that never stops asking for tools.
func WithMaxSteps(n int) Option { return func(a *Agent) { a.maxSteps = n } }

// WithHistory seeds the conversation with prior messages, for session resume.
// The slice is copied, so the caller keeps ownership of theirs.
func WithHistory(h []provider.Message) Option {
	return func(a *Agent) { a.history = append([]provider.Message(nil), h...) }
}

// WithCompactor sets the context-compaction strategy (default compact.None).
func WithCompactor(c compact.Strategy) Option { return func(a *Agent) { a.compactor = c } }

// WithCompactThreshold turns on auto-compaction: before each turn, if the
// estimated history size exceeds n tokens, the compactor runs. 0 disables it.
func WithCompactThreshold(n int) Option { return func(a *Agent) { a.compactThreshold = n } }

// WithWarnFn registers a sink for non-fatal notifications (e.g. a compaction
// that failed and was skipped).
func WithWarnFn(f func(error)) Option { return func(a *Agent) { a.warn = f } }

// WithStreaming turns on streaming when the provider implements provider.Streamer.
func WithStreaming(on bool) Option { return func(a *Agent) { a.stream = on } }

// WithDeltaFn registers the sink for streamed text deltas.
func WithDeltaFn(f func(string)) Option { return func(a *Agent) { a.onDelta = f } }

// New builds an Agent. provider, tools and approver are required; options tune
// the rest. Defaults: 4096 max tokens, 20 steps, no compaction.
func New(p provider.Provider, tools *tool.Registry, ap approval.Approver, opts ...Option) *Agent {
	a := &Agent{
		provider:  p,
		tools:     tools,
		approver:  ap,
		maxTokens: 4096,
		maxSteps:  20,
		compactor: compact.None{},
		warn:      func(error) {},
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.compactor == nil {
		a.compactor = compact.None{}
	}
	if a.warn == nil {
		a.warn = func(error) {}
	}
	return a
}

// History returns the running message log (for tests, debug view, persistence).
func (a *Agent) History() []provider.Message { return a.history }

// CompactorName reports the active compaction strategy.
func (a *Agent) CompactorName() string { return a.compactor.Name() }

// SetCompactor swaps the compaction strategy at runtime (the /compact command).
func (a *Agent) SetCompactor(c compact.Strategy) {
	if c != nil {
		a.compactor = c
	}
}

// Compact runs the current strategy now, regardless of the threshold, and
// returns the estimated token size before and after.
func (a *Agent) Compact(ctx context.Context) (before, after int, err error) {
	before = compact.EstimateTokens(a.history)
	compacted, err := a.compactor.Compact(ctx, a.history)
	if err != nil {
		return before, before, err
	}
	a.history = compacted
	return before, compact.EstimateTokens(a.history), nil
}

// maybeCompact runs auto-compaction between turns when the history is over the
// threshold. A compaction failure is reported via warn and otherwise ignored --
// compaction is an optimization, never a correctness requirement.
func (a *Agent) maybeCompact(ctx context.Context) {
	if a.compactThreshold <= 0 {
		return
	}
	if compact.EstimateTokens(a.history) <= a.compactThreshold {
		return
	}
	compacted, err := a.compactor.Compact(ctx, a.history)
	if err != nil {
		a.warn(fmt.Errorf("compactación (%s) falló, sigo con el historial completo: %w", a.compactor.Name(), err))
		return
	}
	a.history = compacted
}

// Run appends the user input to the history and drives the inner loop until the
// model returns a final answer. It returns that final text.
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	a.maybeCompact(ctx)
	a.history = append(a.history, provider.Message{Role: provider.RoleUser, Text: userInput})

	for step := 0; step < a.maxSteps; step++ {
		req := provider.Request{
			System:    a.system,
			Messages:  a.history,
			Tools:     a.toolDefs(),
			MaxTokens: a.maxTokens,
		}

		var resp provider.Response
		var err error
		if s, ok := a.provider.(provider.Streamer); ok && a.stream {
			resp, err = s.StreamMessage(ctx, req, a.onDelta)
		} else {
			resp, err = a.provider.SendMessage(ctx, req)
		}
		if err != nil {
			return "", fmt.Errorf("provider: %w", err)
		}

		a.history = append(a.history, provider.Message{
			Role:      provider.RoleAssistant,
			Text:      resp.Text,
			ToolCalls: resp.ToolCalls,
		})

		if resp.StopReason != provider.StopToolUse || len(resp.ToolCalls) == 0 {
			return resp.Text, nil
		}

		results := make([]provider.ToolResult, 0, len(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			results = append(results, a.runTool(ctx, call))
		}
		a.history = append(a.history, provider.Message{Role: provider.RoleUser, ToolResults: results})
	}

	return "", fmt.Errorf("se alcanzó el límite de %d pasos sin respuesta final", a.maxSteps)
}

// runTool applies the approval gateway, then executes the tool. It always
// returns a ToolResult: a refusal or an execution error becomes an IsError
// result fed back to the model, never a hard failure of the loop.
func (a *Agent) runTool(ctx context.Context, call provider.ToolCall) provider.ToolResult {
	if !a.approver.Confirm(call) {
		return provider.ToolResult{CallID: call.ID, IsError: true,
			Content: "El usuario denegó la ejecución de esta herramienta."}
	}

	t, ok := a.tools.Get(call.Name)
	if !ok {
		return provider.ToolResult{CallID: call.ID, IsError: true,
			Content: fmt.Sprintf("No existe una herramienta llamada %q.", call.Name)}
	}

	out, err := t.Execute(ctx, call.Input)
	if err != nil {
		return provider.ToolResult{CallID: call.ID, IsError: true,
			Content: fmt.Sprintf("La herramienta %q falló: %v", call.Name, err)}
	}
	return provider.ToolResult{CallID: call.ID, Content: out}
}

// toolDefs projects the registry into the provider-facing tool definitions.
func (a *Agent) toolDefs() []provider.ToolDef {
	tools := a.tools.All()
	defs := make([]provider.ToolDef, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return defs
}
