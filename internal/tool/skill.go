package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/skill"
)

// Skill loads a named SKILL.md playbook into the current turn. The model reads
// the returned body and follows it in place of its default approach. When the
// skill was loaded from disk, the response also states its base directory so
// the model can resolve any bundled paths the body refers to (reference/*,
// scripts/*).
type Skill struct {
	Skills *skill.Registry
}

func (Skill) Name() string { return "skill" }

func (s Skill) Description() string {
	base := "Cargá las instrucciones de un skill cuando la tarea coincide con uno. Devuelve el " +
		"cuerpo del SKILL.md para que lo sigas en este turno."
	if s.Skills == nil || s.Skills.Len() == 0 {
		return base + " (no hay skills instalados)"
	}
	return base + " Skills disponibles:\n" + s.Skills.Catalog()
}

func (Skill) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "Nombre del skill a cargar."},
		},
		"required": []string{"name"},
	}
}

func (s Skill) Execute(_ context.Context, input json.RawMessage) (string, error) {
	if s.Skills == nil || s.Skills.Len() == 0 {
		return "", errors.New("no hay skills instalados")
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	sk, ok := s.Skills.Get(in.Name)
	if !ok {
		return "", fmt.Errorf("no existe el skill %q. Disponibles:\n%s", in.Name, s.Skills.Catalog())
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# skill: %s\n\n", sk.Name)
	if filepath.IsAbs(sk.Path) {
		fmt.Fprintf(&b, "Directorio base del skill: %s\n"+
			"Resolvé contra él cualquier ruta relativa que mencione el skill (reference/…, scripts/…).\n\n",
			filepath.Dir(sk.Path))
	}
	b.WriteString(sk.Body)
	return b.String(), nil
}
