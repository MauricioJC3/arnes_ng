package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// EditFile replaces an exact snippet of text in an existing file. It takes
// either a single old/new pair or an "edits" array applied in order to the same
// file in one shot; the array is all-or-nothing (nothing is written if any edit
// fails). A non-nil Tracker enforces read-before-write: editing a file the model
// has not read this session is refused.
type EditFile struct{ Tracker *FileTracker }

func (EditFile) Name() string { return "edit_file" }

func (EditFile) Description() string {
	return "Editá un archivo existente reemplazando fragmentos EXACTOS de texto. `old` es el " +
		"texto tal cual está (con su indentación) y debe aparecer una sola vez — si no, hacelo " +
		"más específico incluyendo líneas de contexto, o pasá replace_all. Para varios cambios " +
		"en el mismo archivo pasá `edits` (array de {old, new, replace_all}) y se aplican en " +
		"orden en una sola llamada: si alguno falla no se escribe nada. Esta es la herramienta " +
		"por defecto para modificar código; write_file es solo para archivos nuevos."
}

func (EditFile) InputSchema() map[string]any {
	edit := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"old":         map[string]any{"type": "string", "description": "El texto exacto a reemplazar, con su indentación."},
			"new":         map[string]any{"type": "string", "description": "El texto nuevo."},
			"replace_all": map[string]any{"type": "boolean", "description": "Reemplazar todas las apariciones de 'old' (default false)."},
		},
		"required": []string{"old", "new"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Ruta del archivo a editar."},
			"old":  map[string]any{"type": "string", "description": "El texto exacto a reemplazar, con su indentación. Ignorado si se pasa 'edits'."},
			"new":  map[string]any{"type": "string", "description": "El texto nuevo. Ignorado si se pasa 'edits'."},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Reemplazar todas las apariciones de 'old' (default false).",
			},
			"edits": map[string]any{
				"type":        "array",
				"items":       edit,
				"description": "Varios reemplazos aplicados en orden al mismo archivo. Si se pasa, se ignoran 'old'/'new' de nivel superior.",
			},
		},
		"required": []string{"path"},
	}
}

type editOp struct {
	Old        string `json:"old"`
	New        string `json:"new"`
	ReplaceAll bool   `json:"replace_all"`
}

func (e EditFile) Execute(_ context.Context, input json.RawMessage) (string, error) {
	var in struct {
		Path       string   `json:"path"`
		Old        string   `json:"old"`
		New        string   `json:"new"`
		ReplaceAll bool     `json:"replace_all"`
		Edits      []editOp `json:"edits"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	if in.Path == "" {
		return "", errors.New("el parámetro 'path' es obligatorio")
	}
	if err := e.Tracker.GuardWrite(in.Path); err != nil {
		return "", err
	}

	ops := in.Edits
	if len(ops) == 0 {
		ops = []editOp{{Old: in.Old, New: in.New, ReplaceAll: in.ReplaceAll}}
	}

	data, err := os.ReadFile(in.Path)
	if err != nil {
		return "", fmt.Errorf("no se pudo leer %s: %w", in.Path, err)
	}
	content := string(data)
	original := content

	total := 0
	for i, op := range ops {
		label := ""
		if len(ops) > 1 {
			label = fmt.Sprintf(" (edición %d/%d)", i+1, len(ops))
		}
		if op.Old == "" {
			return "", fmt.Errorf("'old' no puede estar vacío%s; usá write_file para crear un archivo", label)
		}
		if op.Old == op.New {
			return "", fmt.Errorf("'old' y 'new' son idénticos%s", label)
		}

		count := strings.Count(content, op.Old)
		switch {
		case count == 0:
			return "", fmt.Errorf("no se encontró el fragmento en %s%s", in.Path, label)
		case count > 1 && !op.ReplaceAll:
			return "", fmt.Errorf("el fragmento aparece %d veces en %s%s; hacelo más específico o pasá replace_all", count, in.Path, label)
		}

		if op.ReplaceAll {
			content = strings.ReplaceAll(content, op.Old, op.New)
			total += count
		} else {
			content = strings.Replace(content, op.Old, op.New, 1)
			total++
		}
	}

	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(in.Path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(in.Path, []byte(content), mode); err != nil {
		return "", fmt.Errorf("no se pudo escribir %s: %w", in.Path, err)
	}
	e.Tracker.MarkRead(in.Path) // the edit result is known; a follow-up edit needs no re-read

	delta := strings.Count(content, "\n") - strings.Count(original, "\n")
	sfx := ""
	if total != 1 {
		sfx = "s"
	}
	if len(ops) > 1 {
		return fmt.Sprintf("editado %s: %d edición(es), %d reemplazo%s, %+d líneas", in.Path, len(ops), total, sfx, delta), nil
	}
	return fmt.Sprintf("editado %s: %d reemplazo%s, %+d líneas", in.Path, total, sfx, delta), nil
}
