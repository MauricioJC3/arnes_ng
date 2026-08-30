package app

import (
	"context"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

func TestFirstUserText(t *testing.T) {
	tests := []struct {
		name string
		in   []provider.Message
		want string
	}{
		{"vacío", nil, ""},
		{
			"primer turno de usuario real",
			[]provider.Message{
				{Role: provider.RoleUser, Text: "  arreglá el bug del login  "},
				{Role: provider.RoleAssistant, Text: "ok"},
			},
			"arreglá el bug del login",
		},
		{
			"saltea turnos que sólo llevan tool results",
			[]provider.Message{
				{Role: provider.RoleUser, ToolResults: []provider.ToolResult{{CallID: "c1", Content: "x"}}},
				{Role: provider.RoleUser, Text: "esta es la tarea"},
			},
			"esta es la tarea",
		},
		{
			"sin texto de usuario",
			[]provider.Message{{Role: provider.RoleAssistant, Text: "hola"}},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstUserText(tt.in); got != tt.want {
				t.Fatalf("firstUserText = %q, quiero %q", got, tt.want)
			}
		})
	}
}

func TestAnchorText(t *testing.T) {
	t.Run("sin tarea ni checklist devuelve vacío", func(t *testing.T) {
		a := &App{}
		if got := a.anchorText(); got != "" {
			t.Fatalf("anchorText = %q, quiero vacío", got)
		}
	})

	t.Run("incluye la tarea original y el plan con marcas de estado", func(t *testing.T) {
		store := todo.NewStore()
		store.Set([]todo.Item{
			{Content: "escribir el handler", Status: todo.Done},
			{Content: "conectar el middleware", Status: todo.InProgress},
			{Content: "agregar el test", Status: todo.Pending},
		})
		a := &App{firstTask: "armá la API de invoices", todos: store}

		got := a.anchorText()
		for _, want := range []string{
			"# Tarea original de la sesión", "armá la API de invoices",
			"# Plan actual (checklist)",
			"- [x] escribir el handler",
			"- [~] conectar el middleware",
			"- [ ] agregar el test",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("anchorText no contiene %q:\n%s", want, got)
			}
		}
	})
}

func TestOpenTodos(t *testing.T) {
	store := todo.NewStore()
	store.Set([]todo.Item{
		{Content: "a", Status: todo.Done},
		{Content: "b", Status: todo.Pending},
		{Content: "c", Status: todo.InProgress},
	})
	a := &App{todos: store}

	got := a.openTodos()
	if strings.Join(got, ",") != "b,c" {
		t.Fatalf("openTodos = %v, quiero [b c]", got)
	}

	if (&App{}).openTodos() != nil {
		t.Fatal("openTodos sin store debería ser nil")
	}
}

func TestVerifier(t *testing.T) {
	if (&App{}).verifier() != nil {
		t.Fatal("sin check_command el verificador debe ser nil")
	}

	if testing.Short() {
		t.Skip("-short: el verificador ejecuta un comando de shell")
	}

	t.Run("comando que pasa", func(t *testing.T) {
		v := (&App{checkCommand: "true"}).verifier()
		out, ok := v(context.Background())
		if !ok {
			t.Fatalf("esperaba ok=true, out=%q", out)
		}
	})

	t.Run("comando que falla devuelve la salida y ok=false", func(t *testing.T) {
		v := (&App{checkCommand: "echo fallo-de-build >&2; exit 1"}).verifier()
		out, ok := v(context.Background())
		if ok {
			t.Fatal("esperaba ok=false")
		}
		if !strings.Contains(out, "fallo-de-build") {
			t.Fatalf("la salida no llegó: %q", out)
		}
	})
}
