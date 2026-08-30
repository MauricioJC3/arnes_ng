package main

// wiring.go holds the composition-root plumbing: reading the environment and the
// default config paths into the values run() assembles into an app.Deps. Nothing
// here has domain logic -- that lives in internal/app.

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/app"
	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/compact"
	"github.com/MauricioJC3/arnes_ng/internal/config"
	"github.com/MauricioJC3/arnes_ng/internal/hook"
	"github.com/MauricioJC3/arnes_ng/internal/lsp"
	"github.com/MauricioJC3/arnes_ng/internal/mcp"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/skill"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
	"github.com/MauricioJC3/arnes_ng/internal/tui"
	"github.com/MauricioJC3/arnes_ng/internal/update"
)

// loadTheme reads the TUI theme from ARNES_THEME or the default theme file.
func loadTheme() (tui.Theme, error) {
	path := os.Getenv("ARNES_THEME")
	if path == "" {
		p, err := tui.ThemePath()
		if err != nil {
			return tui.Theme{}, err
		}
		path = p
	}
	return tui.LoadTheme(path)
}

// loadMCP reads the MCP config (ARNES_MCP or the default path). A missing file
// yields an empty config.
func loadMCP() (mcp.Config, error) {
	path := os.Getenv("ARNES_MCP")
	if path == "" {
		p, err := mcp.DefaultPath()
		if err != nil {
			return mcp.Config{}, err
		}
		path = p
	}
	return mcp.LoadFile(path)
}

// loadHooks reads the hooks config (ARNES_HOOKS or the default path). A missing
// file yields an empty config (no hooks).
func loadHooks() (hook.Config, error) {
	path := os.Getenv("ARNES_HOOKS")
	if path == "" {
		p, err := hook.DefaultPath()
		if err != nil {
			return hook.Config{}, err
		}
		path = p
	}
	return hook.LoadFile(path)
}

// loadLSP reads the LSP config (ARNES_LSP or the default path), falling back to
// the built-in default (gopls for Go) when the file is absent.
func loadLSP() (lsp.Config, error) {
	path := os.Getenv("ARNES_LSP")
	if path == "" {
		p, err := lsp.DefaultPath()
		if err != nil {
			return lsp.Config{}, err
		}
		path = p
	}
	return lsp.LoadFile(path)
}

// loadSkills scans the project (<cwd>/.arnes/skills) and global (ARNES_SKILLS or
// ~/.arnes/skills) skill directories. On every run it first makes sure the
// curated default skills bundled in the binary are present in the global dir,
// adding only the ones that are missing -- a user's own skills and their edits
// to a default are left alone, and nothing is prompted. A seeding failure never
// blocks startup.
func loadSkills(cwd string) ([]skill.Skill, error) {
	global := os.Getenv("ARNES_SKILLS")
	if global == "" {
		p, err := skill.DefaultDir()
		if err != nil {
			return nil, err
		}
		global = p
	}
	_, _ = skill.SeedDefaults(global)
	return skill.Load(skill.Dirs(cwd, global)...)
}

// loadSubagents reads the subagents config (ARNES_SUBAGENTS or the default
// path), falling back to the built-in set when the file is absent.
func loadSubagents() ([]subagent.Definition, error) {
	path := os.Getenv("ARNES_SUBAGENTS")
	if path == "" {
		p, err := subagent.DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	return subagent.LoadFile(path)
}

// startupConfig layers the ARNES_PROVIDER / ARNES_MODEL env overrides onto a
// copy of the loaded config. API keys are merged separately (app.MergeEnvKeys).
func startupConfig(cfg config.Config) config.Config {
	startup := cfg.Clone()
	if v := os.Getenv("ARNES_PROVIDER"); v != "" {
		startup.Provider = v
	}
	if v := os.Getenv("ARNES_MODEL"); v != "" {
		startup.Model = v
	}
	return startup
}

// resolveUI reads ARNES_UI (tui by default) and ARNES_STREAM. Streaming applies
// only to the TUI and is on unless ARNES_STREAM is an explicit off value.
func resolveUI() (uiMode string, streaming bool) {
	uiMode = strings.ToLower(cmp.Or(os.Getenv("ARNES_UI"), "tui"))
	streaming = uiMode == "tui" && !isFalsey(os.Getenv("ARNES_STREAM"))
	return uiMode, streaming
}

// buildApprover builds the approval gateway. In TUI mode requests flow through a
// channel (and, when streaming, a 256-slot delta channel is created too); in
// plain mode they prompt on stdin/stdout. Read-only tools and the in-memory
// checklist are always waved through so normal mode doesn't ask on every
// read_file -- writes, bash and remember still go through approval. The returned
// channels are nil outside TUI mode (and deltas is nil when streaming is off).
func buildApprover(uiMode string, streaming bool, stdin *bufio.Reader, stdout io.Writer) (approval.Approver, chan approval.Request, chan string) {
	var approver approval.Approver
	var approvals chan approval.Request
	var deltas chan string

	if uiMode == "tui" {
		ch := approval.NewChannel()
		approver, approvals = ch, ch.Requests
		if streaming {
			deltas = make(chan string, 256)
		}
	} else {
		approver = approval.Prompt{In: stdin, Out: stdout}
	}

	approver = approval.NewSafe(approver,
		"todo_write", "read_file", "grep", "glob", "recall", "lsp", "skill")
	return approver, approvals, deltas
}

// newTodoBridge wires a todo store to a buffered, latest-wins channel: the model
// mutates the store via todo_write, the TUI reads the channel. The drain loop
// keeps only the newest snapshot so a slow reader never blocks a fast writer.
func newTodoBridge() (*todo.Store, chan []todo.Item) {
	store := todo.NewStore()
	ch := make(chan []todo.Item, 1)
	store.OnChange(func(items []todo.Item) {
		for {
			select {
			case ch <- items:
				return
			default:
				select {
				case <-ch:
				default:
				}
			}
		}
	})
	return store, ch
}

// compactionFromEnv reads ARNES_COMPACT / ARNES_COMPACT_AT into an auto-
// compaction strategy and threshold. Default: "sliding" at 120k tokens, so a
// long session does not bloat the context and push the model into loops.
// ARNES_COMPACT=off disables it; ARNES_COMPACT=summarize upgrades it.
func compactionFromEnv(p provider.Provider) (compact.Strategy, int, error) {
	name := strings.ToLower(cmp.Or(os.Getenv("ARNES_COMPACT"), "sliding"))
	if name == "off" || name == "none" || name == "" {
		return nil, 0, nil
	}
	s, err := app.StrategyByName(name, p)
	if err != nil {
		return nil, 0, err
	}
	at := 120_000
	if raw := os.Getenv("ARNES_COMPACT_AT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, 0, fmt.Errorf("ARNES_COMPACT_AT inválido: %q", raw)
		}
		at = n
	}
	return s, at, nil
}

