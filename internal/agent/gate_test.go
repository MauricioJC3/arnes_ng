package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
)

// editThenStop scripts a provider that asks for one edit_file call, then (on
// every later call) just returns final text. n counts provider calls.
func editThenStop(finalText string) (*provider.MockProvider, *int) {
	var n int
	p := &provider.MockProvider{}
	p.Handler = func(provider.Request) (provider.Response, error) {
		n++
		if n == 1 {
			return provider.Response{
				ToolCalls:  []provider.ToolCall{{ID: "c1", Name: "edit_file", Input: json.RawMessage(`{}`)}},
				StopReason: provider.StopToolUse,
			}, nil
		}
		return provider.Response{Text: finalText, StopReason: provider.StopEndTurn}, nil
	}
	return p, &n
}

func TestCompletionGateVerifier(t *testing.T) {
	ctx := context.Background()
	edit := &fakeTool{name: "edit_file", out: "written"}

	t.Run("verificación en verde deja cerrar el turno", func(t *testing.T) {
		p, _ := editThenStop("listo")
		calls := 0
		a := New(p, tool.NewRegistry(edit), approval.AllowAll{}, WithMaxSteps(5),
			WithVerifier(func(context.Context) (string, bool) { calls++; return "", true }))

		out, err := a.Run(ctx, "editá el archivo")
		if err != nil {
			t.Fatal(err)
		}
		if out != "listo" {
			t.Fatalf("out = %q, quiero \"listo\"", out)
		}
		if calls != 1 {
			t.Fatalf("verificador llamado %d veces, quiero 1", calls)
		}
	})

	t.Run("verificación en rojo vuelve al modelo con la salida y reintenta", func(t *testing.T) {
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(req provider.Request) (provider.Response, error) {
			n++
			switch n {
			case 1:
				return provider.Response{
					ToolCalls:  []provider.ToolCall{{ID: "c1", Name: "edit_file", Input: json.RawMessage(`{}`)}},
					StopReason: provider.StopToolUse,
				}, nil
			case 2:
				return provider.Response{Text: "ya está, listo", StopReason: provider.StopEndTurn}, nil
			default:
				last := req.Messages[len(req.Messages)-1]
				if last.Role != provider.RoleUser || !strings.Contains(last.Text, "verificación") {
					t.Fatalf("no llegó el aviso de verificación en rojo: %+v", last)
				}
				if !strings.Contains(last.Text, "FAIL: boom") {
					t.Fatalf("el aviso no incluyó la salida del check: %q", last.Text)
				}
				return provider.Response{Text: "ahora sí", StopReason: provider.StopEndTurn}, nil
			}
		}
		gate := 0
		a := New(p, tool.NewRegistry(edit), approval.AllowAll{}, WithMaxSteps(8),
			WithVerifier(func(context.Context) (string, bool) {
				gate++
				if gate == 1 {
					return "FAIL: boom", false
				}
				return "", true
			}))

		out, err := a.Run(ctx, "editá")
		if err != nil {
			t.Fatal(err)
		}
		if out != "ahora sí" {
			t.Fatalf("out = %q, quiero \"ahora sí\"", out)
		}
		if gate != 2 {
			t.Fatalf("verificador llamado %d veces, quiero 2", gate)
		}
	})

	t.Run("sin ediciones no corre el verificador", func(t *testing.T) {
		p := provider.NewMock(provider.Response{Text: "respuesta directa", StopReason: provider.StopEndTurn})
		calls := 0
		a := New(p, tool.NewRegistry(edit), approval.AllowAll{}, WithMaxSteps(5),
			WithVerifier(func(context.Context) (string, bool) { calls++; return "", true }))

		if _, err := a.Run(ctx, "solo respondé"); err != nil {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Fatalf("verificador llamado %d veces, quiero 0 (no hubo ediciones)", calls)
		}
	})

	t.Run("verificación en rojo repetida: se rinde tras el tope y no hace loop", func(t *testing.T) {
		p, _ := editThenStop("insisto: listo")
		gate := 0
		a := New(p, tool.NewRegistry(edit), approval.AllowAll{}, WithMaxSteps(30),
			WithVerifier(func(context.Context) (string, bool) { gate++; return "sigue en rojo", false }))

		out, err := a.Run(ctx, "editá")
		if err != nil {
			t.Fatalf("el turno debe cerrar con texto parcial, no fallar: %v", err)
		}
		if out != "insisto: listo" {
			t.Fatalf("out = %q", out)
		}
		if gate != maxVerifyRetries {
			t.Fatalf("verificador llamado %d veces, quiero el tope %d", gate, maxVerifyRetries)
		}
	})
}

