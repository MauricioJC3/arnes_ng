package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/agent"
	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
)

// ToolName is the fixed name of the delegate tool.
const ToolName = "delegate"

// DelegateTool lets the main agent hand a scoped task to a named subagent. It
// implements tool.Tool, so it registers like any other tool.
type DelegateTool struct {
	defs       *Registry
	providerFn func() provider.Provider // resolved per call so /connect is honored
	tools      *tool.Registry
	approver   approval.Approver
	history    func() []provider.Message // parent history, for InheritHistory
	maxSteps   int
}

// Option configures a DelegateTool.
type Option func(*DelegateTool)

// WithParentHistory supplies the parent conversation history for subagents
// whose Definition has InheritHistory set.
func WithParentHistory(f func() []provider.Message) Option {
	return func(d *DelegateTool) { d.history = f }
}

// WithMaxSteps bounds each subagent's inner loop (default 20).
func WithMaxSteps(n int) Option {
	return func(d *DelegateTool) {
		if n > 0 {
			d.maxSteps = n
		}
	}
}

// NewDelegateTool builds the tool. providerFn is called per delegation so a
// runtime /connect is picked up. tools is the pool subagents draw from; the
// delegate tool itself is always excluded from a subagent's set.
func NewDelegateTool(defs *Registry, providerFn func() provider.Provider, tools *tool.Registry, ap approval.Approver, opts ...Option) *DelegateTool {
	d := &DelegateTool{defs: defs, providerFn: providerFn, tools: tools, approver: ap, maxSteps: 20}
	for _, o := range opts {
		o(d)
	}
	return d
}

func (*DelegateTool) Name() string { return ToolName }

func (d *DelegateTool) Description() string {
	var b strings.Builder
	b.WriteString("Delegá una subtarea acotada y autocontenida a un subagente especializado. " +
		"Corre su propio loop con su propio system prompt y herramientas, y devuelve un resultado de texto. " +
		"Subagentes disponibles:\n")
	for _, def := range d.defs.All() {
		fmt.Fprintf(&b, "  - %s: %s\n", def.Name, def.Description)
	}
	return b.String()
}

func (*DelegateTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent": map[string]any{"type": "string", "description": "Nombre del subagente a usar."},
			"task":  map[string]any{"type": "string", "description": "La tarea concreta y autocontenida para el subagente."},
		},
		"required": []string{"agent", "task"},
	}
}

func (d *DelegateTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Agent string `json:"agent"`
		Task  string `json:"task"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	def, ok := d.defs.Get(in.Agent)
	if !ok {
		return "", fmt.Errorf("no existe el subagente %q", in.Agent)
	}
	if strings.TrimSpace(in.Task) == "" {
		return "", errors.New("'task' es obligatorio")
	}

	p := d.providerFn()

	// Per-subagent model via save/restore. Delegation is synchronous -- the
	// parent is blocked inside this call -- so mutating the shared provider is safe.
	if def.Model != "" && def.Model != p.Model() {
		prev := p.Model()
		p.SetModel(def.Model)
		defer p.SetModel(prev)
	}

	opts := []agent.Option{
		agent.WithSystem(def.System),
		agent.WithMaxSteps(d.maxSteps),
	}
	if def.InheritHistory && d.history != nil {
		opts = append(opts, agent.WithHistory(d.history()))
	}

	sub := agent.New(p, d.subagentTools(def), d.approver, opts...)
	out, err := sub.Run(ctx, in.Task)
	if err != nil {
		return "", fmt.Errorf("subagente %q: %w", def.Name, err)
	}
	return out, nil
}

// subagentTools resolves the tool set for def, always excluding the delegate
// tool itself so subagents cannot spawn subagents.
func (d *DelegateTool) subagentTools(def Definition) *tool.Registry {
	names := def.Tools
	if len(names) == 0 {
		for _, t := range d.tools.All() {
			names = append(names, t.Name())
		}
	}
	kept := make([]string, 0, len(names))
	for _, n := range names {
		if n != ToolName {
			kept = append(kept, n)
		}
	}
	return d.tools.Subset(kept...)
}
