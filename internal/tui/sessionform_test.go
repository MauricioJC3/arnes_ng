package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/session"
)

type fakeSessionsConv struct {
	fakeConv
	metas   []session.Meta
	resumed string
}

func (f *fakeSessionsConv) ListSessions() ([]session.Meta, error) { return f.metas, nil }

func (f *fakeSessionsConv) ResumeSession(id string) (string, error) {
	f.resumed = id
	return "reanudada " + id, nil
}

func (f *fakeSessionsConv) NewSession() (string, error) { return "sesión nueva", nil }

func threeMetas() []session.Meta {
	return []session.Meta{
		{ID: "20260101-000000-aaaa", Title: "refactor del panel", Messages: 8, Todo: session.TodoProgress{Done: 1, Total: 3}},
		{ID: "20260102-000000-bbbb", Title: "fix del provider", Messages: 4, Todo: session.TodoProgress{Done: 2, Total: 2}},
		{ID: "20260103-000000-cccc", Title: "sin tareas", Messages: 2},
	}
}

func openSessionPicker(t *testing.T, conv *fakeSessionsConv) Model {
	t.Helper()
	m := New(Config{
		Conv:      conv,
		Provider:  provider.NewMock(),
		SessionID: func() string { return "otra" },
		Approvals: make(chan approval.Request),
		Theme:     DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = tm.(Model)
	m.ta.SetValue("/sessions")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if m.session == nil {
		t.Fatal("no se abrió el picker de /sessions")
	}
	return m
}

func TestSessionFormNavAndSelect(t *testing.T) {
	f := newSessionForm(threeMetas(), "20260102-000000-bbbb")
	if f.idx != 1 {
		t.Fatalf("el cursor debería arrancar en la sesión actual (idx 1), está en %d", f.idx)
	}

	f.update(tea.KeyMsg{Type: tea.KeyDown})
	if f.idx != 2 {
		t.Fatalf("down -> idx = %d, quiero 2", f.idx)
	}

	done, cancelled := f.update(tea.KeyMsg{Type: tea.KeyEnter})
	if cancelled || done == nil || done.id != "20260103-000000-cccc" {
		t.Fatalf("enter debería devolver la 3ra sesión: done=%+v cancelled=%v", done, cancelled)
	}

	if _, cancelled := f.update(tea.KeyMsg{Type: tea.KeyEsc}); !cancelled {
		t.Fatal("esc debería cancelar")
	}
}

func TestSlashSessionsOpensPickerWithProgress(t *testing.T) {
	m := openSessionPicker(t, &fakeSessionsConv{metas: threeMetas()})
	v := m.View()
	for _, want := range []string{"refactor del panel", "1/3 tareas", "✓ tareas"} {
		if !strings.Contains(v, want) {
			t.Fatalf("el picker no muestra %q:\n%s", want, v)
		}
	}
}

func TestSlashLsAliasOpensPicker(t *testing.T) {
	conv := &fakeSessionsConv{metas: threeMetas()}
	m := New(Config{
		Conv: conv, Provider: provider.NewMock(),
		SessionID: func() string { return "x" },
		Approvals: make(chan approval.Request), Theme: DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = tm.(Model)
	m.ta.SetValue("/ls")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tm.(Model).session == nil {
		t.Fatal("/ls también debería abrir el picker")
	}
}

func TestSessionPickerResumesOnEnter(t *testing.T) {
	conv := &fakeSessionsConv{metas: threeMetas()}
	m := openSessionPicker(t, conv)

	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown}) // -> idx 1
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)

	if m.session != nil {
		t.Fatal("el picker debería cerrarse tras elegir")
	}
	if conv.resumed != "20260102-000000-bbbb" {
		t.Fatalf("ResumeSession recibió %q", conv.resumed)
	}
	if !strings.Contains(plain(m), "reanudada 20260102-000000-bbbb") {
		t.Fatalf("no se registró la reanudación:\n%s", plain(m))
	}
}

func TestSessionPickerEscClosesIt(t *testing.T) {
	m := openSessionPicker(t, &fakeSessionsConv{metas: threeMetas()})
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tm.(Model).session != nil {
		t.Fatal("esc debería cerrar el picker")
	}
}

func TestSlashSessionsFallsBackWhenEmpty(t *testing.T) {
	conv := &fakeSessionsConv{metas: nil}
	m := New(Config{
		Conv: conv, Provider: provider.NewMock(),
		SessionID: func() string { return "x" },
		Approvals: make(chan approval.Request), Theme: DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = tm.(Model)
	m.ta.SetValue("/sessions")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)

	if m.session != nil {
		t.Fatal("sin sesiones no debería abrirse el picker interactivo")
	}
	if !strings.Contains(plain(m), "no hay sesiones guardadas") {
		t.Fatalf("debería caer al listado de texto:\n%s", plain(m))
	}
}
