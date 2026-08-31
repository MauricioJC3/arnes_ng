// Package agent runs the inner loop: it feeds history to the provider, executes
// the tools the model asks for (behind the approval gateway), and repeats until
// the model produces a final answer or the step budget runs out.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/compact"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
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
	compactThreshold int                     // tokens; 0 disables auto-compaction
	warn             func(error)             // non-fatal notifications
	hooks            Hooks                   // pre/post tool-call hooks; nil disables them
	observe          func(provider.ToolCall) // passive pre-execute observer; nil disables it

	stream  bool
	onDelta func(string) // text deltas when streaming

	providerRetries int // extra attempts on a transient provider failure; 0 = no retry

	maxToolResult int // byte cap on a single tool result fed back to the model; 0 disables
	contextGuard  int // token ceiling that forces an emergency compaction mid-turn; 0 disables

	// completion gate: a turn cannot end until these pass. See completionGate.
	verifier    func(context.Context) (output string, ok bool) // project check (build/test/lint); nil disables it
	anchorFn    func() string                                  // dynamic system-prompt suffix (original task + live plan); nil disables it
	openTodosFn func() []string                                // labels of unfinished checklist items; nil disables the nudge
	dirty       bool                                           // a mutating tool ran since the last passing verification

	usedIn, usedOut int // cumulative token usage for this agent
}

// Hooks runs user-configured commands around every tool call. A nil Hooks (the
// default) is a no-op.
type Hooks interface {
	// PreTool runs before the tool executes. A non-nil error cancels the call;
	// the error text is fed back to the model as the tool result.
	PreTool(ctx context.Context, call provider.ToolCall) error
	// PostTool runs after the tool executed, with its output and whether it
	// errored. A non-empty return is appended to the tool result as a note.
	PostTool(ctx context.Context, call provider.ToolCall, result string, isErr bool) string
	// Stop runs once the turn's final message is ready and the completion gate
	// has passed. A non-empty return is fed back to the model as a note and the
	// turn takes one more round; it fires at most once per turn.
	Stop(ctx context.Context) string
}

// Option configures an Agent at construction.
type Option func(*Agent)

// WithSystem sets the system prompt.
func WithSystem(s string) Option { return func(a *Agent) { a.system = s } }

// DefaultMaxTokens is the per-call output-token cap when the caller sets none.
// It is generous on purpose: a model cut off mid write_file on a large file
// loops trying to finish it. Raise it with WithMaxTokens (ARNES_MAX_TOKENS /
// "max_tokens" in the config).
const DefaultMaxTokens = 8192

// WithMaxTokens caps output tokens per model call. n <= 0 keeps the default.
func WithMaxTokens(n int) Option {
	return func(a *Agent) {
		if n > 0 {
			a.maxTokens = n
		}
	}
}

// DefaultMaxSteps is the tool round-trip budget for one Run when the caller sets
// none. It is a circuit breaker against a model that never stops asking for
// tools -- not a cap on useful work -- so it is generous; raise it with
// WithMaxSteps (ARNES_MAX_STEPS / "max_steps" in the config).
const DefaultMaxSteps = 50

// WithMaxSteps bounds how many tool round-trips one Run may take before it
// gives up. This is the guard against a model that never stops asking for tools.
func WithMaxSteps(n int) Option { return func(a *Agent) { a.maxSteps = n } }

// DefaultMaxToolResult is the byte cap on a single tool result before the middle
// is elided. One uncapped read_file or bash (a big generated file, a verbose
// build log) can drop hundreds of KB into an immutable history message and, a
// few of those in, blow past the model's context window -- which no sliding
// compaction can undo because the giant message is recent. ~200 KB is ~50k
// tokens: plenty for a real file, far short of an overflow.
const DefaultMaxToolResult = 200_000

// WithMaxToolResult sets the byte cap on a single tool result (ARNES_MAX_TOOL_OUTPUT).
// n <= 0 disables the cap.
func WithMaxToolResult(n int) Option { return func(a *Agent) { a.maxToolResult = n } }

