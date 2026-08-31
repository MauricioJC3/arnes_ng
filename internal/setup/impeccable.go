package setup

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/hook"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
)

//go:embed assets
var assetsFS embed.FS

// impeccablePin is the upstream commit the embedded snapshot came from
// (see assets/impeccable/NOTICE.md).
const impeccablePin = "b0594c7"

// ccToArnesTool maps Claude Code tool names to arnes tool names for the
// subagent conversion. Unknown names (and delegate) are dropped.
var ccToArnesTool = map[string]string{
	"Read": "read_file", "Write": "write_file", "Edit": "edit_file",
	"Bash": "bash", "Glob": "glob", "Grep": "grep",
}

// Report is the outcome of an init run: one line per action, plus a Node warning.
type Report struct {
	lines       []string
	nodeMissing bool
}

func (r *Report) add(format string, a ...any) { r.lines = append(r.lines, fmt.Sprintf(format, a...)) }

// String renders the report for the CLI.
func (r Report) String() string {
	var b strings.Builder
	for _, l := range r.lines {
		b.WriteString("  " + l + "\n")
	}
	if r.nodeMissing {
		b.WriteString("\n  ⚠ node no está en el PATH — impeccable lo necesita para sus scripts. Instalá Node y volvé a probar.\n")
	}
	b.WriteString("\nlisto — reiniciá arnes para que tome la config.\n")
	return b.String()
}

// InitImpeccable installs the impeccable skill, its 4 subagents and its hooks,
// and adds ui-skills to mcp.json. The snapshot comes from the embedded bundle
// unless from names a local impeccable checkout or a git URL.
func InitImpeccable(p Paths, from string) (Report, error) {
	var rep Report

	skillSrc, agentsSrc, cleanup, err := impeccableSources(from)
	if err != nil {
		return rep, err
	}
	defer cleanup()

	// 1. skill tree -> <skills>/impeccable/
	skillDst := filepath.Join(p.SkillsDir, "impeccable")
	if err := os.RemoveAll(skillDst); err != nil {
		return rep, err
	}
	n, err := copyTree(skillSrc, skillDst)
	if err != nil {
		return rep, fmt.Errorf("copiando el skill: %w", err)
	}
	rep.add("skill impeccable → %s (%d archivos, snapshot %s)", skillDst, n, impeccablePin)

	// 2. shim -> <hooks-dir>/impeccable-shim.mjs
	shim, err := assetsFS.ReadFile("assets/impeccable/impeccable-shim.mjs")
	if err != nil {
		return rep, err
	}
	shimPath := filepath.Join(p.hookAssetsDir(), "impeccable-shim.mjs")
	if err := os.MkdirAll(filepath.Dir(shimPath), 0o755); err != nil {
		return rep, err
	}
	if err := os.WriteFile(shimPath, shim, 0o644); err != nil {
		return rep, err
	}
	rep.add("shim → %s", shimPath)

	// 3. subagents merge
	added, err := mergeSubagents(p.SubagentsFile, agentsSrc)
	if err != nil {
		return rep, fmt.Errorf("subagentes: %w", err)
	}
	rep.add("subagentes en %s (+%d de impeccable)", p.SubagentsFile, added)

	// 4. hooks merge
	hooksAdded, err := mergeHooks(p.HooksFile, shimPath)
	if err != nil {
		return rep, fmt.Errorf("hooks: %w", err)
	}
	rep.add("hooks en %s (+%d entradas)", p.HooksFile, hooksAdded)

	// 5. ui-skills into mcp.json
	msg, err := InitUISkills(p)
	if err != nil {
		return rep, err
	}
	rep.add("%s", msg)

	// 6. node check
	if _, err := exec.LookPath("node"); err != nil {
		rep.nodeMissing = true
	}
	return rep, nil
}

// impeccableSources returns fs.FS handles for the skill dir and the agents dir,
// plus a cleanup func. from: "" = embedded; a git URL = shallow clone to temp;
// otherwise a local impeccable checkout (reads its .claude/ layout).
func impeccableSources(from string) (skillSrc, agentsSrc fs.FS, cleanup func(), err error) {
	cleanup = func() {}

	if from == "" {
		s, e := fs.Sub(assetsFS, "assets/impeccable/skill")
		if e != nil {
			return nil, nil, cleanup, e
		}
		a, e := fs.Sub(assetsFS, "assets/impeccable/agents")
		if e != nil {
			return nil, nil, cleanup, e
		}
		return s, a, cleanup, nil
	}

	dir := from
	if strings.Contains(from, "://") || strings.HasSuffix(from, ".git") {
		tmp, e := os.MkdirTemp("", "arnes-impeccable-*")
		if e != nil {
			return nil, nil, cleanup, e
		}
		cleanup = func() { os.RemoveAll(tmp) }
		c := exec.Command("git", "clone", "--depth", "1", from, tmp)
		c.Stderr = os.Stderr
		if e := c.Run(); e != nil {
			cleanup()
			return nil, nil, func() {}, fmt.Errorf("git clone %s: %w", from, e)
		}
		dir = tmp
	}

	skillDir := filepath.Join(dir, ".claude", "skills", "impeccable")
	agentsDir := filepath.Join(dir, ".claude", "agents")
	if _, e := os.Stat(filepath.Join(skillDir, "SKILL.md")); e != nil {
		cleanup()
		return nil, nil, func() {}, fmt.Errorf("%s no parece un checkout de impeccable (falta .claude/skills/impeccable/SKILL.md)", dir)
	}
	return os.DirFS(skillDir), os.DirFS(agentsDir), cleanup, nil
}

