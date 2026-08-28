package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

func TestHumanTokens(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0t"},
		{999, "999t"},
		{1000, "1.0k"},
		{12345, "12.3k"},
		{100_000, "100k"},
		{2_500_000, "2500k"},
	}
	for _, c := range cases {
		if got := humanTokens(c.n); got != c.want {
			t.Errorf("humanTokens(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("abcdefghijklmnopqrst"); got != "abcdefghijklm" {
		t.Fatalf("shortID de un id largo = %q", got)
	}
	if got := shortID("corto"); got != "corto" {
		t.Fatalf("shortID de un id corto lo cambió: %q", got)
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("  hola\nmundo  ", 100); got != "hola mundo" {
		t.Fatalf("oneLine sin truncar = %q", got)
	}
	if got := oneLine("palabra bastante larga", 6); got != "palab…" {
		t.Fatalf("oneLine truncado = %q", got)
	}
}

func TestBoxWidth(t *testing.T) {
	var m Model
	m.width = 3
	if got := m.boxWidth(); got != 2 {
		t.Fatalf("boxWidth con ancho chico = %d, quería 2", got)
	}
	m.width = 80
	if got := m.boxWidth(); got != 78 {
		t.Fatalf("boxWidth normal = %d, quería 78", got)
	}
}

func TestWrapToZeroWidth(t *testing.T) {
	if got := wrapTo("hola", 0); got != "hola" {
		t.Fatalf("wrapTo con w=0 debería devolver el texto crudo, dio %q", got)
	}
}

func modelWithTodos(t *testing.T) Model {
	t.Helper()
	m := New(Config{
		Conv:      &fakeConv{},
		Provider:  provider.NewMock(),
		SessionID: func() string { return "s" },
		Approvals: make(chan approval.Request),
		Todos:     make(chan []todo.Item, 1),
		Theme:     DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	return tm.(Model)
}

func TestTodoPanelRendersEveryStatus(t *testing.T) {
	m := modelWithTodos(t)
	items := []todo.Item{
		{Content: "diseñar la API", Status: todo.Done},
		{Content: "implementar el handler", Status: todo.InProgress},
		{Content: "escribir los tests", Status: todo.Pending},
	}
	tm, cmd := m.Update(todosMsg(items))
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("todosMsg debería re-armar el waiter")
	}

	v := m.View()
	for _, want := range []string{"tareas", "1/3", "diseñar la API", "implementar el handler", "escribir los tests"} {
		if !strings.Contains(v, want) {
			t.Fatalf("el panel de tareas no muestra %q:\n%s", want, v)
		}
	}
}

func TestTodoPanelTruncatesLongList(t *testing.T) {
	m := modelWithTodos(t)
	var items []todo.Item
	for i := 0; i < 12; i++ {
		items = append(items, todo.Item{Content: "una tarea más", Status: todo.Pending})
	}
	tm, _ := m.Update(todosMsg(items))
	m = tm.(Model)
	if !strings.Contains(m.View(), "más") {
		t.Fatalf("una lista de 12 debería colapsar en '… +N más':\n%s", m.View())
	}
}

func TestMenuViewRendersSuggestions(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	m.menu.update("/")
	if !m.menu.open || len(m.menu.items) == 0 {
		t.Fatal("'/' debería abrir el menú con todos los comandos")
	}
	first := m.menu.items[0].Name
	if !strings.Contains(m.menuView(), first) {
		t.Fatalf("menuView no muestra el primer comando %q:\n%s", first, m.menuView())
	}
	if !strings.Contains(m.View(), first) {
		t.Fatalf("View() no incluye el menú abierto:\n%s", m.View())
	}
}

func TestStatusBarShowsModeBadges(t *testing.T) {
	c := &fakeModeConv{mode: "auto"}
	m := New(Config{
		Conv: c, Provider: provider.NewMock(),
		SessionID: func() string { return "s" },
		Approvals: make(chan approval.Request), Theme: DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = tm.(Model)
	if !strings.Contains(m.View(), "auto") {
		t.Fatalf("no muestra el badge de modo auto:\n%s", m.View())
	}
	c.mode = "plan"
	if !strings.Contains(m.View(), "plan") {
		t.Fatalf("no muestra el badge de modo plan:\n%s", m.View())
	}
}

func TestStatusBarShowsQuitHint(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // arma el aviso de salida
	m = tm.(Model)
	if !strings.Contains(m.View(), "Esc de nuevo") {
		t.Fatalf("la status bar no muestra el aviso de salida:\n%s", m.View())
	}
}
