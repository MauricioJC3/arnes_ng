package tui

import (
	"errors"
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
	if d, c, fetch := f.update(key("down")); d != nil || c || fetch {
		t.Fatal("no debería terminar en el paso de proveedor")
	}
	if _, _, _ = f.update(key("enter")); f.step != stepKey || f.provider != "deepseek" {
		t.Fatalf("no pasó a stepKey: step=%v provider=%q", f.step, f.provider)
	}

	// paso key: escribir y confirmar -> dispara la búsqueda de modelos
	for _, r := range "sk-xyz" {
		f.update(key(string(r)))
	}
	if _, _, fetch := f.update(key("enter")); !fetch || f.step != stepModel || !f.loading {
		t.Fatalf("enter en la key debería pedir la lista: fetch=%v step=%v loading=%v", fetch, f.step, f.loading)
	}

	// llega la lista en vivo
	f.setModels([]string{"deepseek-v4-flash", "deepseek-v4-pro"}, nil)
	if f.loading || f.models[f.modelIdx] != "deepseek-v4-flash" {
		t.Fatalf("setModels no dejó el picker listo: loading=%v models=%v", f.loading, f.models)
	}
	if f.models[len(f.models)-1] != manualModelOption {
		t.Fatalf("falta la opción manual al final: %v", f.models)
	}

	// elegir el primero
	done, cancelled, _ := f.update(key("enter"))
	if cancelled || done == nil {
		t.Fatalf("no terminó: done=%v cancelled=%v", done, cancelled)
	}
	if done.provider != "deepseek" || done.model != "deepseek-v4-flash" || done.key != "sk-xyz" {
		t.Fatalf("resultado = %+v", *done)
	}
}

func TestConnectFormCancel(t *testing.T) {
	f := newConnectForm()
	if _, c, _ := f.update(key("esc")); !c {
		t.Fatal("esc debería cancelar")
	}
}

func TestConnectFormKeyIsMasked(t *testing.T) {
	f := newConnectForm()
	f.update(key("enter")) // -> stepKey
	for _, r := range "secret" {
		f.update(key(string(r)))
	}
	if got := f.input.View(); got == "secret" || contains(got, "secret") {
		t.Fatalf("la key se ve en claro: %q", got)
	}
}

func TestConnectFormOfflineFallback(t *testing.T) {
	f := newConnectForm()
	f.update(key("enter"))            // proveedor: anthropic -> stepKey
	f.update(key("enter"))            // key vacía -> stepModel, loading
	f.setModels(nil, errors.New("x")) // la búsqueda falló

	if f.loading {
		t.Fatal("setModels con error debería salir de loading")
	}
	if f.note == "" {
		t.Fatal("un fallo debería dejar una nota visible")
	}
	// usa la lista local del proveedor + opción manual
	want := len(connectModels["anthropic"]) + 1
	if len(f.models) != want {
		t.Fatalf("models = %v (querìa %d filas)", f.models, want)
	}
}

func TestConnectFormManualModel(t *testing.T) {
	f := newConnectForm()
	f.update(key("enter")) // proveedor: anthropic -> stepKey
	for _, r := range "sk-1" {
		f.update(key(string(r)))
	}
	f.update(key("enter")) // -> stepModel, loading
	f.setModels(nil, nil)  // sin red: cae a la lista local
	f.update(key("up"))    // "up" desde el primer ítem envuelve al último = manual

	if f.models[f.modelIdx] != manualModelOption {
		t.Fatalf("no quedó sobre la opción manual: %q", f.models[f.modelIdx])
	}
	if _, _, _ = f.update(key("enter")); !f.manual || f.step != stepModel {
		t.Fatalf("no entró en modo manual: manual=%v step=%v", f.manual, f.step)
	}

	for _, r := range "modelo-raro-9000" {
		f.update(key(string(r)))
	}
	done, cancelled, _ := f.update(key("enter"))
	if cancelled || done == nil || done.model != "modelo-raro-9000" || done.provider != "anthropic" || done.key != "sk-1" {
		t.Fatalf("resultado = %+v cancelled=%v", done, cancelled)
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
