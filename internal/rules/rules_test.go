package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Run("sin archivo devuelve vacío", func(t *testing.T) {
		text, src, err := Load(t.TempDir(), "")
		if err != nil || text != "" || src != "" {
			t.Fatalf("text=%q src=%q err=%v", text, src, err)
		}
	})

	t.Run("toma AGENTS.md del directorio", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("  usá tabs, no espacios  "), 0o644); err != nil {
			t.Fatal(err)
		}
		text, src, err := Load(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		if text != "usá tabs, no espacios" || src != "AGENTS.md" {
			t.Fatalf("text=%q src=%q", text, src)
		}
	})

	t.Run("prioriza AGENTS.md sobre agent.md", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("primero"), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "agent.md"), []byte("segundo"), 0o644)
		text, src, _ := Load(dir, "")
		if text != "primero" || src != "AGENTS.md" {
			t.Fatalf("text=%q src=%q", text, src)
		}
	})

	t.Run("el override gana", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("del dir"), 0o644)
		other := filepath.Join(t.TempDir(), "mis-reglas.md")
		_ = os.WriteFile(other, []byte("override"), 0o644)
		text, src, _ := Load(dir, other)
		if text != "override" || src != other {
			t.Fatalf("text=%q src=%q", text, src)
		}
	})

	t.Run("override inexistente es error", func(t *testing.T) {
		if _, _, err := Load(t.TempDir(), "/no/existe.md"); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestWrap(t *testing.T) {
	if Wrap("  ", "x") != "" {
		t.Error("reglas vacías deberían dar cadena vacía")
	}
	got := Wrap("no borres tests", "AGENTS.md")
	if got == "" || !contains(got, "AGENTS.md") || !contains(got, "no borres tests") {
		t.Fatalf("Wrap = %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