func TestCompletionGateAnchor(t *testing.T) {
	ctx := context.Background()

	t.Run("el system prompt lleva el texto del ancla en cada llamada", func(t *testing.T) {
		p := provider.NewMock(
			provider.Response{
				ToolCalls:  []provider.ToolCall{{ID: "c1", Name: "edit_file", Input: json.RawMessage(`{}`)}},
				StopReason: provider.StopToolUse,
			},
			provider.Response{Text: "ok", StopReason: provider.StopEndTurn},
		)
		edit := &fakeTool{name: "edit_file", out: "w"}
		a := New(p, tool.NewRegistry(edit), approval.AllowAll{}, WithMaxSteps(5),
			WithSystem("BASE"),
			WithAnchorFn(func() string { return "\n\n# ANCLA\ntarea original + plan" }))

		if _, err := a.Run(ctx, "dale"); err != nil {
			t.Fatal(err)
		}
		for i, c := range p.Calls {
			if !strings.HasPrefix(c.System, "BASE") || !strings.Contains(c.System, "# ANCLA") {
				t.Fatalf("Calls[%d].System sin ancla: %q", i, c.System)
			}
		}
	})
}

func TestCompletionGateTodoNudge(t *testing.T) {
	ctx := context.Background()

	t.Run("nudge cuando el turno cierra con tareas pendientes", func(t *testing.T) {
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(req provider.Request) (provider.Response, error) {
			n++
			if n == 1 {
				return provider.Response{Text: "creo que terminé", StopReason: provider.StopEndTurn}, nil
			}
			last := req.Messages[len(req.Messages)-1]
			if last.Role != provider.RoleUser || !strings.Contains(last.Text, "sin completar") {
				t.Fatalf("no llegó el nudge de tareas pendientes: %+v", last)
			}
			return provider.Response{Text: "ok, ahora sí las cierro", StopReason: provider.StopEndTurn}, nil
		}
		open := []string{"conectar el handler", "agregar el test"}
		a := New(p, tool.NewRegistry(), approval.AllowAll{}, WithMaxSteps(5),
			WithOpenTodosFn(func() []string { return open }))

		out, err := a.Run(ctx, "hacé la feature")
		if err != nil {
			t.Fatal(err)
		}
		if out != "ok, ahora sí las cierro" {
			t.Fatalf("out = %q", out)
		}
		if n != 2 {
			t.Fatalf("llamadas al provider = %d, quiero 2", n)
		}
	})

	t.Run("el nudge de tareas es una sola vez por turno", func(t *testing.T) {
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			n++
			return provider.Response{Text: "sigo sin cerrarlas", StopReason: provider.StopEndTurn}, nil
		}
		a := New(p, tool.NewRegistry(), approval.AllowAll{}, WithMaxSteps(10),
			WithOpenTodosFn(func() []string { return []string{"algo pendiente"} }))

		out, err := a.Run(ctx, "hacelo")
		if err != nil {
			t.Fatal(err)
		}
		if out != "sigo sin cerrarlas" {
			t.Fatalf("out = %q", out)
		}
		if n != 2 {
			t.Fatalf("llamadas al provider = %d, quiero 2 (un solo nudge y corta)", n)
		}
	})
}
