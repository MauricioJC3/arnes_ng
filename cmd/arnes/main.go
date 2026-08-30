// Command arnes wires a provider, the base tools, the approval gateway and the
// agent into a REPL over stdin/stdout, persisting each turn to a session file.
//
// Environment:
//
//	ARNES_PROVIDER      anthropic (default) | deepseek | kimi | openai
//	ARNES_MODEL         optional model override for the chosen provider
//	ARNES_MODE          permission mode at startup: normal (default) | auto | plan
//	ARNES_RESUME        session id (or unique prefix) to resume on start
//	ARNES_COMPACT       auto-compaction: sliding (default) | summarize | off
//	ARNES_COMPACT_AT    token threshold for auto-compaction (default 120000)
//	ARNES_MAX_STEPS     tool round-trips allowed per turn (default 50)
//	ARNES_MAX_TOKENS    output-token cap per model call (default 8192)
//	ARNES_MAX_TOOL_OUTPUT  byte cap on one tool result before its middle is elided (default 200000)
//	ARNES_CONTEXT_GUARD   estimated-token ceiling that forces a mid-turn compaction (default 150000)
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
//
// run() is the composition root: it reads config (see wiring.go), builds the
// ports, assembles them into an app.Deps, and hands control to the TUI or the
// plain REPL. The application logic lives in internal/app.
package main

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/app"
	"github.com/MauricioJC3/arnes_ng/internal/checkpoint"
	"github.com/MauricioJC3/arnes_ng/internal/compact"
	"github.com/MauricioJC3/arnes_ng/internal/config"
	"github.com/MauricioJC3/arnes_ng/internal/hook"
	"github.com/MauricioJC3/arnes_ng/internal/lsp"
	"github.com/MauricioJC3/arnes_ng/internal/mcp"
	"github.com/MauricioJC3/arnes_ng/internal/memory"
	"github.com/MauricioJC3/arnes_ng/internal/repl"
	"github.com/MauricioJC3/arnes_ng/internal/rules"
	"github.com/MauricioJC3/arnes_ng/internal/session"
	"github.com/MauricioJC3/arnes_ng/internal/skill"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
	"github.com/MauricioJC3/arnes_ng/internal/tui"
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

	// Per-turn tool-step budget: ARNES_MAX_STEPS wins over the config; 0 lets the
	// app apply agent.DefaultMaxSteps.
	maxSteps, err := maxStepsFromEnv(cfg)
	if err != nil {
		return err
	}

	// Per-call output-token cap: ARNES_MAX_TOKENS wins over the config; 0 lets
	// the app apply agent.DefaultMaxTokens.
	maxTokens, err := maxTokensFromEnv(cfg)
	if err != nil {
		return err
	}

	// Safety nets against a single turn overflowing the model's context window:
	// a byte cap per tool result, and an estimated-token ceiling that forces a
	// mid-turn compaction. Both fall back to the agent's own defaults when 0.
	maxToolOut, err := positiveIntFromEnv("ARNES_MAX_TOOL_OUTPUT")
	if err != nil {
		return err
	}
	contextGuard, err := positiveIntFromEnv("ARNES_CONTEXT_GUARD")
	if err != nil {
		return err
	}

	// A once-a-day background check for a newer release. It never blocks startup;
	// the result (if any) is surfaced on notices.
	notices := make(chan string, 1)
	go checkForUpdate(cfg.AutoUpdate || isTruthy(os.Getenv("ARNES_AUTO_UPDATE")), notices)

	approver, approvals, deltas := buildApprover(uiMode, streaming, stdin, os.Stdout)

	// The tool-activity feed: the agent's pre-execute observer pushes a short
	// line per tool call, the TUI renders them dim in the transcript. Buffered
	// and drop-on-full (see App.emitActivity), TUI only.
	var activity chan string
	if uiMode == "tui" {
		activity = make(chan string, 128)
	}

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
		MaxSteps:      maxSteps,
		MaxTokens:     maxTokens,
		MaxToolResult: maxToolOut,
		ContextGuard:  contextGuard,
		Streaming:     streaming,
		Deltas:        deltas,
		Activity:      activity,
		Checkpoints:   checkpoint.NewStore(),
		Mem:           mem,
		Rules:         rulesWrapped,
		Subagents:     subReg,
		Version:       version,
		Repo:          repo,
		Todos:         todoStore,
		CheckCommand:  cmp.Or(os.Getenv("ARNES_CHECK_CMD"), cfg.CheckCommand),
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
		Files:  tool.NewFileTracker(),
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
			Activity:  activity,
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
