package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	s, err := NewFileStore(filepath.Join(t.TempDir(), "mem", "notes.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newScopedStore(t *testing.T, path, project string) *FileStore {
	t.Helper()
	s, err := NewFileStore(path, project)
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

func TestProjectScoping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.json")

	// Nota vieja sin proyecto (pre-scoping): la ve todo el mundo.
	legacy := newScopedStore(t, path, "")
	if _, err := legacy.Add("nota global vieja", nil); err != nil {
		t.Fatal(err)
	}

	a := newScopedStore(t, path, "owner/proj-a")
	b := newScopedStore(t, path, "owner/proj-b")
	if _, err := a.Add("solo de A", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Add("solo de B", nil); err != nil {
		t.Fatal(err)
	}

	got, _ := a.All()
	if len(got) != 2 {
		t.Fatalf("A debería ver su nota + la global, tiene %d: %+v", len(got), got)
	}
	for _, n := range got {
		if n.Text == "solo de B" {
			t.Fatal("A no debería ver notas de B")
		}
	}
	if s, _ := a.Search("solo", nil, 0); len(s) != 1 || s[0].Text != "solo de A" {
		t.Fatalf("Search de A cruzó proyectos: %+v", s)
	}

	// Un store sin scoping ve todo.
	if all, _ := newScopedStore(t, path, "").All(); len(all) != 3 {
		t.Fatalf("sin scoping quiero 3, tengo %d", len(all))
	}
}

func TestDigest(t *testing.T) {
	s := newStore(t)
	if Digest(s, 15) != "" {
		t.Fatal("digest de un store vacío debería ser \"\"")
	}
	_, _ = s.Add("la config vive en ~/.foo", []string{"config"})
	_, _ = s.Add("se usa postgres 16", nil)

	d := Digest(s, 15)
	if !strings.Contains(d, "## Memoria del proyecto") ||
		!strings.Contains(d, "se usa postgres 16") ||
		!strings.Contains(d, "la config vive en ~/.foo") ||
		!strings.Contains(d, "(tags: config)") {
		t.Fatalf("digest incompleto:\n%s", d)
	}

	// respeta el tope
	for i := 0; i < 30; i++ {
		_, _ = s.Add("relleno", nil)
	}
	if n := strings.Count(Digest(s, 5), "\n- "); n != 5 {
		t.Fatalf("con maxNotes 5 quiero 5 ítems, tengo %d", n)
	}
}
