package tui

import (
	"strings"
	"testing"
)

func TestMarkdownStyle(t *testing.T) {
	t.Setenv("ARNES_MD_STYLE", "")
	if markdownStyle() != "dark" {
		t.Fatalf("default = %q, quiero dark", markdownStyle())
	}
	t.Setenv("ARNES_MD_STYLE", "light")
	if markdownStyle() != "light" {
		t.Fatalf("override = %q", markdownStyle())
	}
	// "auto" queries the terminal and leaks into the input -- never honored.
	t.Setenv("ARNES_MD_STYLE", "auto")
	if markdownStyle() != "dark" {
		t.Fatalf("auto no debería aceptarse, dio %q", markdownStyle())
	}
}

func TestMouseEnabledDefaultsOff(t *testing.T) {
	t.Setenv("ARNES_MOUSE", "")
	if mouseEnabled() {
		t.Fatal("el mouse debería estar OFF por defecto (para poder copiar)")
	}
	t.Setenv("ARNES_MOUSE", "on")
	if !mouseEnabled() {
		t.Fatal("ARNES_MOUSE=on debería activarlo")
	}
}

func TestMarkdownRendererUsesExplicitStyle(t *testing.T) {
	// A model with an explicit style renders markdown without touching the
	// terminal (glamour's auto style would query stdin).
	m := New(Config{Theme: DefaultTheme(), MarkdownStyle: "dark"})
	m.vp.Width = 80
	out := m.markdown("# hola\n\ntexto **fuerte**")
	if strings.Contains(out, "]11;") {
		t.Fatalf("la salida contiene una respuesta OSC del terminal: %q", out)
	}
	if out == "" {
		t.Fatal("markdown devolvió vacío")
	}
}
