// Command arnes wires a provider, the base tools, the approval gateway and the
// agent into a REPL over stdin/stdout, persisting each turn to a session file.
//
// Environment:
//
//	ARNES_PROVIDER      anthropic (default) | deepseek | kimi | openai
//	ARNES_MODEL         optional model override for the chosen provider
//	ARNES_MODE          permission mode at startup: normal (default) | auto | plan
//	ARNES_RESUME        session id (or unique prefix) to resume on start
//	ARNES_COMPACT       auto-compaction: off (default) | sliding | summarize
//	ARNES_COMPACT_AT    token threshold for auto-compaction (default 120000)
//	ARNES_SUBAGENTS     path to a subagents JSON file (default ~/.arnes/subagents.json)
//	ARNES_SKILLS        path to the global skills directory (default ~/.arnes/skills)
//	ARNES_MCP           path to an mcp.json file (default ~/.arnes/mcp.json)
//	ARNES_HOOKS         path to a hooks.json file (default ~/.arnes/hooks.json)
//	ARNES_LSP           path to an lsp.json file (default ~/.arnes/lsp.json)
//	ARNES_UI            tui (default) | plain
//	ARNES_STREAM        off to disable live token streaming in the TUI
//	ARNES_MOUSE         on to capture the mouse for wheel scroll (off by default; Ctrl+O toggles)
//	ARNES_THEME         path to a theme JSON file (default ~/.arnes/theme.json)
//	ARNES_CONFIG        path to the settings file (default ~/.arnes/config.json)
//	ARNES_AUTO_UPDATE  on to let the daily check install a newer release itself
//	ANTHROPIC_API_KEY / DEEPSEEK_API_KEY / MOONSHOT_API_KEY / OPENAI_API_KEY
//
// Provider, model and API keys can also live in the config file and be set at
// runtime with /connect. Environment variables always win over the file.
package main

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
	"github.com/MauricioJC3/arnes_ng/internal/checkpoint"
	"github.com/MauricioJC3/arnes_ng/internal/compact"
	"github.com/MauricioJC3/arnes_ng/internal/config"
	"github.com/MauricioJC3/arnes_ng/internal/hook"
	"github.com/MauricioJC3/arnes_ng/internal/lsp"
	"github.com/MauricioJC3/arnes_ng/internal/mcp"
	"github.com/MauricioJC3/arnes_ng/internal/memory"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/repl"
	"github.com/MauricioJC3/arnes_ng/internal/rules"
	"github.com/MauricioJC3/arnes_ng/internal/session"
	"github.com/MauricioJC3/arnes_ng/internal/skill"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
	"github.com/MauricioJC3/arnes_ng/internal/tui"
	"github.com/MauricioJC3/arnes_ng/internal/update"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// repo is the GitHub "owner/name" the self-updater pulls releases from.
const repo = "MauricioJC3/arnes_ng"

func main() {
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-v" || a == "version" {
			fmt.Println("arnes", version)
			return
		}
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "arnés:", err)
		os.Exit(1)
	}
}

