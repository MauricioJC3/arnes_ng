package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
)

// recordingProvider captures every request and returns a fixed final answer.
func recordingProvider(text string) (*provider.MockProvider, *[]provider.Request) {
	var seen []provider.Request
	p := &provider.MockProvider{}
	p.Handler = func(req provider.Request) (provider.Response, error) {
		seen = append(seen, req)
		return provider.Response{Text: text, StopReason: provider.StopEndTurn}, nil
	}
	return p, &seen
}

// countingApprover records how many times Confirm was called and always allows.
type countingApprover struct{ n int }

func (c *countingApprover) Confirm(provider.ToolCall) bool { c.n++; return true }

func toolNames(defs []provider.ToolDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}

func mustInput(t *testing.T, agent, task string) json.RawMessage {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"agent": agent, "task": task})
	return b
}

// newDelegate wires a DelegateTool whose tool pool includes itself.
func newDelegate(t *testing.T, p provider.Provider, defs *Registry, base *tool.Registry, opts ...Option) *DelegateTool {
	t.Helper()
	d := NewDelegateTool(defs, func() provider.Provider { return p }, nil, func() approval.Approver { return approval.AllowAll{} }, opts...)
	d.tools = base.With(d)
	return d
}

func TestDelegateExecute(t *testing.T) {
	ctx := context.Background()
	base := tool.NewRegistry(tool.ReadFile{}, tool.Bash{}, tool.WriteFile{})

	t.Run("corre el subagente y devuelve su respuesta final", func(t *testing.T) {
		p, seen := recordingProvider("investigado: está en foo.go")
		defs := NewRegistry(Definition{Name: "research", System: "sos research", Tools: []string{"read_file"}})
		d := newDelegate(t, p, defs, base)

		out, err := d.Execute(ctx, mustInput(t, "research", "dónde está la config"))
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if out != "investigado: está en foo.go" {
			t.Fatalf("out = %q", out)
		}
		if len(*seen) == 0 {
			t.Fatal("el subagente no llamó al provider")
		}
		// el system prompt del subagente es el de la definición
		if (*seen)[0].System != "sos research" {
			t.Errorf("system del subagente = %q", (*seen)[0].System)
		}
	})

	t.Run("respeta la lista de tools de la definición", func(t *testing.T) {
		p, seen := recordingProvider("ok")
		defs := NewRegistry(Definition{Name: "r", System: "s", Tools: []string{"read_file"}})
		d := newDelegate(t, p, defs, base)

		if _, err := d.Execute(ctx, mustInput(t, "r", "tarea")); err != nil {
			t.Fatal(err)
		}
		got := toolNames((*seen)[0].Tools)
		if len(got) != 1 || got[0] != "read_file" {
			t.Fatalf("tools del subagente = %v, quiero [read_file]", got)
		}
	})

	t.Run("el subagente nunca recibe la tool delegate", func(t *testing.T) {
		p, seen := recordingProvider("ok")
		defs := NewRegistry(Definition{Name: "r", System: "s"}) // Tools vacío = todas
		d := newDelegate(t, p, defs, base)

		if _, err := d.Execute(ctx, mustInput(t, "r", "tarea")); err != nil {
			t.Fatal(err)
		}
		for _, n := range toolNames((*seen)[0].Tools) {
			if n == ToolName {
				t.Fatalf("el subagente recibió %q: %v", ToolName, toolNames((*seen)[0].Tools))
			}
		}
		if len(toolNames((*seen)[0].Tools)) != 3 { // read_file, bash, write_file
			t.Fatalf("esperaba las 3 tools base, tengo %v", toolNames((*seen)[0].Tools))
		}
	})

	t.Run("InheritHistory pasa el historial del padre", func(t *testing.T) {
		p, seen := recordingProvider("ok")
		defs := NewRegistry(Definition{Name: "r", System: "s", InheritHistory: true})
		parent := []provider.Message{{Role: provider.RoleUser, Text: "contexto del padre"}}
		d := newDelegate(t, p, defs, base, WithParentHistory(func() []provider.Message { return parent }))

		if _, err := d.Execute(ctx, mustInput(t, "r", "seguí")); err != nil {
			t.Fatal(err)
		}
		msgs := (*seen)[0].Messages
		if len(msgs) != 2 || msgs[0].Text != "contexto del padre" || msgs[1].Text != "seguí" {
			t.Fatalf("historial del subagente mal armado: %+v", msgs)
		}
	})

	t.Run("el approver se resuelve por llamada (respeta un cambio de modo)", func(t *testing.T) {
		// Provider que pide una tool call y en el turno siguiente cierra.
		mp := &provider.MockProvider{}
		step := 0
		mp.Handler = func(provider.Request) (provider.Response, error) {
			if step++; step%2 == 1 {
				return provider.Response{
					ToolCalls:  []provider.ToolCall{{ID: "c1", Name: "read_file", Input: json.RawMessage(`{"path":"x"}`)}},
					StopReason: provider.StopToolUse,
				}, nil
			}
			return provider.Response{Text: "listo", StopReason: provider.StopEndTurn}, nil
		}
		defs := NewRegistry(Definition{Name: "r", System: "s", Tools: []string{"read_file"}})

		cur := &countingApprover{}
		d := NewDelegateTool(defs, func() provider.Provider { return mp }, nil,
			func() approval.Approver { return cur })
		d.tools = base.With(d)

		if _, err := d.Execute(ctx, mustInput(t, "r", "leé x")); err != nil {
			t.Fatal(err)
		}
		if cur.n != 1 {
			t.Fatalf("el primer approver se consultó %d veces, quiero 1", cur.n)
		}

		// "Cambio de modo": la fn ahora devuelve otro approver. El delegate debe
		// usar el nuevo, no el que capturó antes.
		next := &countingApprover{}
		cur = next
		if _, err := d.Execute(ctx, mustInput(t, "r", "leé x de nuevo")); err != nil {
			t.Fatal(err)
		}
		if next.n != 1 {
			t.Fatalf("el approver nuevo se consultó %d veces, quiero 1", next.n)
		}
	})

	t.Run("subagente desconocido es error", func(t *testing.T) {
		d := newDelegate(t, provider.NewMock(), NewRegistry(), base)
		if _, err := d.Execute(ctx, mustInput(t, "fantasma", "x")); err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("task vacía es error", func(t *testing.T) {
		defs := NewRegistry(Definition{Name: "r", System: "s"})
		d := newDelegate(t, provider.NewMock(), defs, base)
		if _, err := d.Execute(ctx, mustInput(t, "r", "   ")); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestDelegateDescriptionListsSubagents(t *testing.T) {
	defs := NewRegistry(
		Definition{Name: "research", Description: "explora código"},
		Definition{Name: "test-writer", Description: "escribe tests"},
	)
	mock := provider.NewMock()
	d := NewDelegateTool(defs, func() provider.Provider { return mock }, tool.NewRegistry(), func() approval.Approver { return approval.AllowAll{} })
	desc := d.Description()
	if !strings.Contains(desc, "research: explora código") || !strings.Contains(desc, "test-writer: escribe tests") {
		t.Fatalf("Description no lista los subagentes:\n%s", desc)
	}
}
