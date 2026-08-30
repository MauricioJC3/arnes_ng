package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	t.Run("un comando que excede el timeout se cancela y lo reporta", func(t *testing.T) {
		start := time.Now()
		got, err := Bash{Timeout: 200 * time.Millisecond}.Execute(
			context.Background(), mustJSON(t, map[string]string{"command": "sleep 5"}))
		if err != nil {
			t.Fatalf("la tool no debería devolver error de Go por un timeout: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("el comando siguió corriendo %s, debería haberse cortado enseguida", elapsed)
		}
		if !strings.Contains(got, "límite de tiempo") {
			t.Fatalf("salida = %q, quiero la nota de timeout", got)
		}
	})

	t.Run("timeout_seconds del input pisa el default de la tool", func(t *testing.T) {
		start := time.Now()
		got, err := Bash{Timeout: 30 * time.Second}.Execute(
			context.Background(), mustJSON(t, map[string]any{"command": "sleep 5", "timeout_seconds": 1}))
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("timeout_seconds=1 no se respetó: corrió %s", elapsed)
		}
		if !strings.Contains(got, "límite de tiempo") {
			t.Fatalf("salida = %q, quiero la nota de timeout", got)
		}
	})
}

func TestBashResolveTimeout(t *testing.T) {
	cases := []struct {
		name        string
		structField time.Duration
		perCallSecs int
		want        time.Duration
	}{
		{"default cuando todo es cero", 0, 0, DefaultBashTimeout},
		{"valor de la tool cuando no hay per-call", 45 * time.Second, 0, 45 * time.Second},
		{"per-call pisa el valor de la tool", 45 * time.Second, 5, 5 * time.Second},
		{"per-call negativo se ignora", 45 * time.Second, -3, 45 * time.Second},
		{"per-call se topea al máximo", 0, 99999, MaxBashTimeout},
		{"valor de la tool se topea al máximo", 30 * time.Minute, 0, MaxBashTimeout},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := Bash{Timeout: tt.structField}.resolveTimeout(tt.perCallSecs)
			if got != tt.want {
				t.Errorf("resolveTimeout(%d) con Timeout=%s = %s, quiero %s",
					tt.perCallSecs, tt.structField, got, tt.want)
			}
		})
	}
}
