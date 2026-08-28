package tool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andresmjimenez/arnes/internal/memory"
)

func newMemStore(t *testing.T) memory.Store {
	t.Helper()
	s, err := memory.NewFileStore(filepath.Join(t.TempDir(), "notes.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRememberAndRecall(t *testing.T) {
	store := newMemStore(t)

	out, err := Remember{Store: store}.Execute(context.Background(), mustJSON(t, map[string]any{
		"text": "el proyecto usa arquitectura hexagonal",
		"tags": []string{"arquitectura"},
	}))
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if !strings.Contains(out, "recordado") {
		t.Errorf("out = %q", out)
	}

	got, err := Recall{Store: store}.Execute(context.Background(), mustJSON(t, map[string]any{"query": "hexagonal"}))
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if !strings.Contains(got, "arquitectura hexagonal") || !strings.Contains(got, "tags: arquitectura") {
		t.Errorf("recall no encontró la nota: %q", got)
	}
}

func TestRecallNoResults(t *testing.T) {
	got, err := Recall{Store: newMemStore(t)}.Execute(context.Background(), json.RawMessage(`{"query":"nada"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "sin resultados") {
		t.Errorf("out = %q", got)
	}
}

func TestRememberRejectsEmptyText(t *testing.T) {
	if _, err := (Remember{Store: newMemStore(t)}).Execute(context.Background(), json.RawMessage(`{"text":""}`)); err == nil {
		t.Fatal("esperaba error por texto vacío")
	}
}
