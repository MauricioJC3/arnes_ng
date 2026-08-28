package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/MauricioJC3/arnes_ng/internal/skill"
)

// Skill loads a named SKILL.md playbook into the current turn. The model reads
// the returned body and follows it in place of its default approach.
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
	return fmt.Sprintf("# skill: %s\n\n%s", sk.Name, sk.Body), nil
}
