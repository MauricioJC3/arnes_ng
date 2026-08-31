package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/hook"
	"github.com/MauricioJC3/arnes_ng/internal/mcp"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
)

func TestInitImpeccableEmbedded(t *testing.T) {
	p := tmpPaths(t)

	rep, err := InitImpeccable(p, "")
	if err != nil {
		t.Fatalf("InitImpeccable: %v", err)
	}

	// 1. skill tree
	if _, err := os.Stat(filepath.Join(p.SkillsDir, "impeccable", "SKILL.md")); err != nil {
		t.Fatalf("no se copió el SKILL.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.SkillsDir, "impeccable", "scripts", "context.mjs")); err != nil {
		t.Fatalf("no se copiaron los scripts: %v", err)
	}

	// 2. shim
	shim := filepath.Join(p.hookAssetsDir(), "impeccable-shim.mjs")
	if b, err := os.ReadFile(shim); err != nil || !strings.Contains(string(b), "impeccable") {
		t.Fatalf("shim mal escrito: err=%v", err)
	}

	// 3. subagents: 2 defaults + 4 impeccable, tools mapped
	defs, err := subagent.LoadFile(p.SubagentsFile)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]subagent.Definition{}
	for _, d := range defs {
		byName[d.Name] = d
	}
	for _, n := range []string{"research", "test-writer", "impeccable-asset-producer", "impeccable-documenter", "impeccable-finish-reviewer", "impeccable-manual-edit-applier"} {
		if _, ok := byName[n]; !ok {
			t.Fatalf("falta subagente %q (tengo %d)", n, len(defs))
		}
	}
	fr := byName["impeccable-finish-reviewer"]
	if len(fr.System) < 100 {
		t.Fatalf("system del reviewer muy corto: %d", len(fr.System))
	}
	for _, tl := range fr.Tools {
		if tl != "read_file" && tl != "bash" && tl != "glob" && tl != "grep" {
			t.Fatalf("tool sin mapear en %q: %q", fr.Name, tl)
		}
	}

	// 4. hooks: pre (block:false) + post + stop, all pointing at the shim
	cfg, err := hook.LoadFile(p.HooksFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PreTool) != 1 || len(cfg.PostTool) != 1 || len(cfg.Stop) != 1 {
		t.Fatalf("hooks mal armados: %+v", cfg)
	}
	if cfg.PreTool[0].Block == nil || *cfg.PreTool[0].Block {
		t.Fatalf("el pre-hook debería ser block:false, es %v", cfg.PreTool[0].Block)
	}
	if !strings.Contains(cfg.Stop[0].Command, "impeccable-shim") || !strings.Contains(cfg.Stop[0].Command, "stop") {
		t.Fatalf("stop hook mal: %q", cfg.Stop[0].Command)
	}

	// 5. mcp.json has ui-skills
	mc, err := mcp.LoadFile(p.MCPFile)
	if err != nil {
		t.Fatal(err)
	}
	if mc.MCPServers["ui-skills"].URL != UISkillsURL {
		t.Fatalf("falta ui-skills: %+v", mc.MCPServers)
	}

	_ = rep.String() // must not panic
}

func TestInitImpeccableIdempotent(t *testing.T) {
	p := tmpPaths(t)
	if _, err := InitImpeccable(p, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := InitImpeccable(p, ""); err != nil {
		t.Fatalf("segunda corrida falló: %v", err)
	}

	defs, _ := subagent.LoadFile(p.SubagentsFile)
	seen := map[string]int{}
	for _, d := range defs {
		seen[d.Name]++
	}
	for n, c := range seen {
		if c != 1 {
			t.Fatalf("subagente %q duplicado (%d)", n, c)
		}
	}
	cfg, _ := hook.LoadFile(p.HooksFile)
	if len(cfg.PreTool) != 1 || len(cfg.PostTool) != 1 || len(cfg.Stop) != 1 {
		t.Fatalf("hooks duplicados en la segunda corrida: %+v", cfg)
	}
}

func TestInitImpeccablePreservesUserSubagents(t *testing.T) {
	p := tmpPaths(t)
	mine := []subagent.Definition{{Name: "mio", Description: "propio", System: "hacé lo tuyo"}}
	data, _ := json.MarshalIndent(mine, "", "  ")
	if err := os.WriteFile(p.SubagentsFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InitImpeccable(p, ""); err != nil {
		t.Fatal(err)
	}
	defs, _ := subagent.LoadFile(p.SubagentsFile)
	var hasMine, hasImp bool
	for _, d := range defs {
		if d.Name == "mio" {
			hasMine = true
		}
		if d.Name == "impeccable-documenter" {
			hasImp = true
		}
	}
	if !hasMine || !hasImp {
		t.Fatalf("mio=%v impeccable=%v", hasMine, hasImp)
	}
}
