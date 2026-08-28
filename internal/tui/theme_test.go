package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTheme(t *testing.T) {
	t.Run("archivo ausente = defaults", func(t *testing.T) {
		got, err := LoadTheme(filepath.Join(t.TempDir(), "nope.json"))
		if err != nil {
			t.Fatal(err)
		}
		if got != DefaultTheme() {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("campos vacíos caen al default", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "theme.json")
		if err := os.WriteFile(path, []byte(`{"accent":"#ff0000"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := LoadTheme(path)
		if err != nil {
			t.Fatal(err)
		}
		if got.Accent != "#ff0000" {
			t.Errorf("Accent = %q", got.Accent)
		}
		if got.User != DefaultTheme().User {
			t.Errorf("User no cayó al default: %q", got.User)
		}
	})

	t.Run("JSON roto es error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "theme.json")
		_ = os.WriteFile(path, []byte("{roto"), 0o644)
		if _, err := LoadTheme(path); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestThemeStyles(t *testing.T) {
	s := DefaultTheme().Styles()
	if got := s.User.Render("x"); got == "" {
		t.Fatal("el estilo User no renderiza nada")
	}
}
