package tool

import (
	"context"
	"encoding/json"
	"testing"
)

// stubTool is a minimal Tool for exercising the registry.
type stubTool struct{ name string }

func (s stubTool) Name() string                                           { return s.name }
func (stubTool) Description() string                                      { return "stub" }
func (stubTool) InputSchema() map[string]any                              { return map[string]any{"type": "object"} }
func (stubTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }

func TestRegistryRegistersAndLooksUp(t *testing.T) {
	r := NewRegistry(stubTool{"a"}, stubTool{"b"})

	if got, ok := r.Get("a"); !ok || got.Name() != "a" {
		t.Fatalf("Get(a) = %v, %v", got, ok)
	}
	if _, ok := r.Get("nope"); ok {
		t.Fatal("Get de un nombre desconocido debería fallar")
	}
}

func TestRegistryPreservesOrderAndDropsLaterDuplicates(t *testing.T) {
	r := NewRegistry(
		stubTool{"first"},
		stubTool{"second"},
		stubTool{"first"}, // duplicado: se ignora
		stubTool{"third"},
	)

	got := make([]string, 0, 3)
	for _, tl := range r.All() {
		got = append(got, tl.Name())
	}
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("All() = %v, quería %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("All()[%d] = %q, quería %q", i, got[i], want[i])
		}
	}
}

func TestRegistrySubsetKeepsOrderAndSkipsUnknown(t *testing.T) {
	r := NewRegistry(stubTool{"a"}, stubTool{"b"}, stubTool{"c"})
	sub := r.Subset("c", "a", "desconocida")

	got := make([]string, 0, 2)
	for _, tl := range sub.All() {
		got = append(got, tl.Name())
	}
	if len(got) != 2 || got[0] != "c" || got[1] != "a" {
		t.Fatalf("Subset = %v, quería [c a]", got)
	}
}

func TestRegistryWithAppends(t *testing.T) {
	base := NewRegistry(stubTool{"a"})
	ext := base.With(stubTool{"b"})

	if _, ok := ext.Get("b"); !ok {
		t.Fatal("With no agregó la tool nueva")
	}
	if _, ok := base.Get("b"); ok {
		t.Fatal("With mutó el registry original")
	}
}
