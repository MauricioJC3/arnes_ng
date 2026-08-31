package tool

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/skill"
)

func TestSkillTool(t *testing.T) {
	ctx := context.Background()
	reg := skill.NewRegistry(
		skill.Skill{Name: "deploy", Description: "cómo deployar", Body: "1. correr CI\n2. tag\n"},
	)
	tl := Skill{Skills: reg}

	t.Run("descripción lista los skills", func(t *testing.T) {
		if !strings.Contains(tl.Description(), "deploy: cómo deployar") {
			t.Fatalf("description = %q", tl.Description())
		}
	})

	t.Run("carga el cuerpo del skill", func(t *testing.T) {
		out, err := tl.Execute(ctx, mustJSON(t, map[string]any{"name": "deploy"}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "skill: deploy") || !strings.Contains(out, "correr CI") {
			t.Fatalf("out = %q", out)
		}
	})

	t.Run("skill en disco reporta su directorio base", func(t *testing.T) {
		abs := filepath.Join(t.TempDir(), "impeccable", "SKILL.md")
		withPath := Skill{Skills: skill.NewRegistry(
			skill.Skill{Name: "impeccable", Body: "corré scripts/context.mjs", Path: abs},
		)}
		out, err := withPath.Execute(ctx, mustJSON(t, map[string]any{"name": "impeccable"}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Directorio base del skill: "+filepath.Dir(abs)) {
			t.Fatalf("no reportó el directorio base: %q", out)
		}
	})

	t.Run("skill sin path en disco no reporta directorio", func(t *testing.T) {
		out, err := tl.Execute(ctx, mustJSON(t, map[string]any{"name": "deploy"}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "Directorio base") {
			t.Fatalf("no debería reportar directorio sin path absoluto: %q", out)
		}
	})

	t.Run("skill inexistente es error con la lista", func(t *testing.T) {
		_, err := tl.Execute(ctx, mustJSON(t, map[string]any{"name": "fantasma"}))
		if err == nil || !strings.Contains(err.Error(), "deploy") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("sin skills instalados", func(t *testing.T) {
		_, err := (Skill{Skills: skill.NewRegistry()}).Execute(ctx, mustJSON(t, map[string]any{"name": "x"}))
		if err == nil {
			t.Fatal("esperaba error")
		}
		if !strings.Contains((Skill{}).Description(), "no hay skills") {
			t.Fatal("la descripción debería avisar que no hay skills")
		}
	})
}
