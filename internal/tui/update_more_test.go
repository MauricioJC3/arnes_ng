package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

func TestModelInitReturnsCommands(t *testing.T) {
	full := New(Config{
		Conv: &fakeConv{}, Provider: provider.NewMock(),
		SessionID: func() string { return "s" },
		Approvals: make(chan approval.Request),
		Deltas:    make(chan string, 1),
		Notices:   make(chan string, 1),
		Todos:     make(chan []todo.Item, 1),
		Theme:     DefaultTheme(),
	})
	if full.Init() == nil {
		t.Fatal("Init con todos los canales no devolvió comandos")
	}

	minimal := New(Config{
		Conv: &fakeConv{}, Provider: provider.NewMock(),
		SessionID: func() string { return "s" },
		Approvals: make(chan approval.Request),
		Theme:     DefaultTheme(),
	})
	if minimal.Init() == nil {
		t.Fatal("Init mínimo no devolvió comandos")
	}
}

func TestModelNoticeAppendsInfoLine(t *testing.T) {
	m := New(Config{
		Conv: &fakeConv{}, Provider: provider.NewMock(),
		SessionID: func() string { return "s" },
		Approvals: make(chan approval.Request),
		Notices:   make(chan string, 1),
		Theme:     DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = tm.(Model)

	tm, cmd := m.Update(noticeMsg("hay una versión nueva"))
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("noticeMsg debería re-armar el waiter")
	}
	if !strings.Contains(plain(m), "hay una versión nueva") {
		t.Fatalf("el aviso no entró al transcript:\n%s", plain(m))
	}
}

func TestModelMouseToggle(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	if m.mouseOn {
		t.Fatal("el mouse debería arrancar apagado")
	}

	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = tm.(Model)
	if !m.mouseOn || cmd == nil {
		t.Fatal("Ctrl+O no encendió la captura del mouse / no emitió comando")
	}
	if !strings.Contains(m.View(), "scroll") {
		t.Fatalf("la status bar no indica el modo scroll:\n%s", m.View())
	}

	tm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = tm.(Model)
	if m.mouseOn || cmd == nil {
		t.Fatal("el segundo Ctrl+O no apagó el mouse")
	}
}

func TestModelMouseMsgIsHandled(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	tm, _ := m.Update(tea.MouseMsg{})
	if _, ok := tm.(Model); !ok {
		t.Fatal("MouseMsg no devolvió un Model")
	}
}

func TestModelResizeReflowsAssistantEntries(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	m.add(kindAssistant, "# título\n\nun texto lo bastante largo como para reajustarse a un ancho menor")
	before := m.entries[len(m.entries)-1].rendered

	tm, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = tm.(Model)
	if after := m.entries[len(m.entries)-1].rendered; after == before {
		t.Fatal("el resize no re-renderizó la entry assistant al ancho nuevo")
	}
}

func TestModelShiftTabNoopWithoutModes(t *testing.T) {
	m, _ := newModel(t, &fakeConv{}) // fakeConv no implementa command.Modes
	before := len(m.entries)
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = tm.(Model)
	if len(m.entries) != before {
		t.Fatal("shift+tab sin soporte de modos no debería tocar nada")
	}
}

func TestModelConnectFormCancelWithEsc(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	m.ta.SetValue("/connect")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if m.connect == nil || !strings.Contains(m.View(), "proveedor") {
		t.Fatalf("no se abrió/renderizó el formulario /connect:\n%s", m.View())
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if m.connect != nil || !strings.Contains(plain(m), "/connect cancelado") {
		t.Fatalf("Esc no canceló el formulario:\n%s", plain(m))
	}
}

func TestModelCtrlCClosesConnectForm(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	m.ta.SetValue("/connect")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = tm.(Model)
	if m.connect != nil || !strings.Contains(plain(m), "/connect cancelado") {
		t.Fatalf("Ctrl+C no cerró /connect:\n%s", plain(m))
	}
}

func TestModelCtrlCClosesMenu(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	m.menu.update("/")
	if !m.menu.open {
		t.Fatal("el menú debería estar abierto")
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = tm.(Model)
	if m.menu.open {
		t.Fatal("Ctrl+C no cerró el menú de comandos")
	}
}

func TestModelCtrlCDeniesPendingApproval(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	req := approval.Request{Call: provider.ToolCall{Name: "rm"}, Response: make(chan bool, 1)}
	tm, _ := m.Update(approvalMsg(req))
	m = tm.(Model)

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = tm.(Model)
	if m.pending != nil {
		t.Fatal("Ctrl+C no resolvió la aprobación pendiente")
	}
	if got := <-req.Response; got {
		t.Fatal("Ctrl+C sobre una aprobación debería denegar (false)")
	}
}

// connectErrConv implements Connector + Modeler but every call fails.
type connectErrConv struct {
	fakeConv
}

func (connectErrConv) Connect(_, _, _ string) (string, error) { return "", errors.New("boom") }
func (connectErrConv) ActiveProvider() string                 { return "anthropic" }
func (connectErrConv) Model() string                          { return "claude-x" }
func (connectErrConv) KeyedProviders() []string               { return []string{"anthropic"} }
func (connectErrConv) SetModel(string) (string, error)        { return "", errors.New("no se pudo") }

func TestModelConnectFormReportsConnectError(t *testing.T) {
	m := New(Config{
		Conv: &connectErrConv{}, Provider: provider.NewMock(),
		SessionID: func() string { return "s" },
		Approvals: make(chan approval.Request),
		Theme:     DefaultTheme(),
	}) // sin ListModels -> lista de modelos offline
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = tm.(Model)

	m.ta.SetValue("/connect")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // elige proveedor -> paso key
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // key vacía -> paso modelo (offline)
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // elige el primer modelo -> Connect falla
	m = tm.(Model)

	if m.connect != nil {
		t.Fatal("el formulario debería cerrarse aun cuando Connect falla")
	}
	if !strings.Contains(plain(m), "boom") {
		t.Fatalf("no se mostró el error de Connect:\n%s", plain(m))
	}
}

func TestModelModelPickerCancel(t *testing.T) {
	conv := &connectFake{}
	m := New(Config{
		Conv: conv, Provider: provider.NewMock(),
		SessionID: func() string { return "s" },
		Approvals: make(chan approval.Request), Theme: DefaultTheme(),
		ListModels: func(_ context.Context, p, _ string) ([]string, error) {
			return []string{p + "-a", p + "-b"}, nil
		},
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = tm.(Model)

	m.ta.SetValue("/model")
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	tm, _ = m.Update(cmd().(modelGroupsMsg))
	m = tm.(Model)
	if m.model == nil || !strings.Contains(m.View(), "modelo") {
		t.Fatalf("no se abrió/renderizó el picker de /model:\n%s", m.View())
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if m.model != nil || !strings.Contains(plain(m), "/model cancelado") {
		t.Fatalf("Esc no canceló el picker:\n%s", plain(m))
	}
}
