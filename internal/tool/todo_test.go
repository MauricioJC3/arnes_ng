package tool

import (
	"context"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

func TestTodoWrite(t *testing.T) {
	ctx := context.Background()

	t.Run("guarda la lista y devuelve el resumen", func(t *testing.T) {
		st := todo.NewStore()
		out, err := (TodoWrite{Store: st}).Execute(ctx, mustJSON(t, map[string]any{
			"todos": []any{
				map[string]any{"content": "leer el código", "status": "completed"},
				map[string]any{"content": "escribir el fix", "status": "in_progress"},
				map[string]any{"content": "correr tests", "status": "pending"},
			},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "tareas: 1/3") {
			t.Errorf("resumen = %q", out)
		}
		got := st.Get()
		if len(got) != 3 || got[1].Status != todo.InProgress {
			t.Fatalf("store = %+v", got)
		}
	})

	t.Run("status inválido es error", func(t *testing.T) {
		st := todo.NewStore()
		_, err := (TodoWrite{Store: st}).Execute(ctx, mustJSON(t, map[string]any{
			"todos": []any{map[string]any{"content": "x", "status": "doing"}},
		}))
		if err == nil || !strings.Contains(err.Error(), "status inválido") {
			t.Fatalf("esperaba error de status, tengo: %v", err)
		}
	})

	t.Run("content vacío es error", func(t *testing.T) {
		st := todo.NewStore()
		_, err := (TodoWrite{Store: st}).Execute(ctx, mustJSON(t, map[string]any{
			"todos": []any{map[string]any{"content": "  ", "status": "pending"}},
		}))
		if err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("más de un in_progress es error", func(t *testing.T) {
		st := todo.NewStore()
		_, err := (TodoWrite{Store: st}).Execute(ctx, mustJSON(t, map[string]any{
			"todos": []any{
				map[string]any{"content": "a", "status": "in_progress"},
				map[string]any{"content": "b", "status": "in_progress"},
			},
		}))
		if err == nil || !strings.Contains(err.Error(), "in_progress") {
			t.Fatalf("esperaba error, tengo: %v", err)
		}
	})

	t.Run("sin store es error", func(t *testing.T) {
		_, err := (TodoWrite{}).Execute(ctx, mustJSON(t, map[string]any{
			"todos": []any{map[string]any{"content": "a", "status": "pending"}},
		}))
		if err == nil {
			t.Fatal("esperaba error")
		}
	})
}