// DefaultContextGuard is the estimated-token ceiling that trips an emergency
// compaction inside the loop, regardless of the auto-compaction threshold (or
// whether auto-compaction is on at all). It sits above the default 120k
// compaction threshold and below a 200k-context model's limit, so a turn that
// balloons mid-flight gets shrunk -- or fails cleanly with the history saved --
// instead of every subsequent provider call returning HTTP 400.
const DefaultContextGuard = 150_000

// WithContextGuard sets the emergency token ceiling (ARNES_CONTEXT_GUARD).
// n <= 0 disables it.
func WithContextGuard(n int) Option { return func(a *Agent) { a.contextGuard = n } }

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

// WithHooks registers pre/post tool-call hooks.
func WithHooks(h Hooks) Option { return func(a *Agent) { a.hooks = h } }

// WithToolObserver registers fn, called with every approved tool call right
// before it executes (after any pre-tool hook). It cannot block the call; it is
// for passive bookkeeping such as checkpoint file snapshots.
func WithToolObserver(fn func(provider.ToolCall)) Option {
	return func(a *Agent) { a.observe = fn }
}

// WithStreaming turns on streaming when the provider implements provider.Streamer.
func WithStreaming(on bool) Option { return func(a *Agent) { a.stream = on } }

// DefaultProviderRetries is how many extra times a model call is re-issued when
// it fails with a transient error (HTTP 429/5xx, a dropped or stalled stream)
// before the turn gives up. The provider adapters already retry at the
// transport level; this is the outer net that also rescues a mid-stream drop,
// which the transport cannot resume but the loop can safely re-request.
const DefaultProviderRetries = 3

// WithProviderRetries sets how many extra attempts a transient provider failure
// gets. n <= 0 disables loop-level retry (a single attempt).
func WithProviderRetries(n int) Option {
	return func(a *Agent) {
		if n < 0 {
			n = 0
		}
		a.providerRetries = n
	}
}

// providerRetry* tune the loop-level backoff; package vars so tests shrink them.
var (
	providerRetryBase = 1 * time.Second
	providerRetryCap  = 15 * time.Second
)

// WithDeltaFn registers the sink for streamed text deltas.
func WithDeltaFn(f func(string)) Option { return func(a *Agent) { a.onDelta = f } }

// WithUsage seeds the cumulative token counters, so a rebuilt agent keeps the
// session's running total.
func WithUsage(in, out int) Option {
	return func(a *Agent) { a.usedIn, a.usedOut = in, out }
}

// WithVerifier registers the project check the completion gate runs before a
// turn is allowed to end, once a mutating tool has run. It returns the check's
// output and whether it passed; on a failure the output is fed back to the model
// and the turn continues. nil (the default) disables the gate.
func WithVerifier(fn func(context.Context) (output string, ok bool)) Option {
	return func(a *Agent) { a.verifier = fn }
}

// WithAnchorFn registers a function whose return value is appended to the system
// prompt on every model call -- used to keep the original task and the live plan
// in view even after compaction has dropped them from the history. nil disables it.
func WithAnchorFn(fn func() string) Option {
	return func(a *Agent) { a.anchorFn = fn }
}

// WithOpenTodosFn registers a function returning the labels of checklist items
// that are not done. When a turn tries to end with any still open, the gate
// nudges the model once to finish or account for them. nil disables the nudge.
func WithOpenTodosFn(fn func() []string) Option {
	return func(a *Agent) { a.openTodosFn = fn }
}