func run() error {
	stdin := bufio.NewReader(os.Stdin)

	cfgPath, err := configPath()
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	startup := startupConfig(cfg)
	prov, providerName, err := app.ProviderFromConfig(app.MergeEnvKeys(startup))
	if err != nil {
		return err
	}

	sessDir, err := session.DefaultDir()
	if err != nil {
		return err
	}
	store, err := session.NewFileStore(sessDir)
	if err != nil {
		return err
	}

	memPath, err := memory.DefaultPath()
	if err != nil {
		return err
	}
	memCWD, _ := os.Getwd()
	projID := memory.DetectID(memCWD)
	mem, err := memory.NewFileStore(memPath, projID)
	if err != nil {
		return err
	}

	autoCompactor, compactAt, err := compactionFromEnv(prov)
	if err != nil {
		return err
	}

	uiMode, streaming := resolveUI()

	// Permission mode: ARNES_MODE wins over the config file; default normal.
	startMode, err := app.ParseMode(cmp.Or(os.Getenv("ARNES_MODE"), cfg.Mode, app.ModeNormal))
	if err != nil {
		return err
	}

	// A once-a-day background check for a newer release. It never blocks startup;
	// the result (if any) is surfaced on notices.
	notices := make(chan string, 1)
	go checkForUpdate(cfg.AutoUpdate || isTruthy(os.Getenv("ARNES_AUTO_UPDATE")), notices)

	approver, approvals, deltas := buildApprover(uiMode, streaming, stdin, os.Stdout)

	// The task checklist: the model keeps it via todo_write, the TUI renders it
	// live. A buffered, latest-wins channel decouples the two goroutines.
	todoStore, todos := newTodoBridge()

	subDefs, err := loadSubagents()
	if err != nil {
		return err
	}
	subReg := subagent.NewRegistry(subDefs...)

	hookCfg, err := loadHooks()
	if err != nil {
		return err
	}
	hookCount := len(hookCfg.PreTool) + len(hookCfg.PostTool)

	cwd, _ := os.Getwd()
	rulesText, rulesSrc, err := rules.Load(cwd, os.Getenv("ARNES_RULES"))
	if err != nil {
		return err
	}
	rulesWrapped := rules.Wrap(rulesText, rulesSrc)
	rulesLabel := "sin reglas"
	if rulesSrc != "" {
		rulesLabel = "reglas " + rulesSrc
	}

	lspCfg, err := loadLSP()
	if err != nil {
		return err
	}
	lspMgr := lsp.NewManager(lspCfg, cwd)
	defer lspMgr.CloseAll()

	skills, err := loadSkills(cwd)
	if err != nil {
		return err
	}
	skillReg := skill.NewRegistry(skills...)

	deps := app.Deps{
		ProviderName:  providerName,
		Provider:      prov,
		Cfg:           cfg,
		CfgPath:       cfgPath,
		Store:         store,
		BaseApprover:  approver,
		Mode:          startMode,
		AutoCompactor: autoCompactor,
		CompactAt:     compactAt,
		Streaming:     streaming,
		Deltas:        deltas,
		Checkpoints:   checkpoint.NewStore(),
		Mem:           mem,
		Rules:         rulesWrapped,
		Subagents:     subReg,
		Version:       version,
		Repo:          repo,
	}
	if !hookCfg.Empty() {
		deps.Hooks = hook.New(hookCfg, 30*time.Second)
	}
	a := app.New(deps)

	// The base pool has every tool except delegate; the agent's registry is the
	// base plus delegate. Subagents draw from the base only (no recursion).
	base := app.BuildBaseTools(app.BaseToolDeps{
		Todos:  todoStore,
		LSPMgr: lspMgr,
		Skills: skillReg,
		Mem:    mem,
	})

	mcpTools := 0
	if mcpCfg, err := loadMCP(); err != nil {
		return err
	} else if len(mcpCfg.MCPServers) > 0 {
		fmt.Printf("conectando %d servidor(es) MCP...\n", len(mcpCfg.MCPServers))
		mgr := mcp.Connect(context.Background(), mcpCfg, func(err error) { fmt.Fprintln(os.Stderr, "arnés:", err) })
		defer mgr.Close()
		base = base.With(mgr.Tools()...)
		mcpTools = len(mgr.Tools())
	}

	delegate := subagent.NewDelegateTool(subReg, a.Provider, base, a.EffectiveApprover,
		subagent.WithParentHistory(a.History),
	)
	a.SetTools(base.With(delegate))

	if id := os.Getenv("ARNES_RESUME"); id != "" {
		if _, err := a.ResumeSession(id); err != nil {
			return err
		}
	} else if _, err := a.NewSession(); err != nil {
		return err
	}

	summary := a.StartupSummary(app.StartupInfo{
		RulesLabel: rulesLabel,
		Skills:     skillReg.Len(),
		MCPTools:   mcpTools,
		Hooks:      hookCount,
		LSPServers: lspMgr.Configured(),
		ProjID:     projID,
	})

	if uiMode == "tui" {
		theme, err := loadTheme()
		if err != nil {
			return err
		}
		return tui.Run(tui.Options{
			Conv:       a,
			ProviderFn: a.Provider,
			SessionID:  a.SessionID,
			Stats:      func() int { return compact.EstimateTokens(a.History()) },
			Cost: func() string {
				in, out := a.SessionUsage()
				return costLine(a.Model(), in, out)
			},
			Approvals: approvals,
			Deltas:    deltas,
			Notices:   notices,
			Todos:     todos,
			Theme:     theme,
			Greeting:  summary,
			ListModels: func(ctx context.Context, providerName, apiKey string) ([]string, error) {
				return app.ListModels(ctx, app.MergeEnvKeys(startup), providerName, apiKey)
			},
		})
	}

	fmt.Println(summary)
	go func() {
		if msg := <-notices; msg != "" {
			fmt.Fprintln(os.Stdout, "\narnés:", msg)
		}
	}()
	return repl.New(a, prov, stdin, os.Stdout).Run(context.Background())
}

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
// ~/.arnes/skills) skill directories.
func loadSkills(cwd string) ([]skill.Skill, error) {
	global := os.Getenv("ARNES_SKILLS")
	if global == "" {
		p, err := skill.DefaultDir()
		if err != nil {
			return nil, err
		}
		global = p
	}
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
// compaction strategy and threshold. Default: disabled.
func compactionFromEnv(p provider.Provider) (compact.Strategy, int, error) {
	name := strings.ToLower(cmp.Or(os.Getenv("ARNES_COMPACT"), "off"))
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
