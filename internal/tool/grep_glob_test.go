package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\nfunc main() { helloWorld() }\n")
	write("internal/a/a.go", "package a\nfunc helloWorld() {}\n")
	write("internal/a/a_test.go", "package a\nfunc TestX(t *T) { helloWorld() }\n")
	write("README.md", "# proyecto\nla funcion helloWorld esta en internal/a\n")
	return dir
}

func TestGrep(t *testing.T) {
	dir := tree(t)
	cd(t, dir)

	t.Run("encuentra el patrón con file:line", func(t *testing.T) {
		out, err := Grep{}.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "helloWorld"}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "main.go:2:") || !strings.Contains(out, "internal/a/a.go:2:") {
			t.Fatalf("out:\n%s", out)
		}
	})

	t.Run("filtra por glob", func(t *testing.T) {
		out, _ := Grep{}.Execute(context.Background(), mustJSON(t, map[string]any{
			"pattern": "helloWorld", "glob": "*_test.go",
		}))
		if !strings.Contains(out, "a_test.go") || strings.Contains(out, "README") {
			t.Fatalf("out:\n%s", out)
		}
	})

	t.Run("sin coincidencias", func(t *testing.T) {
		out, err := Grep{}.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "noExisteEsto"}))
		if err != nil || !strings.Contains(out, "sin coincidencias") {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})

	t.Run("regex inválida es error", func(t *testing.T) {
		if _, err := (Grep{}).Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "("})); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestGlob(t *testing.T) {
	dir := tree(t)
	cd(t, dir)

	t.Run("** cualquier profundidad", func(t *testing.T) {
		out, err := Glob{}.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "**/*.go"}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"main.go", "internal/a/a.go", "internal/a/a_test.go"} {
			if !strings.Contains(out, want) {
				t.Fatalf("falta %q en:\n%s", want, out)
			}
		}
	})

	t.Run("patrón específico", func(t *testing.T) {
		out, _ := Glob{}.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "internal/**/*_test.go"}))
		if !strings.Contains(out, "a_test.go") || strings.Contains(out, "main.go") {
			t.Fatalf("out:\n%s", out)
		}
	})

	t.Run("sin coincidencias", func(t *testing.T) {
		out, _ := Glob{}.Execute(context.Background(), mustJSON(t, map[string]any{"pattern": "**/*.rs"}))
		if !strings.Contains(out, "sin coincidencias") {
			t.Fatalf("out=%q", out)
		}
	})
}

// cd changes into dir for the test and restores the cwd afterwards.
func cd(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
}
