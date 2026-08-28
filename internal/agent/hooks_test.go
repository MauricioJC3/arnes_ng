package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
)

type stubHooks struct {
	preErr   error
	postNote string
	preCalls int
	postSeen string // result string PostTool was handed
}

func (s *stubHooks) PreTool(context.Context, provider.ToolCall) error {
	s.preCalls++
	return s.preErr
}

func (s *stubHooks) PostTool(_ context.Context, _ provider.ToolCall, result string, _ bool) string {
	s.postSeen = result
	return s.postNote
}

func toolUseThenDone(name string) *provider.MockProvider {
	return provider.NewMock(
		provider.Response{
			ToolCalls:  []provider.ToolCall{{ID: "c1", Name: name, Input: json.RawMessage(`{}`)}},
			StopReason: provider.StopToolUse,
		},
		provider.Response{Text: "listo", StopReason: provider.StopEndTurn},
	)
}

func TestAgentHooks(t *testing.T) {
	ctx := context.Background()

	t.Run("pre-tool que bloquea cancela la ejecución", func(t *testing.T) {
		ft := &fakeTool{name: "danger", out: "no debería correr"}
		h := &stubHooks{preErr: errors.New("hook dijo que no")}
		a := New(toolUseThenDone("danger"), tool.NewRegistry(ft), approval.AllowAll{},
			WithMaxSteps(5), WithHooks(h))

		if _, err := a.Run(ctx, "hacelo"); err != nil {
			t.Fatal(err)
		}
		if h.preCalls != 1 {
			t.Fatalf("PreTool corrió %d veces, quiero 1", h.preCalls)
		}
		if ft.calls != 0 {
			t.Fatalf("la tool corrió pese al bloqueo (%d veces)", ft.calls)
		}
		fed := a.History()[2].ToolResults[0]
		if !fed.IsError || !strings.Contains(fed.Content, "hook dijo que no") {
			t.Fatalf("no se realimentó el bloqueo: %+v", fed)
		}
	})

	t.Run("post-tool agrega una nota al resultado", func(t *testing.T) {
		ft := &fakeTool{name: "edit", out: "editado ok"}
		h := &stubHooks{postNote: "gofmt: 1 archivo"}
		a := New(toolUseThenDone("edit"), tool.NewRegistry(ft), approval.AllowAll{},
			WithMaxSteps(5), WithHooks(h))

		if _, err := a.Run(ctx, "editá"); err != nil {
			t.Fatal(err)
		}
		if h.postSeen != "editado ok" {
			t.Fatalf("PostTool recibió %q", h.postSeen)
		}
		got := a.History()[2].ToolResults[0].Content
		if !strings.Contains(got, "editado ok") || !strings.Contains(got, "[hook] gofmt: 1 archivo") {
			t.Fatalf("resultado sin la nota del hook: %q", got)
		}
	})
}
