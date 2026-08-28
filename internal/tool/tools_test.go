package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hola.txt")
	if err := os.WriteFile(path, []byte("contenido"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("lee un archivo existente", func(t *testing.T) {
		got, err := ReadFile{}.Execute(context.Background(), mustJSON(t, map[string]string{"path": path}))
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if got != "contenido" {
			t.Fatalf("contenido = %q", got)
		}
	})

	t.Run("archivo inexistente devuelve error", func(t *testing.T) {
		_, err := ReadFile{}.Execute(context.Background(), mustJSON(t, map[string]string{"path": filepath.Join(dir, "nope")}))
		if err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("path vacío devuelve error", func(t *testing.T) {
		if _, err := (ReadFile{}).Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nuevo.txt")

	got, err := WriteFile{}.Execute(context.Background(), mustJSON(t, map[string]string{"path": path, "content": "hola mundo"}))
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if !strings.Contains(got, "10 bytes") {
		t.Errorf("resumen inesperado: %q", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "hola mundo" {
		t.Fatalf("el archivo no quedó bien escrito: %q (err %v)", data, err)
	}
}

func TestBash(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short: ejecuta comandos reales")
	}

	t.Run("captura stdout", func(t *testing.T) {
		got, err := Bash{}.Execute(context.Background(), mustJSON(t, map[string]string{"command": "echo hola"}))
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if strings.TrimSpace(got) != "hola" {
			t.Fatalf("stdout = %q", got)
		}
	})

	t.Run("un exit code != 0 no es error de Go, pero se reporta en la salida", func(t *testing.T) {
		got, err := Bash{}.Execute(context.Background(), mustJSON(t, map[string]string{"command": "echo antes; exit 3"}))
		if err != nil {
			t.Fatalf("la tool no debería devolver error de Go por un exit code: %v", err)
		}
		if !strings.Contains(got, "antes") || !strings.Contains(got, "error") {
			t.Fatalf("salida = %q, quiero que incluya stdout y la nota de error", got)
		}
	})

	t.Run("command vacío devuelve error", func(t *testing.T) {
		if _, err := (Bash{}).Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
			t.Fatal("esperaba error")
		}
	})
}