// maxStepsFromEnv resolves the per-turn tool-step budget: ARNES_MAX_STEPS wins
// over the config file; 0 (neither set) lets the agent apply its own default.
func maxStepsFromEnv(cfg config.Config) (int, error) {
	if raw := os.Getenv("ARNES_MAX_STEPS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("ARNES_MAX_STEPS inválido: %q", raw)
		}
		return n, nil
	}
	if cfg.MaxSteps < 0 {
		return 0, fmt.Errorf("max_steps inválido en la config: %d", cfg.MaxSteps)
	}
	return cfg.MaxSteps, nil
}

// maxTokensFromEnv resolves the per-call output-token cap: ARNES_MAX_TOKENS wins
// over the config file; 0 (neither set) lets the agent apply its own default.
func maxTokensFromEnv(cfg config.Config) (int, error) {
	if raw := os.Getenv("ARNES_MAX_TOKENS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("ARNES_MAX_TOKENS inválido: %q", raw)
		}
		return n, nil
	}
	if cfg.MaxTokens < 0 {
		return 0, fmt.Errorf("max_tokens inválido en la config: %d", cfg.MaxTokens)
	}
	return cfg.MaxTokens, nil
}

// providerRetriesFromEnv reads ARNES_PROVIDER_RETRIES. Unset returns -1 (the
// "use the agent default" signal so 0 stays meaningful as "disable retry"); a
// non-numeric or negative value is an error.
func providerRetriesFromEnv() (int, error) {
	raw := os.Getenv("ARNES_PROVIDER_RETRIES")
	if raw == "" {
		return -1, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("ARNES_PROVIDER_RETRIES inválido: %q", raw)
	}
	return n, nil
}

// positiveIntFromEnv reads an optional positive integer from the named env var.
// Unset returns 0 (the caller's "use the default" signal); a non-numeric or
// non-positive value is an error.
func positiveIntFromEnv(name string) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s inválido: %q", name, raw)
	}
	return n, nil
}

// isFalsey reports whether s is an explicit "off" value.
func isFalsey(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "false", "off", "no":
		return true
	default:
		return false
	}
}

// isTruthy reports whether s is an explicit "on" value.
func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// checkForUpdate runs a throttled (once/day) check for a newer arnes release.
// When one exists it reports on notices; with auto on, it installs the release
// in place first and reports that instead. Any error is swallowed -- a failed
// check must never get in the way of starting up.
func checkForUpdate(auto bool, notices chan<- string) {
	stampPath, err := update.DefaultStampPath()
	if err != nil {
		return
	}
	stamp := update.Stamp{Path: stampPath}
	if !stamp.Due(time.Now(), 24*time.Hour) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rel, newer, err := update.Check(ctx, update.GitHub{Repo: repo}, version)
	_ = stamp.Mark(time.Now()) // even on error: don't retry until tomorrow
	if err != nil || !newer {
		return
	}

	msg := "hay una versión nueva de arnes (" + rel.Version + ") — usá /update-arnes"
	if auto {
		switch self, err := update.SelfPath(); {
		case err != nil:
			// fall through with the plain notice
		default:
			if err := update.Apply(ctx, rel, self); err == nil {
				msg = "arnes se actualizó a " + rel.Version + " — reiniciá para usarla"
			} else {
				msg = "no pude autoactualizar a " + rel.Version + " (" + err.Error() + ") — probá /update-arnes"
			}
		}
	}
	select {
	case notices <- msg:
	default:
	}
}

// costLine formats the running cost of a session for the status bar. It returns
// "" when the model has no known price.
func costLine(model string, in, out int) string {
	usd, ok := provider.Cost(model, in, out)
	if !ok {
		return ""
	}
	return fmt.Sprintf("$%.4f", usd)
}

// configPath is ARNES_CONFIG or the default location.
func configPath() (string, error) {
	if p := os.Getenv("ARNES_CONFIG"); p != "" {
		return p, nil
	}
	return config.DefaultPath()
}
