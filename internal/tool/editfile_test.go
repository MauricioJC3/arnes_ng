package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEditFile(t *testing.T) {
	ctx := context.Background()

	t.Run("reemplazo puntual", func(t *testing.T) {
		p := writeTemp(t, "hola mundo\nchau mundo\n")
		out, err := EditFile{}.Execute(ctx, mustJSON(t, map[string]any{
			"path": p, "old": "hola mundo", "new": "buenas mundo",
		}))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != "buenas mundo\nchau mundo\n" {
			t.Fatalf("archivo = %q", got)
		}
		if !strings.Contains(out, "1 reemplazo") {
			t.Errorf("resumen = %q", out)
		}
	})

	t.Run("fragmento ambiguo sin replace_all es error", func(t *testing.T) {
		p := writeTemp(t, "x\nx\n")
		_, err := EditFile{}.Execute(ctx, mustJSON(t, map[string]any{"path": p, "old": "x", "new": "y"}))
		if err == nil || !strings.Contains(err.Error(), "2 veces") {
			t.Fatalf("esperaba error de ambigüedad, tengo: %v", err)
		}
	})

	t.Run("replace_all reemplaza todo", func(t *testing.T) {
		p := writeTemp(t, "a\na\na\n")
		out, err := EditFile{}.Execute(ctx, mustJSON(t, map[string]any{
			"path": p, "old": "a", "new": "b", "replace_all": true,
		}))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != "b\nb\nb\n" {
			t.Fatalf("archivo = %q", got)
		}
		if !strings.Contains(out, "3 reemplazos") {
			t.Errorf("resumen = %q", out)
		}
	})

	t.Run("fragmento inexistente es error", func(t *testing.T) {
		p := writeTemp(t, "hola\n")
		if _, err := (EditFile{}).Execute(ctx, mustJSON(t, map[string]any{"path": p, "old": "chau", "new": "x"})); err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("old == new es error", func(t *testing.T) {
		p := writeTemp(t, "hola\n")
		if _, err := (EditFile{}).Execute(ctx, mustJSON(t, map[string]any{"path": p, "old": "hola", "new": "hola"})); err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("old vacío es error", func(t *testing.T) {
		p := writeTemp(t, "hola\n")
		if _, err := (EditFile{}).Execute(ctx, mustJSON(t, map[string]any{"path": p, "old": "", "new": "x"})); err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("archivo inexistente es error", func(t *testing.T) {
		if _, err := (EditFile{}).Execute(ctx, json.RawMessage(`{"path":"/no/existe","old":"a","new":"b"}`)); err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("preserva los permisos del archivo", func(t *testing.T) {
		p := writeTemp(t, "linea\n")
		if err := os.Chmod(p, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := (EditFile{}).Execute(ctx, mustJSON(t, map[string]any{"path": p, "old": "linea", "new": "otra"})); err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(p)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("permisos = %o, quiero 600", info.Mode().Perm())
		}
	})

	t.Run("edits múltiples en orden", func(t *testing.T) {
		p := writeTemp(t, "uno\ndos\ntres\n")
		out, err := EditFile{}.Execute(ctx, mustJSON(t, map[string]any{
			"path": p,
			"edits": []any{
				map[string]any{"old": "uno", "new": "1"},
				map[string]any{"old": "dos", "new": "2"},
				map[string]any{"old": "tres", "new": "3"},
			},
		}))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != "1\n2\n3\n" {
			t.Fatalf("archivo = %q", got)
		}
		if !strings.Contains(out, "3 edición(es)") || !strings.Contains(out, "3 reemplazos") {
			t.Errorf("resumen = %q", out)
		}
	})

	t.Run("edits encadenados: uno depende del anterior", func(t *testing.T) {
		p := writeTemp(t, "valor = 1\n")
		if _, err := (EditFile{}).Execute(ctx, mustJSON(t, map[string]any{
			"path": p,
			"edits": []any{
				map[string]any{"old": "valor = 1", "new": "valor = 2"},
				map[string]any{"old": "valor = 2", "new": "valor = 3"},
			},
		})); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != "valor = 3\n" {
			t.Fatalf("archivo = %q", got)
		}
	})

	t.Run("edits es all-or-nothing: el segundo falla, no se escribe nada", func(t *testing.T) {
		p := writeTemp(t, "alfa\nbeta\n")
		_, err := EditFile{}.Execute(ctx, mustJSON(t, map[string]any{
			"path": p,
			"edits": []any{
				map[string]any{"old": "alfa", "new": "ALFA"},
				map[string]any{"old": "no-existe", "new": "x"},
			},
		}))
		if err == nil || !strings.Contains(err.Error(), "edición 2/2") {
			t.Fatalf("esperaba error de la edición 2, tengo: %v", err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != "alfa\nbeta\n" {
			t.Fatalf("el archivo se modificó pese al fallo: %q", got)
		}
	})

	t.Run("edits con replace_all por entrada", func(t *testing.T) {
		p := writeTemp(t, "a a a\nb\n")
		if _, err := (EditFile{}).Execute(ctx, mustJSON(t, map[string]any{
			"path": p,
			"edits": []any{
				map[string]any{"old": "a", "new": "x", "replace_all": true},
				map[string]any{"old": "b", "new": "y"},
			},
		})); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != "x x x\ny\n" {
			t.Fatalf("archivo = %q", got)
		}
	})
}
