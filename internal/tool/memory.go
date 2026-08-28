package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/memory"
)

// Remember saves a fact into the harness's persistent memory.
type Remember struct {
	Store memory.Store
}

func (Remember) Name() string { return "remember" }

func (Remember) Description() string {
	return "Guarda un dato importante en la memoria persistente del arnés para recuperarlo " +
		"en sesiones futuras: decisiones, convenciones, preferencias del usuario, datos del " +
		"proyecto. Poné tags cortos para poder buscarlo después."
}

func (Remember) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string", "description": "El dato a recordar, en una o dos frases."},
			"tags": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Etiquetas cortas para clasificar y luego encontrar el dato.",
			},
		},
		"required": []string{"text"},
	}
}

func (r Remember) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Text string   `json:"text"`
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	note, err := r.Store.Add(in.Text, in.Tags)
	if err != nil {
		return "", err
	}
	return "recordado (id " + note.ID + ")", nil
}

// Recall searches the harness's persistent memory.
type Recall struct {
	Store memory.Store
}

func (Recall) Name() string { return "recall" }

func (Recall) Description() string {
	return "Busca en la memoria persistente del arnés por texto y/o tags. Usalo cuando el " +
		"usuario haga referencia a algo de una conversación anterior o pregunte qué se hizo " +
		"o decidió antes."
}

func (Recall) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Términos a buscar; todos deben aparecer en la nota."},
			"tags":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Filtrar por al menos uno de estos tags."},
			"limit": map[string]any{"type": "integer", "description": "Máximo de resultados (default 10)."},
		},
		"required": []string{},
	}
}

func (r Recall) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Query string   `json:"query"`
		Tags  []string `json:"tags"`
		Limit int      `json:"limit"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	notes, err := r.Store.Search(in.Query, in.Tags, in.Limit)
	if err != nil {
		return "", err
	}
	if len(notes) == 0 {
		return "sin resultados en la memoria", nil
	}
	var b strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&b, "- [%s] %s", n.CreatedAt.Format("2006-01-02"), n.Text)
		if len(n.Tags) > 0 {
			fmt.Fprintf(&b, " (tags: %s)", strings.Join(n.Tags, ", "))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