// copyTree writes every regular file under src to dst, returning the count.
func copyTree(src fs.FS, dst string) (int, error) {
	count := 0
	err := fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		count++
		return os.WriteFile(target, data, 0o644)
	})
	return count, err
}

// mergeSubagents converts the impeccable-*.md agents in agentsSrc to arnes
// Definitions and merges them into the JSON file at path (creating it from the
// built-in defaults when absent). Existing names are left untouched.
func mergeSubagents(path string, agentsSrc fs.FS) (int, error) {
	defs, err := subagent.LoadFile(path) // returns Defaults() when the file is absent
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, d := range defs {
		have[d.Name] = true
	}

	entries, err := fs.ReadDir(agentsSrc, ".")
	if err != nil {
		return 0, err
	}
	added := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "impeccable-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := fs.ReadFile(agentsSrc, e.Name())
		if err != nil {
			return 0, err
		}
		def, err := agentMarkdownToDef(string(raw))
		if err != nil {
			return 0, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if have[def.Name] {
			continue
		}
		defs = append(defs, def)
		have[def.Name] = true
		added++
	}
	if added == 0 {
		return 0, nil
	}

	data, err := json.MarshalIndent(defs, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	return added, os.WriteFile(path, append(data, '\n'), 0o644)
}

// agentMarkdownToDef parses a Claude Code agent .md (YAML-ish frontmatter +
// body) into an arnes subagent Definition, mapping tool names.
func agentMarkdownToDef(md string) (subagent.Definition, error) {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return subagent.Definition{}, fmt.Errorf("sin frontmatter")
	}
	var def subagent.Definition
	var lastKey string
	i := 1
	for ; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			i++
			break
		}
		if item := strings.TrimSpace(line); strings.HasPrefix(item, "- ") && lastKey == "tools" {
			def.Tools = appendTool(def.Tools, strings.TrimSpace(item[2:]))
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		lastKey = strings.TrimSpace(key)
		val = strings.TrimSpace(strings.Trim(strings.TrimSpace(val), `"'`))
		switch lastKey {
		case "name":
			def.Name = val
		case "description":
			def.Description = val
		case "model":
			if val != "" && val != "inherit" {
				def.Model = val
			}
		case "tools":
			for _, t := range strings.Split(val, ",") {
				def.Tools = appendTool(def.Tools, strings.TrimSpace(t))
			}
		}
	}
	def.System = strings.TrimSpace(strings.Join(lines[i:], "\n"))
	if def.Name == "" || def.System == "" {
		return subagent.Definition{}, fmt.Errorf("falta name o cuerpo")
	}
	return def, nil
}

func appendTool(tools []string, ccName string) []string {
	mapped, ok := ccToArnesTool[ccName]
	if !ok {
		return tools
	}
	for _, t := range tools {
		if t == mapped {
			return tools
		}
	}
	return append(tools, mapped)
}

// mergeHooks adds the impeccable pre/post/stop entries to the hooks file at
// path (creating it when absent), pointing them at shimPath. Entries whose
// command already mentions the shim are left as-is.
func mergeHooks(path, shimPath string) (int, error) {
	cfg, err := hook.LoadFile(path)
	if err != nil {
		return 0, err
	}
	node := "node " + quote(shimPath)
	block := false

	added := 0
	ensure := func(list *[]hook.Hook, match, phase string, blk *bool) {
		for _, h := range *list {
			if strings.Contains(h.Command, "impeccable-shim") {
				return
			}
		}
		*list = append(*list, hook.Hook{Match: match, Command: node + " " + phase, Block: blk})
		added++
	}
	ensure(&cfg.PreTool, "^(edit_file|write_file)$", "pre", &block)
	ensure(&cfg.PostTool, "^(edit_file|write_file)$", "post", nil)
	ensure(&cfg.Stop, "^Stop$", "stop", nil)

	if added == 0 {
		return 0, nil
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	return added, os.WriteFile(path, append(data, '\n'), 0o644)
}

// quote wraps s in double quotes for a shell command string (bash and
// PowerShell both take "…" with literal backslashes).
func quote(s string) string { return `"` + s + `"` }
