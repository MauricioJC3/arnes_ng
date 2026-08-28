package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestConnectFormHappyPath(t *testing.T) {
	f := newConnectForm()

	// paso proveedor: bajar una vez -> deepseek
	if d, c := f.update(key("down")); d != nil || c {
		t.Fatal("no debería terminar en el paso de proveedor")
	}
	if _, _ = f.update(key("enter")); f.step != stepModel {
		t.Fatalf("no pasó a stepModel: %v", f.step)
	}
	if f.provider != "deepseek" {
		t.Fatalf("provider = %q", f.provider)
	}
	// el modelo viene precargado con el default
	if f.input.Value() != "deepseek-chat" {
		t.Fatalf("modelo precargado = %q", f.input.Value())
	}

	// paso modelo: confirmar con el default
	if _, _ = f.update(key("enter")); f.step != stepKey {
		t.Fatalf("no pasó a stepKey: %v", f.step)
	}

	// paso key: escribir y confirmar
	for _, r := range "sk-xyz" {
		f.update(key(string(r)))
	}
	done, cancelled := f.update(key("enter"))
	if cancelled || done == nil {
		t.Fatalf("no terminó: done=%v cancelled=%v", done, cancelled)
	}
	if done.provider != "deepseek" || done.model != "deepseek-chat" || done.key != "sk-xyz" {
		t.Fatalf("resultado = %+v", *done)
	}
}

func TestConnectFormCancel(t *testing.T) {
	f := newConnectForm()
	if _, c := f.update(key("esc")); !c {
		t.Fatal("esc debería cancelar")
	}
}

func TestConnectFormKeyIsMasked(t *testing.T) {
	f := newConnectForm()
	f.update(key("enter")) // -> stepModel
	f.update(key("enter")) // -> stepKey
	for _, r := range "secret" {
		f.update(key(string(r)))
	}
	if got := f.input.View(); got == "secret" || contains(got, "secret") {
		t.Fatalf("la key se ve en claro: %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
