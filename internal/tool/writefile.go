package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile creates or overwrites a text file, making parent directories as needed.
type WriteFile struct{}

func (WriteFile) Name() string { return "write_file" }

func (WriteFile) Description() string {
	return "Crea un archivo NUEVO o reescribe uno completo con el contenido dado (crea los " +
		"directorios padre). Para cambios puntuales en un archivo existente usá edit_file: " +
		"es más barato y no arriesga pisar el resto del archivo."
}

func (WriteFile) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "Ruta del archivo a escribir."},
			"content": map[string]any{"type": "string", "description": "Contenido completo del archivo."},
		},
		"required": []string{"path", "content"},
	}
}

func (WriteFile) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	if in.Path == "" {
		return "", errors.New("el parámetro 'path' es obligatorio")
	}
	if dir := filepath.Dir(in.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("no se pudo crear el directorio %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(in.Path, []byte(in.Content), 0o644); err != nil {
		return "", fmt.Errorf("no se pudo escribir %s: %w", in.Path, err)
	}
	return fmt.Sprintf("escritos %d bytes en %s", len(in.Content), in.Path), nil
}
