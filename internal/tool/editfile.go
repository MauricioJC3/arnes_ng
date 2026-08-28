package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// EditFile replaces an exact snippet of text in an existing file.
type EditFile struct{}

func (EditFile) Name() string { return "edit_file" }

func (EditFile) Description() string {
	return "Editá un archivo existente reemplazando un fragmento EXACTO de texto. `old` es el " +
		"texto tal cual está (con su indentación) y debe aparecer una sola vez — si no, hacelo " +
		"más específico incluyendo líneas de contexto, o pasá replace_all. Esta es la " +
		"herramienta por defecto para modificar código; write_file es solo para archivos nuevos."
}

func (EditFile) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Ruta del archivo a editar."},
			"old":  map[string]any{"type": "string", "description": "El texto exacto a reemplazar, con su indentación."},
			"new":  map[string]any{"type": "string", "description": "El texto nuevo."},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Reemplazar todas las apariciones de 'old' (default false).",
			},
		},
		"required": []string{"path", "old", "new"},
	}
}

func (EditFile) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path       string `json:"path"`
		Old        string `json:"old"`
		New        string `json:"new"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	if in.Path == "" {
		return "", errors.New("el parámetro 'path' es obligatorio")
	}
	if in.Old == "" {
		return "", errors.New("'old' no puede estar vacío; usá write_file para crear un archivo")
	}
	if in.Old == in.New {
		return "", errors.New("'old' y 'new' son idénticos")
	}

	data, err := os.ReadFile(in.Path)
	if err != nil {
		return "", fmt.Errorf("no se pudo leer %s: %w", in.Path, err)
	}
	content := string(data)

	count := strings.Count(content, in.Old)
	switch {
	case count == 0:
		return "", fmt.Errorf("no se encontró el fragmento en %s", in.Path)
	case count > 1 && !in.ReplaceAll:
		return "", fmt.Errorf("el fragmento aparece %d veces en %s; hacelo más específico o pasá replace_all", count, in.Path)
	}

	var updated string
	replaced := 1
	if in.ReplaceAll {
		updated = strings.ReplaceAll(content, in.Old, in.New)
		replaced = count
	} else {
		updated = strings.Replace(content, in.Old, in.New, 1)
	}

	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(in.Path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(in.Path, []byte(updated), mode); err != nil {
		return "", fmt.Errorf("no se pudo escribir %s: %w", in.Path, err)
	}

	delta := strings.Count(updated, "\n") - strings.Count(content, "\n")
	sfx := ""
	if replaced != 1 {
		sfx = "s"
	}
	return fmt.Sprintf("editado %s: %d reemplazo%s, %+d líneas", in.Path, replaced, sfx, delta), nil
}