// New builds an Agent. provider, tools and approver are required; options tune
// the rest. Defaults: DefaultMaxTokens output, DefaultMaxSteps steps, no
// compaction.
func New(p provider.Provider, tools *tool.Registry, ap approval.Approver, opts ...Option) *Agent {
	a := &Agent{
		provider:        p,
		tools:           tools,
		approver:        ap,
		maxTokens:       DefaultMaxTokens,
		maxSteps:        DefaultMaxSteps,
		maxToolResult:   DefaultMaxToolResult,
		contextGuard:    DefaultContextGuard,
		providerRetries: DefaultProviderRetries,
		compactor:       compact.None{},
		warn:            func(error) {},
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

// Usage returns the cumulative input/output token counts across every provider
// call this agent has made.
func (a *Agent) Usage() (in, out int) { return a.usedIn, a.usedOut }

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

// guardContext is the emergency valve against a turn that overflows the model's
// context window mid-flight (many steps, big tool results): every provider call
// after the overflow would just return HTTP 400. When the estimated history
// exceeds contextGuard it compacts NOW -- with the configured strategy, or a
// sliding window when none is set -- ignoring the normal auto-compaction
// threshold. If that still isn't enough (a single oversized message, or nothing
// to drop) it returns an IncompleteError so Run stops with the history intact
// and the user can /compact, /new, or raise ARNES_CONTEXT_GUARD.
func (a *Agent) guardContext(ctx context.Context) error {
	if a.contextGuard <= 0 || compact.EstimateTokens(a.history) <= a.contextGuard {
		return nil
	}

	strat := a.compactor
	if strat == nil || strat.Name() == (compact.None{}).Name() {
		strat = compact.SlidingWindow{}
	}
	if compacted, err := strat.Compact(ctx, a.history); err == nil {
		a.history = compacted
	} else {
		a.warn(fmt.Errorf("compactación de emergencia (%s) falló: %w", strat.Name(), err))
	}

	if compact.EstimateTokens(a.history) > a.contextGuard {
		return &provider.IncompleteError{Reason: fmt.Sprintf("el historial superó el límite de contexto de seguridad (~%d tokens) y no se pudo "+
			"compactar lo suficiente -- usá /compact (probá la estrategia \"summarize\"), /new, o subí ARNES_CONTEXT_GUARD; el historial quedó guardado",
			a.contextGuard)}
	}
	return nil
}

// capToolResult elides the middle of an oversized tool result so one huge
// read_file or bash output can't push the history past the context window. Head
// and tail are both kept -- a build or test log puts the failure at the end.
func (a *Agent) capToolResult(s string) string {
	if a.maxToolResult <= 0 || len(s) <= a.maxToolResult {
		return s
	}
	head := a.maxToolResult * 3 / 5
	tail := a.maxToolResult - head
	omitted := len(s) - head - tail
	out := s[:head] +
		fmt.Sprintf("\n\n[... el arnés recortó %d caracteres del medio de esta salida para no desbordar el contexto; "+
			"si necesitás lo omitido, pedí un rango más chico o filtrá la salida ...]\n\n", omitted) +
		s[len(s)-tail:]
	return strings.ToValidUTF8(out, "") // drop any rune split by the byte cut
}

// maxTruncationRetries is how many times in a row Run will nudge the model to
// continue after its output was cut by the token limit before it gives up and
// hands back the partial answer with an explanation.
const maxTruncationRetries = 2

// repeatCallLimit is the identical-tool-call count that trips the loop guard:
// the same (name + arguments) call arriving for the Nth time means the model is
// stuck retrying something that isn't working, so the turn is cut before it
// burns the whole step budget.
const repeatCallLimit = 3

// maxVerifyRetries bounds how many times the completion gate will bounce the
// model back with a failing project check before it gives up and lets the turn
// end with the partial answer (the failing output stays in the history).
const maxVerifyRetries = 3

// Run appends the user input to the history and drives the inner loop until the
// model returns a final answer. It returns that final text.
func (a *Agent) Run(ctx context.Context, userInput string) (string, error) {
	a.maybeCompact(ctx)
	a.history = append(a.history, provider.Message{Role: provider.RoleUser, Text: userInput})

	var lastText string
	truncations := 0               // consecutive max-token cutoffs (reset on any progress)
	callCounts := map[string]int{} // identical tool call -> times seen this turn
	verifyFails := 0               // consecutive failing project checks at the completion gate
	todoNudged := false            // the unfinished-checklist nudge has fired this turn
	stopHookFired := false         // the stop hook has run this turn (it gets one shot)

	for step := 0; step < a.maxSteps; step++ {
		// Stop as soon as the caller cancels (Ctrl+C / timeout), rather than
		// waiting for the next provider or tool call to notice.
		if err := ctx.Err(); err != nil {
			return lastText, err
		}

		// Emergency context compaction: a turn that balloons past the model's
		// window mid-flight would otherwise 400 on every remaining call.
		if err := a.guardContext(ctx); err != nil {
			return lastText, err
		}

		req := provider.Request{
			System:    a.system + a.anchor(),
			Messages:  a.history,
			Tools:     a.toolDefs(),
			MaxTokens: a.maxTokens,
		}

		resp, err := a.callProvider(ctx, req)
		if err != nil {
			return "", fmt.Errorf("provider: %w", err)
		}
		// EffectiveInputTokens folds Anthropic's cache buckets in at their billing
		// weight, so the running cost stays accurate on long cached sessions.
		a.usedIn += resp.Usage.EffectiveInputTokens()
		a.usedOut += resp.Usage.OutputTokens

		lastText = resp.Text
		// Which tool calls arrived with unusable (truncated / malformed) JSON
		// arguments -- flagged by index before normalizeToolCalls rewrites them
		// to "{}" for the history.
		malformed := make([]bool, len(resp.ToolCalls))
		for i, c := range resp.ToolCalls {
			malformed[i] = !json.Valid(c.Input)
		}
		// Only record the assistant turn when it carries something. An empty
		// response (a stream cut before any token, a "length" stop with no text)
		// would serialize as {"role":"assistant","content":null} with no
		// tool_calls -- which the OpenAI API rejects on the NEXT call, poisoning
		// the whole session.
		if resp.Text != "" || len(resp.ToolCalls) > 0 {
			a.history = append(a.history, provider.Message{
				Role:      provider.RoleAssistant,
				Text:      resp.Text,
				ToolCalls: normalizeToolCalls(resp.ToolCalls),
			})
		}

		// The model's output was cut by the token limit with no tool call to run.
		// Nudge it to continue (or split the work) a couple of times, then stop
		// cleanly instead of returning a silently half-finished answer.
		if resp.StopReason == provider.StopMaxTokens && len(resp.ToolCalls) == 0 {
			truncations++
			if truncations > maxTruncationRetries {
				return lastText, &provider.IncompleteError{Reason: fmt.Sprintf("la respuesta se cortó por el límite de tokens de salida %d veces seguidas; "+
					"subí ARNES_MAX_TOKENS o \"max_tokens\" en la config, o pedí una respuesta más corta -- el historial quedó guardado", truncations)}
			}
			a.history = append(a.history, provider.Message{
				Role: provider.RoleUser,
				Text: "[arnés] Tu respuesta anterior se cortó por el límite de tokens de salida. Continuá exactamente " +
					"desde donde quedaste. Si estabas escribiendo un archivo grande con write_file, partilo: escribí " +
					"una primera parte y después agregá el resto con edit_file.",
			})
			continue
		}
		truncations = 0

		if resp.StopReason != provider.StopToolUse || len(resp.ToolCalls) == 0 {
			// The model wants to end the turn. It only gets to if the completion
			// gate agrees: the project check passes (once anything was edited) and
			// no checklist item is left silently open.
			if nudge, again := a.completionGate(ctx, &verifyFails, &todoNudged); again {
				a.history = append(a.history, provider.Message{Role: provider.RoleUser, Text: nudge})
				continue
			}
			// End-of-turn hook: a whole-project pass (lint, design review) gets
			// one shot to surface findings the model then addresses in a final
			// round. An empty return lets the turn end.
			if a.hooks != nil && !stopHookFired {
				stopHookFired = true
				if note := a.hooks.Stop(ctx); note != "" {
					a.history = append(a.history, provider.Message{Role: provider.RoleUser, Text: "[hook] " + note})
					continue
				}
			}
			return resp.Text, nil
		}

		// Loop guard: if the model keeps asking for the exact same call, it is
		// stuck. Cut the turn as soon as one call reaches repeatCallLimit rather
		// than let it grind through every remaining step.
		for _, call := range resp.ToolCalls {
			key := call.Name + "\x00" + string(call.Input)
			callCounts[key]++
			if callCounts[key] >= repeatCallLimit {
				return lastText, &provider.IncompleteError{Reason: fmt.Sprintf("el modelo repitió la misma llamada a %q %d veces sin avanzar; "+
					"corté el turno para no gastar el presupuesto en un loop -- revisá el último error de esa herramienta y reformulá el pedido",
					call.Name, callCounts[key])}
			}
		}

		results := make([]provider.ToolResult, 0, len(resp.ToolCalls))
		for i, call := range resp.ToolCalls {
			if malformed[i] {
				results = append(results, provider.ToolResult{CallID: call.ID, IsError: true,
					Content: fmt.Sprintf("Los argumentos de %q llegaron incompletos o mal formados (JSON inválido), "+
						"casi seguro porque tu respuesta se cortó por el límite de tokens. Reintentá con argumentos "+
						"más chicos, o partí el trabajo en pasos.", call.Name)})
				continue
			}
			results = append(results, a.runTool(ctx, call))
		}
		a.history = append(a.history, provider.Message{Role: provider.RoleUser, ToolResults: results})
	}

	// The turn ran out of step budget mid-work. The history (including this last
	// partial text) is kept and persisted by the caller, so the user can just
	// say "seguí" to continue from here, or raise ARNES_MAX_STEPS / "max_steps".
	return lastText, &provider.IncompleteError{Reason: fmt.Sprintf("me detuve tras %d pasos sin cerrar la respuesta (tope de seguridad contra loops); "+
		"el historial quedó guardado -- mandá \"seguí\" para continuar o subí el tope con ARNES_MAX_STEPS o \"max_steps\" en la config", a.maxSteps)}
}

// normalizeToolCalls guarantees every tool call carries valid JSON before it
// enters the history. A tool_use block the model never finished emitting arrives
// with empty or truncated Input; invalid json.RawMessage later breaks both the
// tool's own Unmarshal and session persistence (json.Marshal fails on it). The
// caller flags these first (see Run) so the model still gets a useful error.
func normalizeToolCalls(calls []provider.ToolCall) []provider.ToolCall {
	for i := range calls {
		if !json.Valid(calls[i].Input) {
			calls[i].Input = json.RawMessage("{}")
		}
	}
	return calls
}

// runTool applies the approval gateway, then executes the tool. It always
// returns a ToolResult: a refusal, an execution error, or a panic in the tool /
// a hook / the observer becomes an IsError result fed back to the model, never a
// hard failure of the loop (which, in the TUI, would freeze the turn).
func (a *Agent) runTool(ctx context.Context, call provider.ToolCall) (res provider.ToolResult) {
	defer func() {
		if r := recover(); r != nil {
			res = provider.ToolResult{CallID: call.ID, IsError: true,
				Content: fmt.Sprintf("La herramienta %q entró en pánico: %v", call.Name, r)}
		}
	}()

	if !a.approver.Confirm(call) {
		return provider.ToolResult{CallID: call.ID, IsError: true,
			Content: "El usuario denegó la ejecución de esta herramienta."}
	}

	if a.hooks != nil {
		if err := a.hooks.PreTool(ctx, call); err != nil {
			return provider.ToolResult{CallID: call.ID, IsError: true, Content: err.Error()}
		}
	}

	t, ok := a.tools.Get(call.Name)
	if !ok {
		return provider.ToolResult{CallID: call.ID, IsError: true,
			Content: fmt.Sprintf("No existe una herramienta llamada %q.", call.Name)}
	}

	if a.observe != nil {
		a.observe(call)
	}

	res = provider.ToolResult{CallID: call.ID}
	if out, err := t.Execute(ctx, call.Input); err != nil {
		res.IsError = true
		res.Content = fmt.Sprintf("La herramienta %q falló: %v", call.Name, err)
	} else {
		res.Content = out
	}

	// A mutating tool that ran means the workspace changed: the completion gate
	// must re-verify before this turn is allowed to end.
	if !res.IsError && isMutatingTool(call.Name) {
		a.dirty = true
	}

	if a.hooks != nil {
		if note := a.hooks.PostTool(ctx, call, res.Content, res.IsError); note != "" {
			res.Content += "\n\n[hook] " + note
		}
	}
	res.Content = a.capToolResult(res.Content)
	return res
}

// anchor returns the dynamic system-prompt suffix (original task + live plan),
// or "" when no anchor function is set.
func (a *Agent) anchor() string {
	if a.anchorFn == nil {
		return ""
	}
	return a.anchorFn()
}

// callProvider issues one model call (streaming when configured), retrying a
// transient failure -- a rate limit, a 5xx, a dropped or stalled stream -- up to
// providerRetries times with exponential backoff. A cancelled context or a
// non-transient error returns immediately. Nothing has been appended to the
// history yet at the point Run calls this, so a full re-request is safe.
func (a *Agent) callProvider(ctx context.Context, req provider.Request) (provider.Response, error) {
	for attempt := 0; ; attempt++ {
		var resp provider.Response
		var err error
		if s, ok := a.provider.(provider.Streamer); ok && a.stream {
			resp, err = s.StreamMessage(ctx, req, a.onDelta)
		} else {
			resp, err = a.provider.SendMessage(ctx, req)
		}
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil {
			return provider.Response{}, ctx.Err() // the user cancelled: surface that, not the provider error
		}
		if attempt >= a.providerRetries || !provider.Transient(err) {
			return resp, err
		}
		a.warn(fmt.Errorf("llamada al proveedor falló (%v); reintento %d/%d", err, attempt+1, a.providerRetries))
		select {
		case <-time.After(providerBackoff(attempt)):
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		}
	}
}

// providerBackoff is the wait before retry attempt n (0-indexed): 1s, 2s, 4s...
// capped, no jitter -- the transport layer already jitters its own retries.
func providerBackoff(attempt int) time.Duration {
	d := providerRetryBase << attempt
	if d <= 0 || d > providerRetryCap {
		d = providerRetryCap
	}
	return d
}

// mutatingTools are the tool names that change the workspace, so a turn that ran
// one must clear the completion gate's project check before it can end. bash is
// included because a shell command is the usual way an unseen edit (sed, mv,
// codegen) happens -- an extra check run on a read-only command is cheap.
var mutatingTools = map[string]bool{"write_file": true, "edit_file": true, "bash": true}

func isMutatingTool(name string) bool { return mutatingTools[name] }

// completionGate is run when the model tries to end a turn. It returns a nudge
// string and true when the turn must continue instead: the project check failed
// (verifyFails is bumped, capped at maxVerifyRetries), or the checklist still
// has open items (todoNudged flips, so this fires at most once per turn). When
// it returns ("", false) the turn is free to end.
func (a *Agent) completionGate(ctx context.Context, verifyFails *int, todoNudged *bool) (string, bool) {
	if a.verifier != nil && a.dirty && *verifyFails < maxVerifyRetries {
		out, ok := a.verifier(ctx)
		switch {
		case ok:
			a.dirty = false
			*verifyFails = 0
		default:
			*verifyFails++
			if *verifyFails >= maxVerifyRetries {
				a.warn(fmt.Errorf("la verificación del proyecto seguía fallando tras %d intentos; cierro el turno con el historial intacto", maxVerifyRetries))
				break
			}
			return fmt.Sprintf("[arnés] La verificación del proyecto falló. Arreglala antes de terminar -- no "+
				"digas que está listo con la verificación en rojo. Salida del check:\n\n%s", strings.TrimSpace(out)), true
		}
	}

	if a.openTodosFn != nil && !*todoNudged {
		if open := a.openTodosFn(); len(open) > 0 {
			*todoNudged = true
			return fmt.Sprintf("[arnés] Cerrás el turno con %d tarea(s) sin completar en tu checklist:\n- %s\n\n"+
				"Terminalas ahora, o si alguna ya no aplica marcala como corresponde con todo_write y explicá por qué.",
				len(open), strings.Join(open, "\n- ")), true
		}
	}
	return "", false
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
