package memory

import (
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	s, err := NewFileStore(filepath.Join(t.TempDir(), "mem", "notes.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddAndAll(t *testing.T) {
	s := newStore(t)

	if _, err := s.Add("el deploy va por GitHub Actions", []string{"Deploy", "ci", "deploy"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("la DB es postgres 16", []string{"db"}); err != nil {
		t.Fatal(err)
	}

	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("notas = %d, quiero 2", len(all))
	}
	if all[0].Text != "la DB es postgres 16" {
		t.Errorf("orden: quiero la más nueva primero, tengo %q", all[0].Text)
	}
	// tags: minúscula y sin duplicados
	if want := []string{"deploy", "ci"}; len(all[1].Tags) != 2 || all[1].Tags[0] != want[0] || all[1].Tags[1] != want[1] {
		t.Errorf("tags mal normalizados: %v", all[1].Tags)
	}
}

func TestAddRejectsEmpty(t *testing.T) {
	if _, err := newStore(t).Add("   ", nil); err == nil {
		t.Fatal("esperaba error por texto vacío")
	}
}

func TestSearch(t *testing.T) {
	s := newStore(t)
	_, _ = s.Add("el deploy va por GitHub Actions", []string{"deploy"})
	_, _ = s.Add("la DB es postgres 16", []string{"db"})
	_, _ = s.Add("el linter es golangci-lint", []string{"ci"})

	t.Run("por término", func(t *testing.T) {
		got, err := s.Search("deploy", nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Text != "el deploy va por GitHub Actions" {
			t.Fatalf("resultado = %+v", got)
		}
	})

	t.Run("todos los términos deben aparecer", func(t *testing.T) {
		if got, _ := s.Search("deploy postgres", nil, 0); len(got) != 0 {
			t.Fatalf("no debería matchear: %+v", got)
		}
	})

	t.Run("por tag", func(t *testing.T) {
		got, _ := s.Search("", []string{"db"}, 0)
		if len(got) != 1 || got[0].Tags[0] != "db" {
			t.Fatalf("resultado = %+v", got)
		}
	})

	t.Run("limit", func(t *testing.T) {
		if got, _ := s.Search("", nil, 2); len(got) != 2 {
			t.Fatalf("con limit 2 quiero 2, tengo %d", len(got))
		}
	})

	t.Run("sin resultados", func(t *testing.T) {
		if got, _ := s.Search("kubernetes", nil, 0); len(got) != 0 {
			t.Fatalf("quiero 0, tengo %d", len(got))
		}
	})
}
