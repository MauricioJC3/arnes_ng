package setup

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/mcp"
)

// tmpPaths points every init target at a fresh temp dir.
func tmpPaths(t *testing.T) Paths {
	t.Helper()
	d := t.TempDir()
	return Paths{
		SkillsDir:     filepath.Join(d, "skills"),
		SubagentsFile: filepath.Join(d, "subagents.json"),
		HooksFile:     filepath.Join(d, "hooks.json"),
		MCPFile:       filepath.Join(d, "mcp.json"),
	}
}

func TestInitUISkills(t *testing.T) {
	p := tmpPaths(t)

	msg, err := InitUISkills(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "agregado") {
		t.Fatalf("msg = %q", msg)
	}
	cfg, err := mcp.LoadFile(p.MCPFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPServers["ui-skills"].URL != UISkillsURL {
		t.Fatalf("no quedó el server: %+v", cfg.MCPServers)
	}

	// Idempotente: segunda corrida no falla ni duplica.
	msg2, err := InitUISkills(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg2, "ya estaba") {
		t.Fatalf("segunda corrida: %q", msg2)
	}
}

func TestInitUISkillsKeepsOtherServers(t *testing.T) {
	p := tmpPaths(t)
	if err := mcp.AddServer(p.MCPFile, "otro", mcp.ServerConfig{URL: "https://otro/mcp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := InitUISkills(p); err != nil {
		t.Fatal(err)
	}
	cfg, _ := mcp.LoadFile(p.MCPFile)
	if _, ok := cfg.MCPServers["otro"]; !ok {
		t.Fatal("init pisó un server existente")
	}
	if _, ok := cfg.MCPServers["ui-skills"]; !ok {
		t.Fatal("no agregó ui-skills")
	}
}
