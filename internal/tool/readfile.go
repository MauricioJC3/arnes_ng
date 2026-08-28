package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ReadFile returns the full contents of a text file.
type ReadFile struct{}

func (ReadFile) Name() string { return "read_file" }

func (ReadFile) Description() string {
	return "Lee y devuelve el contenido completo de un archivo de texto en la ruta indicada."
}

func (ReadFile) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Ruta del archivo a leer (absoluta o relativa al directorio actual).",
			},
		},
		"required": []string{"path"},
	}
}

func (ReadFile) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	if in.Path == "" {
		return "", errors.New("el parámetro 'path' es obligatorio")
	}
	data, err := os.ReadFile(in.Path)
	if err != nil {
		return "", fmt.Errorf("no se pudo leer %s: %w", in.Path, err)
	}
	return string(data), nil
}
