package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestModelFormSetGroupsBuildsRows(t *testing.T) {
	f := newModelForm("deepseek", "deepseek-v4-pro")
	if !f.loading {
		t.Fatal("un form recién creado debería estar cargando")
	}
	f.setGroups([]modelProviderGroup{
		{provider: "deepseek", models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}},
		{provider: "anthropic", models: []string{"claude-opus-5"}},
	})

	if f.loading {
		t.Fatal("setGroups debería apagar loading")
	}
	// 2 + 1 modelos + fila manual
	if len(f.rows) != 4 {
		t.Fatalf("rows = %d (%+v)", len(f.rows), f.rows)
	}
	if !f.rows[len(f.rows)-1].manual {
		t.Fatal("la última fila debería ser la de escribir a mano")
	}
	// el cursor arranca sobre el modelo actual
	if got := f.rows[f.idx]; got.provider != "deepseek" || got.model != "deepseek-v4-pro" || !got.current {
		t.Fatalf("cursor mal ubicado: %+v", got)
	}
}

func TestModelFormFallbackOnError(t *testing.T) {
	f := newModelForm("deepseek", "x")
	f.setGroups([]modelProviderGroup{
		{provider: "deepseek", err: errors.New("boom")},
	})
	if f.note == "" {
		t.Fatal("un fallo debería dejar una nota")
	}
	// usó la lista local de deepseek + manual
	if len(f.rows) != len(connectModels["deepseek"])+1 {
		t.Fatalf("rows = %d", len(f.rows))
	}
}

func TestModelFormPickSameProvider(t *testing.T) {
	f := newModelForm("deepseek", "deepseek-v4-flash")
	f.setGroups([]modelProviderGroup{
		{provider: "deepseek", models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}},
	})
	f.update(key("down")) // flash -> pro

	done, cancelled := f.update(key("enter"))
	if cancelled || done == nil {
		t.Fatalf("no terminó: done=%v cancelled=%v", done, cancelled)
	}
	if done.provider != "deepseek" || done.model != "deepseek-v4-pro" {
		t.Fatalf("pick = %+v", *done)
	}
}

func TestModelFormPickOtherProvider(t *testing.T) {
	f := newModelForm("deepseek", "deepseek-v4-flash")
	f.setGroups([]modelProviderGroup{
		{provider: "deepseek", models: []string{"deepseek-v4-flash"}},
		{provider: "anthropic", models: []string{"claude-opus-5"}},
	})
	// filas: [0]=deepseek/flash(current) [1]=anthropic/opus [2]=manual
	f.idx = 1
	done, _ := f.update(key("enter"))
	if done == nil || done.provider != "anthropic" || done.model != "claude-opus-5" {
		t.Fatalf("pick = %+v", done)
	}
}

func TestModelFormManualEntry(t *testing.T) {
	f := newModelForm("deepseek", "deepseek-v4-flash")
	f.setGroups([]modelProviderGroup{
		{provider: "deepseek", models: []string{"deepseek-v4-flash"}},
	})
	f.idx = len(f.rows) - 1 // fila manual
	if _, _ = f.update(key("enter")); !f.manual {
		t.Fatal("enter en la fila manual debería activar el input")
	}
	for _, r := range "modelo-nuevo" {
		f.update(key(string(r)))
	}
	done, _ := f.update(key("enter"))
	if done == nil || done.provider != "deepseek" || done.model != "modelo-nuevo" {
		t.Fatalf("pick manual = %+v", done)
	}
}

func TestModelFormEscCancels(t *testing.T) {
	f := newModelForm("deepseek", "x")
	if _, c := f.update(key("esc")); !c {
		t.Fatal("esc debería cancelar")
	}
}

// bigForm builds a picker with n model rows across two providers plus manual.
func bigForm(t *testing.T, n int) *modelForm {
	t.Helper()
	half := make([]string, n/2)
	for i := range half {
		half[i] = "m" + itoa(i)
	}
	f := newModelForm("deepseek", "m0")
	f.setGroups([]modelProviderGroup{
		{provider: "deepseek", models: half},
		{provider: "anthropic", models: half},
	})
	return f
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestModelFormWindowKeepsSelectionVisible(t *testing.T) {
	f := bigForm(t, 40)
	const win = 10
	for _, idx := range []int{0, 5, 19, 20, 38, len(f.rows) - 1} {
		f.idx = idx
		top, end := f.visibleWindow(win)
		if idx < top || idx >= end {
			t.Fatalf("idx %d fuera de la ventana [%d,%d)", idx, top, end)
		}
		if end-top != win {
			t.Fatalf("ventana de %d filas, esperaba %d (idx %d)", end-top, win, idx)
		}
	}
}

func TestModelFormWindowShorterThanList(t *testing.T) {
	f := bigForm(t, 4) // 4 modelos + manual = 5 filas
	top, end := f.visibleWindow(20)
	if top != 0 || end != len(f.rows) {
		t.Fatalf("con ventana grande debería mostrar todo: [%d,%d) de %d", top, end, len(f.rows))
	}
}

func TestModelFormViewScrollsAndAligns(t *testing.T) {
	f := bigForm(t, 40)
	s := DefaultTheme().Styles()

	// Cerca del final: la vista muestra el modelo elegido y el marcador "↑".
	f.idx = len(f.rows) - 2
	out := f.view(s, 8)
	if !strings.Contains(out, f.rows[f.idx].model) {
		t.Fatalf("la vista no muestra el modelo seleccionado:\n%s", out)
	}
	if !strings.Contains(out, "↑") {
		t.Fatalf("esperaba el marcador de scroll hacia arriba:\n%s", out)
	}
	if strings.Count(out, "\n") > 20 {
		t.Fatalf("la vista sigue siendo demasiado alta (%d líneas)", strings.Count(out, "\n"))
	}

	// Cerca del inicio: marcador "↓".
	f.idx = 0
	if out := f.view(s, 8); !strings.Contains(out, "↓") {
		t.Fatalf("esperaba el marcador de scroll hacia abajo:\n%s", out)
	}
}
