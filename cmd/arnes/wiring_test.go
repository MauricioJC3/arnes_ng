package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/config"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

func TestStartupConfigEnvOverrides(t *testing.T) {
	base := config.Config{Provider: "anthropic", Model: "claude-sonnet-5"}
	t.Setenv("ARNES_PROVIDER", "deepseek")
	t.Setenv("ARNES_MODEL", "") // vacío = no override

	got := startupConfig(base)
	if got.Provider != "deepseek" {
		t.Fatalf("ARNES_PROVIDER no pisó el provider: %q", got.Provider)
	}
	if got.Model != "claude-sonnet-5" {
		t.Fatalf("un ARNES_MODEL vacío no debería tocar el modelo: %q", got.Model)
	}
	if base.Provider != "anthropic" {
		t.Fatal("startupConfig mutó el config original")
	}
}

func TestResolveUI(t *testing.T) {
	t.Run("default: tui con streaming", func(t *testing.T) {
		t.Setenv("ARNES_UI", "")
		t.Setenv("ARNES_STREAM", "")
		if ui, s := resolveUI(); ui != "tui" || !s {
			t.Fatalf("ui=%q streaming=%v", ui, s)
		}
	})
	t.Run("plain nunca streamea", func(t *testing.T) {
		t.Setenv("ARNES_UI", "plain")
		t.Setenv("ARNES_STREAM", "")
		if ui, s := resolveUI(); ui != "plain" || s {
			t.Fatalf("ui=%q streaming=%v", ui, s)
		}
	})
	t.Run("ARNES_STREAM=off apaga el streaming en tui", func(t *testing.T) {
		t.Setenv("ARNES_UI", "tui")
		t.Setenv("ARNES_STREAM", "off")
		if _, s := resolveUI(); s {
			t.Fatal("streaming debería estar apagado con ARNES_STREAM=off")
		}
	})
}

func TestBuildApproverPlainMode(t *testing.T) {
	appr, approvals, deltas := buildApprover("plain", false, bufio.NewReader(strings.NewReader("")), io.Discard)

	if approvals != nil || deltas != nil {
		t.Fatal("el modo plain no debería crear canales de TUI")
	}
	if !appr.Confirm(provider.ToolCall{Name: "read_file"}) {
		t.Fatal("read_file debería pasar sin pedir aprobación")
	}
	if appr.Confirm(provider.ToolCall{Name: "bash"}) {
		t.Fatal("bash con stdin vacío (EOF) debería denegarse")
	}
}

func TestBuildApproverTUIStreaming(t *testing.T) {
	appr, approvals, deltas := buildApprover("tui", true, nil, nil)

	if approvals == nil {
		t.Fatal("en TUI debería exponerse el canal de approvals")
	}
	if deltas == nil || cap(deltas) != 256 {
		t.Fatalf("con streaming el canal de deltas debería ser buffered 256, cap=%d", cap(deltas))
	}
	if !appr.Confirm(provider.ToolCall{Name: "skill"}) {
		t.Fatal("skill debería pasar sin aprobación")
	}
}

func TestBuildApproverTUINoStreaming(t *testing.T) {
	_, approvals, deltas := buildApprover("tui", false, nil, nil)
	if approvals == nil {
		t.Fatal("approvals debería existir en TUI aun sin streaming")
	}
	if deltas != nil {
		t.Fatal("sin streaming no debería crearse el canal de deltas")
	}
}

func TestNewTodoBridgeKeepsOnlyTheLatestSnapshot(t *testing.T) {
	store, ch := newTodoBridge()

	store.Set([]todo.Item{{Content: "a", Status: todo.Pending}})
	store.Set([]todo.Item{{Content: "b", Status: todo.Pending}})
	store.Set([]todo.Item{{Content: "c", Status: todo.Pending}})

	select {
	case got := <-ch:
		if len(got) != 1 || got[0].Content != "c" {
			t.Fatalf("el canal debería tener sólo el último snapshot, tiene %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no llegó ningún snapshot al canal")
	}

	select {
	case extra := <-ch:
		t.Fatalf("quedó un snapshot viejo encolado: %+v", extra)
	default:
	}
}
