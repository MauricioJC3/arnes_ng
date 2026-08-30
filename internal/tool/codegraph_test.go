package tool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeCodegraph writes a stub executable that echoes "codegraph <args>" and
// exits 0, and returns its path.
func fakeCodegraph(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub de shell, no aplica en windows")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "codegraph")
	script := "#!/bin/sh\necho \"codegraph $*\"\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewCodeGraphAvailability(t *testing.T) {
	t.Run("sin .codegraph/ devuelve nil", func(t *testing.T) {
		if cg := NewCodeGraph(t.TempDir()); cg != nil {
			t.Fatal("sin índice no debería construirse la tool")
		}
	})

	t.Run("con .codegraph/ pero binario ausente devuelve nil", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".codegraph"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", t.TempDir()) // un PATH sin `codegraph`
		if cg := NewCodeGraph(dir); cg != nil {
			t.Fatal("sin binario en PATH no debería construirse la tool")
		}
	})
}

func TestCodeGraphExecute(t *testing.T) {
	bin := fakeCodegraph(t)
	ctx := context.Background()
	cg := CodeGraph{bin: bin, dir: t.TempDir()}

	t.Run("op de solo lectura corre el subcomando con el query", func(t *testing.T) {
		out, err := cg.Execute(ctx, mustJSON(t, map[string]string{"op": "callers", "query": "ParseConfig"}))
		if err != nil {
			t.Fatal(err)
		}
		if out != "codegraph callers ParseConfig\n" {
			t.Fatalf("salida = %q", out)
		}
	})

	t.Run("op sin query", func(t *testing.T) {
		out, err := cg.Execute(ctx, mustJSON(t, map[string]string{"op": "status"}))
		if err != nil {
			t.Fatal(err)
		}
		if out != "codegraph status\n" {
			t.Fatalf("salida = %q", out)
		}
	})

	t.Run("op mutante se rechaza sin ejecutar", func(t *testing.T) {
		for _, op := range []string{"index", "init", "sync", "uninit", "install", "upgrade"} {
			_, err := cg.Execute(ctx, mustJSON(t, map[string]string{"op": op}))
			if err == nil {
				t.Fatalf("op %q debería rechazarse", op)
			}
		}
	})

	t.Run("op vacía es error", func(t *testing.T) {
		if _, err := cg.Execute(ctx, mustJSON(t, map[string]string{"op": ""})); err == nil {
			t.Fatal("esperaba error por op vacía")
		}
	})
}
