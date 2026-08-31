// Package setup backs `arnes init`: it writes the on-disk config that turns a
// fresh install into one wired for UI Skills and/or the impeccable skill, so a
// teammate runs one command instead of hand-editing JSON.
package setup

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MauricioJC3/arnes_ng/internal/hook"
	"github.com/MauricioJC3/arnes_ng/internal/mcp"
	"github.com/MauricioJC3/arnes_ng/internal/skill"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
)

// UISkillsURL is the UI Skills MCP endpoint.
const UISkillsURL = "https://www.ui-skills.com/mcp"

// Paths are the locations `arnes init` writes to. DefaultPaths resolves them
// from the same env vars and defaults arnes reads at startup, so an init run and
// the next launch agree.
type Paths struct {
	SkillsDir     string // ~/.arnes/skills
	SubagentsFile string // ~/.arnes/subagents.json
	HooksFile     string // ~/.arnes/hooks.json
	MCPFile       string // ~/.arnes/mcp.json
}

// DefaultPaths resolves the effective paths ($ARNES_SKILLS / $ARNES_SUBAGENTS /
// $ARNES_HOOKS / $ARNES_MCP, else the ~/.arnes defaults).
func DefaultPaths() (Paths, error) {
	skillsDir := os.Getenv("ARNES_SKILLS")
	if skillsDir == "" {
		d, err := skill.DefaultDir()
		if err != nil {
			return Paths{}, err
		}
		skillsDir = d
	}
	subs := os.Getenv("ARNES_SUBAGENTS")
	if subs == "" {
		p, err := subagent.DefaultPath()
		if err != nil {
			return Paths{}, err
		}
		subs = p
	}
	hooks := os.Getenv("ARNES_HOOKS")
	if hooks == "" {
		p, err := hook.DefaultPath()
		if err != nil {
			return Paths{}, err
		}
		hooks = p
	}
	mcpFile, err := mcp.ResolvePath()
	if err != nil {
		return Paths{}, err
	}
	return Paths{SkillsDir: skillsDir, SubagentsFile: subs, HooksFile: hooks, MCPFile: mcpFile}, nil
}

// hookAssetsDir is where the impeccable shim is written: a "hooks" folder beside
// hooks.json (so it moves with $ARNES_HOOKS in tests).
func (p Paths) hookAssetsDir() string {
	return filepath.Join(filepath.Dir(p.HooksFile), "hooks")
}

// InitUISkills adds the ui-skills HTTP server to mcp.json. It is idempotent: an
// existing ui-skills entry is left as-is.
func InitUISkills(p Paths) (string, error) {
	cfg, err := mcp.LoadFile(p.MCPFile)
	if err != nil {
		return "", err
	}
	if _, ok := cfg.MCPServers["ui-skills"]; ok {
		return "ui-skills ya estaba configurado en " + p.MCPFile, nil
	}
	if err := mcp.AddServer(p.MCPFile, "ui-skills", mcp.ServerConfig{URL: UISkillsURL}); err != nil {
		return "", err
	}
	return fmt.Sprintf("ui-skills agregado a %s — reiniciá arnes para conectarlo", p.MCPFile), nil
}
