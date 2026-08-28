package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These exercise the ripgrep-less fallback directly, so they don't depend on
// whether `rg` is on PATH.

func TestGrepWalkFindsTextAndSkipsBinariesAndVendorDirs(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel string, body []byte) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("a.txt", []byte("hola\nmundo cruel\n"))
	mk("b.bin", []byte{0x00, 0x01, 'm', 'u', 'n', 'd', 'o', 0x00})
	mk(".git/c.txt", []byte("mundo\n"))
	mk("node_modules/d.txt", []byte("mundo\n"))

	out, err := grepWalk("mundo", dir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.txt:2:") {
		t.Fatalf("no encontró el match en el archivo de texto:\n%s", out)
	}
	for _, forbidden := range []string{"b.bin", ".git", "node_modules"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("no debería reportar %q:\n%s", forbidden, out)
		}
	}
}

func TestGrepWalkGlobFilterAndCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "keep.go"), []byte("VALOR\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "skip.md"), []byte("valor\n"), 0o644)

	out, err := grepWalk("valor", dir, "*.go", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "keep.go") || strings.Contains(out, "skip.md") {
		t.Fatalf("el glob / ignore_case no filtró bien:\n%s", out)
	}
}

func TestGrepWalkNoMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hola\n"), 0o644)

	out, err := grepWalk("zzz-no-esta", dir, "", false)
	if err != nil || out != "sin coincidencias" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestGrepWalkInvalidRegex(t *testing.T) {
	if _, err := grepWalk("(", ".", "", false); err == nil {
		t.Fatal("esperaba error con una regex inválida")
	}
}

func TestIsBinary(t *testing.T) {
	if !isBinary([]byte{'a', 0x00, 'b'}) {
		t.Fatal("un byte NUL debería marcar el contenido como binario")
	}
	if isBinary([]byte("texto normal utf-8 áéí")) {
		t.Fatal("texto plano no es binario")
	}
}

func TestClampLines(t *testing.T) {
	if got := clampLines("una\ndos\ntres", 5); got != "una\ndos\ntres" {
		t.Fatalf("bajo el límite no debería tocar nada: %q", got)
	}
	got := clampLines("a\nb\nc\nd", 2)
	if !strings.HasPrefix(got, "a\nb") || !strings.Contains(got, "cortado en 2") {
		t.Fatalf("no truncó como se espera: %q", got)
	}
}
