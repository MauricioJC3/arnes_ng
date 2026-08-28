package tool

import (
	"encoding/json"
	"testing"
)

// everyTool is one instance of each capability the harness ships, built with
// nil/zero dependencies (the metadata methods never touch them).
func everyTool() []Tool {
	return []Tool{
		Bash{}, Grep{}, Glob{}, ReadFile{}, WriteFile{}, EditFile{},
		TodoWrite{}, LSP{}, Skill{}, Remember{}, Recall{},
	}
}

func TestToolContract(t *testing.T) {
	seen := map[string]bool{}

	for _, tl := range everyTool() {
		name := tl.Name()
		if name == "" {
			t.Errorf("%T.Name() está vacío", tl)
			continue
		}
		if seen[name] {
			t.Errorf("nombre de tool duplicado: %q", name)
		}
		seen[name] = true

		if len(tl.Description()) < 15 {
			t.Errorf("%q.Description() es demasiado corta: %q", name, tl.Description())
		}

		schema := tl.InputSchema()
		if len(schema) == 0 {
			t.Errorf("%q.InputSchema() está vacío", name)
			continue
		}
		if _, err := json.Marshal(schema); err != nil {
			t.Errorf("%q.InputSchema() no serializa a JSON: %v", name, err)
		}
		if schema["type"] != "object" {
			t.Errorf("%q.InputSchema()[\"type\"] = %v, se espera \"object\"", name, schema["type"])
		}
	}
}

func TestEveryBaseToolNameIsRegistrable(t *testing.T) {
	r := NewRegistry(everyTool()...)
	if n := len(r.All()); n != len(everyTool()) {
		t.Fatalf("el registry quedó con %d tools de %d (¿nombres colisionando?)", n, len(everyTool()))
	}
	for _, want := range []string{
		"bash", "grep", "glob", "read_file", "write_file", "edit_file",
		"todo_write", "lsp", "skill", "remember", "recall",
	} {
		if _, ok := r.Get(want); !ok {
			t.Errorf("falta la tool %q", want)
		}
	}
}
