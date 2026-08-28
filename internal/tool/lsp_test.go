package tool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/lsp"
)

type fakeLSP struct {
	diags   []lsp.Diagnostic
	locs    []lsp.Location
	hover   string
	err     error
	gotLine int
	gotChar int
}

func (f *fakeLSP) Diagnostics(context.Context, string) ([]lsp.Diagnostic, error) {
	return f.diags, f.err
}
func (f *fakeLSP) Definition(_ context.Context, _ string, line, char int) ([]lsp.Location, error) {
	f.gotLine, f.gotChar = line, char
	return f.locs, f.err
}
func (f *fakeLSP) Hover(_ context.Context, _ string, line, char int) (string, error) {
	f.gotLine, f.gotChar = line, char
	return f.hover, f.err
}

func lspTool(f *fakeLSP) LSP {
	return LSP{Client: func(context.Context, string) (LSPClient, error) { return f, nil }}
}

func TestLSPTool(t *testing.T) {
	ctx := context.Background()

	t.Run("diagnostics formatea line:col 1-based", func(t *testing.T) {
		f := &fakeLSP{diags: []lsp.Diagnostic{{
			Range:    lsp.Range{Start: lsp.Position{Line: 2, Character: 4}},
			Severity: 1, Source: "gopls", Message: "undefined: Foo",
		}}}
		out, err := lspTool(f).Execute(ctx, mustJSON(t, map[string]any{"action": "diagnostics", "path": "a.go"}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "3:5 error [gopls]: undefined: Foo") {
			t.Fatalf("out = %q", out)
		}
	})

	t.Run("diagnostics vacío", func(t *testing.T) {
		out, err := lspTool(&fakeLSP{}).Execute(ctx, mustJSON(t, map[string]any{"action": "diagnostics", "path": "a.go"}))
		if err != nil || !strings.Contains(out, "sin diagnósticos") {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})

	t.Run("definition convierte a 0-based y formatea", func(t *testing.T) {
		f := &fakeLSP{locs: []lsp.Location{{URI: "file:///proj/x.go", Range: lsp.Range{Start: lsp.Position{Line: 9, Character: 0}}}}}
		out, err := lspTool(f).Execute(ctx, mustJSON(t, map[string]any{
			"action": "definition", "path": "a.go", "line": 10, "character": 3,
		}))
		if err != nil {
			t.Fatal(err)
		}
		if f.gotLine != 9 || f.gotChar != 2 {
			t.Fatalf("posición pasada = %d:%d, quiero 9:2", f.gotLine, f.gotChar)
		}
		if !strings.Contains(out, "x.go:10:1") {
			t.Fatalf("out = %q", out)
		}
	})

	t.Run("hover devuelve el texto", func(t *testing.T) {
		out, err := lspTool(&fakeLSP{hover: "func Foo() error"}).Execute(ctx, mustJSON(t, map[string]any{
			"action": "hover", "path": "a.go", "line": 1, "character": 1,
		}))
		if err != nil || out != "func Foo() error" {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})

	t.Run("action inválida", func(t *testing.T) {
		_, err := lspTool(&fakeLSP{}).Execute(ctx, mustJSON(t, map[string]any{"action": "rename", "path": "a.go"}))
		if err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("error del cliente se propaga", func(t *testing.T) {
		f := &fakeLSP{err: errors.New("server caído")}
		_, err := lspTool(f).Execute(ctx, mustJSON(t, map[string]any{"action": "diagnostics", "path": "a.go"}))
		if err == nil || !strings.Contains(err.Error(), "server caído") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("sin Client configurado", func(t *testing.T) {
		_, err := (LSP{}).Execute(ctx, mustJSON(t, map[string]any{"action": "hover", "path": "a.go"}))
		if err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("resolver del server falla", func(t *testing.T) {
		l := LSP{Client: func(context.Context, string) (LSPClient, error) {
			return nil, errors.New("no hay server para .xyz")
		}}
		_, err := l.Execute(ctx, mustJSON(t, map[string]any{"action": "hover", "path": "a.xyz"}))
		if err == nil || !strings.Contains(err.Error(), ".xyz") {
			t.Fatalf("err = %v", err)
		}
	})
}
