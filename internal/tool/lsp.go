package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/lsp"
)

// LSPClient is the slice of an LSP client the tool uses. *lsp.Client satisfies
// it structurally.
type LSPClient interface {
	Diagnostics(ctx context.Context, path string) ([]lsp.Diagnostic, error)
	Definition(ctx context.Context, path string, line, character int) ([]lsp.Location, error)
	Hover(ctx context.Context, path string, line, character int) (string, error)
}

// LSP exposes language-server diagnostics, go-to-definition and hover. Client
// resolves (and lazily starts) the server for a given file; a nil Client
// disables the tool.
type LSP struct {
	Client func(ctx context.Context, path string) (LSPClient, error)
}

func (LSP) Name() string { return "lsp" }

func (LSP) Description() string {
	return "Consultá un language server sobre un archivo del proyecto. action: " +
		"'diagnostics' (errores/warnings del archivo), 'definition' (dónde se define el " +
		"símbolo en line:character) o 'hover' (tipo/doc del símbolo en line:character). " +
		"line y character son 1-based. Necesita el server configurado (gopls para .go por defecto)."
}

func (LSP) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string", "enum": []string{"diagnostics", "definition", "hover"},
				"description": "Qué consultar.",
			},
			"path":      map[string]any{"type": "string", "description": "Ruta del archivo."},
			"line":      map[string]any{"type": "integer", "description": "Línea 1-based (definition/hover)."},
			"character": map[string]any{"type": "integer", "description": "Columna 1-based (definition/hover)."},
		},
		"required": []string{"action", "path"},
	}
}

func (l LSP) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	if l.Client == nil {
		return "", errors.New("lsp no está disponible en este arnés")
	}
	var in struct {
		Action    string `json:"action"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Character int    `json:"character"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	if in.Path == "" {
		return "", errors.New("'path' es obligatorio")
	}

	cl, err := l.Client(ctx, in.Path)
	if err != nil {
		return "", err
	}

	// LSP positions are zero-based; the tool takes them 1-based.
	line, char := in.Line-1, in.Character-1
	if line < 0 {
		line = 0
	}
	if char < 0 {
		char = 0
	}

	switch in.Action {
	case "diagnostics":
		ds, err := cl.Diagnostics(ctx, in.Path)
		if err != nil {
			return "", err
		}
		if len(ds) == 0 {
			return "sin diagnósticos en " + in.Path, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d diagnóstico(s) en %s:\n", len(ds), in.Path)
		for _, d := range ds {
			src := d.Source
			if src != "" {
				src = " [" + src + "]"
			}
			fmt.Fprintf(&b, "  %d:%d %s%s: %s\n",
				d.Range.Start.Line+1, d.Range.Start.Character+1,
				lsp.SeverityName(d.Severity), src, strings.TrimSpace(d.Message))
		}
		return strings.TrimRight(b.String(), "\n"), nil

	case "definition":
		locs, err := cl.Definition(ctx, in.Path, line, char)
		if err != nil {
			return "", err
		}
		if len(locs) == 0 {
			return "sin definición para esa posición", nil
		}
		var b strings.Builder
		for _, loc := range locs {
			fmt.Fprintf(&b, "%s:%d:%d\n", lsp.URIPath(loc.URI), loc.Range.Start.Line+1, loc.Range.Start.Character+1)
		}
		return strings.TrimRight(b.String(), "\n"), nil

	case "hover":
		txt, err := cl.Hover(ctx, in.Path, line, char)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(txt) == "" {
			return "sin información de hover para esa posición", nil
		}
		return txt, nil

	default:
		return "", fmt.Errorf("action inválida: %q (diagnostics|definition|hover)", in.Action)
	}
}
