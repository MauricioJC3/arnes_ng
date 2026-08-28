package subagent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistry(t *testing.T) {
	r := NewRegistry(
		Definition{Name: "a", Description: "primero"},
		Definition{Name: "", Description: "sin nombre, se ignora"},
		Definition{Name: "b", Description: "segundo"},
		Definition{Name: "a", Description: "duplicado, se ignora"},
	)
	if r.Len() != 2 {
		t.Fatalf("Len = %d, quiero 2", r.Len())
	}
	all := r.All()
	if all[0].Name != "a" || all[1].Name != "b" {
		t.Fatalf("orden inesperado: %v", all)
	}
	if d, ok := r.Get("a"); !ok || d.Description != "primero" {
		t.Fatalf("Get(a) = %+v %v (el duplicado no debería pisar)", d, ok)
	}
	if _, ok := r.Get("nope"); ok {
		t.Fatal("Get de un nombre inexistente devolvió ok")
	}
}

func TestLoadFile(t *testing.T) {
	t.Run("archivo ausente devuelve los defaults", func(t *testing.T) {
		defs, err := LoadFile(filepath.Join(t.TempDir(), "no-existe.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(defs) != len(Defaults()) {
			t.Fatalf("quiero los %d defaults, tengo %d", len(Defaults()), len(defs))
		}
	})

	t.Run("JSON válido se parsea", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "subagents.json")
		body := `[{"name":"docs","description":"escribe docs","system":"sos un escritor","tools":["read_file"]}]`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		defs, err := LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(defs) != 1 || defs[0].Name != "docs" || defs[0].Tools[0] != "read_file" {
			t.Fatalf("parseo inesperado: %+v", defs)
		}
	})

	t.Run("JSON roto es error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "x.json")
		_ = os.WriteFile(path, []byte("{no json"), 0o644)
		if _, err := LoadFile(path); err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("falta system es error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "x.json")
		_ = os.WriteFile(path, []byte(`[{"name":"x"}]`), 0o644)
		if _, err := LoadFile(path); err == nil {
			t.Fatal("esperaba error por 'system' faltante")
		}
	})
}
