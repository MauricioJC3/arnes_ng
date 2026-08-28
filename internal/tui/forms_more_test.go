package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// multiProviderConv has a key for two providers, so the /model picker lists rows
// from both and can switch providers.
type multiProviderConv struct {
	fakeConv
	setModel     string
	setModelErr  error
	connProvider string
	connModel    string
}

func (c *multiProviderConv) ActiveProvider() string   { return "deepseek" }
func (c *multiProviderConv) Model() string            { return "deepseek-v4-flash" }
func (c *multiProviderConv) KeyedProviders() []string { return []string{"deepseek", "anthropic"} }

func (c *multiProviderConv) SetModel(m string) (string, error) {
	c.setModel = m
	return "modelo: " + m, c.setModelErr
}

func (c *multiProviderConv) Connect(p, m, _ string) (string, error) {
	c.connProvider, c.connModel = p, m
	return "cambié a " + p, nil
}

func openModelPicker(t *testing.T, conv *multiProviderConv) Model {
	t.Helper()
	m := New(Config{
		Conv: conv, Provider: provider.NewMock(),
		SessionID: func() string { return "s" },
		Approvals: make(chan approval.Request), Theme: DefaultTheme(),
		ListModels: func(_ context.Context, p, _ string) ([]string, error) {
			return []string{p + "-uno"}, nil
		},
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = tm.(Model)
	m.ta.SetValue("/model")
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	tm, _ = m.Update(cmd().(modelGroupsMsg))
	m = tm.(Model)
	if m.model == nil {
		t.Fatal("no se abrió el picker de /model")
	}
	return m
}

func TestModelPickerSwitchesProvider(t *testing.T) {
	conv := &multiProviderConv{}
	m := openModelPicker(t, conv)

	// filas: deepseek-uno (idx 0), anthropic-uno (idx 1), manual (idx 2)
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = tm.(Model)
	if r := m.model.rows[m.model.idx]; r.provider != "anthropic" || r.manual {
		t.Fatalf("esperaba la fila anthropic-uno, tengo %+v", r)
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if m.model != nil {
		t.Fatal("el picker debería cerrarse tras elegir")
	}
	if conv.connProvider != "anthropic" || conv.connModel != "anthropic-uno" {
		t.Fatalf("no cambió de proveedor vía Connect: %q/%q", conv.connProvider, conv.connModel)
	}
	if conv.setModel != "" {
		t.Fatalf("no debería haber llamado SetModel al cambiar de proveedor (%q)", conv.setModel)
	}
}

func TestModelPickerReportsSetModelError(t *testing.T) {
	conv := &multiProviderConv{setModelErr: errors.New("modelo inválido")}
	m := openModelPicker(t, conv)

	// idx 0 = deepseek-uno, mismo proveedor que el activo -> camino SetModel
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if m.model != nil {
		t.Fatal("el picker debería cerrarse aun con error")
	}
	if !strings.Contains(plain(m), "modelo inválido") {
		t.Fatalf("no se mostró el error de SetModel:\n%s", plain(m))
	}
}

func TestConnectFormViewAtEachStep(t *testing.T) {
	m := New(Config{
		Conv: &connectFake{}, Provider: provider.NewMock(),
		SessionID: func() string { return "s" },
		Approvals: make(chan approval.Request), Theme: DefaultTheme(),
	}) // sin ListModels -> el paso modelo usa la lista offline
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = tm.(Model)

	m.ta.SetValue("/connect")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if !strings.Contains(m.View(), "proveedor") {
		t.Fatalf("paso proveedor no renderiza:\n%s", m.View())
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // -> paso key
	m = tm.(Model)
	if !strings.Contains(m.View(), "API key") {
		t.Fatalf("paso key no renderiza:\n%s", m.View())
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // key vacía -> paso modelo (offline)
	m = tm.(Model)
	if !strings.Contains(m.View(), "modelo para anthropic") {
		t.Fatalf("paso modelo no renderiza:\n%s", m.View())
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp}) // envuelve a la última fila = escribir a mano
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if m.connect == nil || !m.connect.manual {
		t.Fatal("no entró al modo manual del paso modelo")
	}
	if !strings.Contains(m.View(), "confirma") {
		t.Fatalf("el input manual no renderiza:\n%s", m.View())
	}
}
