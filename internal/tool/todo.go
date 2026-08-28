package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

// TodoWrite lets the model maintain the task checklist the TUI shows live.
type TodoWrite struct {
	Store *todo.Store
}

func (TodoWrite) Name() string { return "todo_write" }

func (TodoWrite) Description() string {
	return "Mantené la lista de tareas del trabajo actual. Pasás SIEMPRE la lista completa: " +
		"cada ítem es {content, status} con status pending | in_progress | completed. " +
		"Usalo para planificar tareas de varios pasos y para ir tildando el progreso a la " +
		"vista del usuario. Tené un solo ítem in_progress a la vez. Para tareas triviales de " +
		"un paso no hace falta."
}

func (TodoWrite) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"type":        "array",
				"description": "La lista COMPLETA de tareas, en orden.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{"type": "string", "description": "Qué hay que hacer, en una frase."},
						"status": map[string]any{
							"type":        "string",
							"enum":        []string{"pending", "in_progress", "completed"},
							"description": "Estado del ítem.",
						},
					},
					"required": []string{"content", "status"},
				},
			},
		},
		"required": []string{"todos"},
	}
}

func (t TodoWrite) Execute(_ context.Context, input json.RawMessage) (string, error) {
	if t.Store == nil {
		return "", errors.New("todo_write no está disponible en este arnés")
	}
	var in struct {
		Todos []todo.Item `json:"todos"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}

	inProgress := 0
	for i, it := range in.Todos {
		if strings.TrimSpace(it.Content) == "" {
			return "", fmt.Errorf("el ítem %d no tiene 'content'", i+1)
		}
		if !it.Status.Valid() {
			return "", fmt.Errorf("el ítem %d tiene un status inválido: %q (pending|in_progress|completed)", i+1, it.Status)
		}
		if it.Status == todo.InProgress {
			inProgress++
		}
	}
	if inProgress > 1 {
		return "", fmt.Errorf("hay %d ítems in_progress; dejá uno solo", inProgress)
	}

	t.Store.Set(in.Todos)

	done, total := t.Store.Counts()
	var b strings.Builder
	fmt.Fprintf(&b, "tareas: %d/%d\n", done, total)
	for _, it := range in.Todos {
		b.WriteString(mark(it.Status))
		b.WriteByte(' ')
		b.WriteString(it.Content)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func mark(s todo.Status) string {
	switch s {
	case todo.Done:
		return "[x]"
	case todo.InProgress:
		return "[~]"
	default:
		return "[ ]"
	}
}
