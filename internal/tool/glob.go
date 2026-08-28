package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// maxGlobResults caps how many paths Glob returns.
const maxGlobResults = 200

// Glob finds files by a shell-style pattern, with `**` for any depth.
type Glob struct{}

func (Glob) Name() string { return "glob" }

func (Glob) Description() string {
	return "Encuentra archivos por patrón, relativo al directorio actual. Soporta `**` para " +
		"cualquier profundidad: `**/*.go`, `internal/**/*_test.go`, `cmd/*/main.go`. " +
		"Usala para descubrir archivos; para buscar texto adentro, grep."
}

func (Glob) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{"type": "string", "description": "Patrón glob (ej. `**/*.go`)."},
		},
		"required": []string{"pattern"},
	}
}

func (Glob) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	in.Pattern = strings.TrimSpace(in.Pattern)
	if in.Pattern == "" {
		return "", errors.New("el parámetro 'pattern' es obligatorio")
	}
	if !doublestar.ValidatePattern(in.Pattern) {
		return "", fmt.Errorf("patrón inválido: %q", in.Pattern)
	}

	matches, err := doublestar.Glob(os.DirFS("."), in.Pattern, doublestar.WithFilesOnly())
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "sin coincidencias", nil
	}
	sort.Strings(matches)

	truncated := false
	if len(matches) > maxGlobResults {
		matches = matches[:maxGlobResults]
		truncated = true
	}
	out := strings.Join(matches, "\n")
	if truncated {
		out += fmt.Sprintf("\n… (cortado en %d)", maxGlobResults)
	}
	return out, nil
}
