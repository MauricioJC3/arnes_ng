// Command arnes wires a provider, the base tools, the approval gateway and the
// agent into a REPL over stdin/stdout, persisting each turn to a session file.
//
// Environment:
//
//	ARNES_PROVIDER      anthropic (default) | deepseek | kimi | openai
//	ARNES_MODEL         optional model override for the chosen provider
//	ARNES_RESUME        session id (or unique prefix) to resume on start
//	ARNES_COMPACT       auto-compaction: off (default) | sliding | summarize
//	ARNES_COMPACT_AT    token threshold for auto-compaction (default 120000)
//	ARNES_SUBAGENTS     path to a subagents JSON file (default ~/.arnes/subagents.json)
//	ARNES_MCP           path to an mcp.json file (default ~/.arnes/mcp.json)
//	ARNES_UI            tui (default) | plain
//	ARNES_STREAM        off to disable live token streaming in the TUI
//	ARNES_THEME         path to a theme JSON file (default ~/.arnes/theme.json)
//	ARNES_CONFIG        path to the settings file (default ~/.arnes/config.json)
//	ANTHROPIC_API_KEY / DEEPSEEK_API_KEY / MOONSHOT_API_KEY / OPENAI_API_KEY
//
// Provider, model and API keys can also live in the config file and be set at
// runtime with /connect. Environment variables always win over the file.
package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/andresmjimenez/arnes/internal/agent"
	"github.com/andresmjimenez/arnes/internal/approval"
	"github.com/andresmjimenez/arnes/internal/compact"
	"github.com/andresmjimenez/arnes/internal/config"
	"github.com/andresmjimenez/arnes/internal/mcp"
	"github.com/andresmjimenez/arnes/internal/memory"
	"github.com/andresmjimenez/arnes/internal/provider"
	"github.com/andresmjimenez/arnes/internal/repl"
	"github.com/andresmjimenez/arnes/internal/session"
	"github.com/andresmjimenez/arnes/internal/subagent"
	"github.com/andresmjimenez/arnes/internal/tool"
	"github.com/andresmjimenez/arnes/internal/tui"
)

const systemPrompt = `Sos un asistente de programación que corre dentro de un arnés, en la terminal del usuario.
Tenés herramientas para ejecutar comandos de shell, leer y escribir archivos, y una memoria
persistente (remember / recall) para datos que deben sobrevivir entre sesiones.
Cada uso de una herramienta requiere aprobación humana: si el usuario la deniega, adaptá el plan y seguí.
Sé conciso y directo.`

func main() {
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

	startup := cfg.Clone()
	if v := os.Getenv("ARNES_PROVIDER"); v != "" {
		startup.Provider = v
	}
	if v := os.Getenv("ARNES_MODEL"); v != "" {
		startup.Model = v
	}
	prov, providerName, err := providerFromConfig(mergeEnvKeys(startup))
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
	mem, err := memory.NewFileStore(memPath)
	if err != nil {
		return err
	}

	autoCompactor, compactAt, err := compactionFromEnv(prov)
	if err != nil {
		return err
	}

	uiMode := strings.ToLower(cmp.Or(os.Getenv("ARNES_UI"), "tui"))
	streaming := uiMode == "tui" && !isFalsey(os.Getenv("ARNES_STREAM"))

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
		approver = approval.Prompt{In: stdin, Out: os.Stdout}
	}

	a := &app{
		providerName:  providerName,
		prov:          prov,
		cfg:           cfg,
		cfgPath:       cfgPath,
		store:         store,
		baseApprover:  approver,
		mode:          modeNormal,
		autoCompactor: autoCompactor,
		compactAt:     compactAt,
		streaming:     streaming,
		deltas:        deltas,
	}

	subDefs, err := loadSubagents()
	if err != nil {
		return err
	}
	a.subagents = subagent.NewRegistry(subDefs...)

	// The base pool has every tool except delegate; the agent's registry is the
	// base plus delegate. Subagents draw from the base only (no recursion).
	base := tool.NewRegistry(
		tool.Bash{Timeout: 30 * time.Second},
		tool.ReadFile{},
		tool.WriteFile{},
		tool.Remember{Store: mem},
		tool.Recall{Store: mem},
	)

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

	delegate := subagent.NewDelegateTool(a.subagents, func() provider.Provider { return a.prov }, base, approver,
		subagent.WithParentHistory(func() []provider.Message {
			if a.ag == nil {
				return nil
			}
			return a.ag.History()
		}),
	)
	a.tools = base.With(delegate)

	if id := os.Getenv("ARNES_RESUME"); id != "" {
		if _, err := a.ResumeSession(id); err != nil {
			return err
		}
	} else if _, err := a.NewSession(); err != nil {
		return err
	}

	summary := fmt.Sprintf("proveedor %s · modelo %s · modo %s · sesión %s · compactación %s · subagentes %d · mcp %d tools",
		a.providerName, prov.Model(), a.mode, a.sess.ID, a.ag.CompactorName(), a.subagents.Len(), mcpTools)

	if uiMode == "tui" {
		theme, err := loadTheme()
		if err != nil {
			return err
		}
		return tui.Run(tui.Options{
			Conv:      a,
			Provider:  a.prov,
			SessionID: func() string { return a.sess.ID },
			Stats:     func() int { return compact.EstimateTokens(a.ag.History()) },
			Approvals: approvals,
			Deltas:    deltas,
			Theme:     theme,
			Greeting:  summary,
		})
	}

	fmt.Println(summary)
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

// app holds the machinery to (re)build a conversation and owns the live one. It
// is the repl.Conversation, repl.Sessions, repl.Compaction and repl.Subagents
// the REPL talks to.
type app struct {
	providerName  string
	prov          provider.Provider
	cfg           config.Config
	cfgPath       string
	tools         *tool.Registry
	baseApprover  approval.Approver
	mode          string
	store         session.Store
	subagents     *subagent.Registry
	autoCompactor compact.Strategy
	compactAt     int
	streaming     bool
	deltas        chan string

	sess *session.Session
	ag   *agent.Agent
	conv *session.Persisting
}

// Connect implements command.Connector: switch provider (and optionally model /
// api key), rebuild the agent, and persist the choice to the config file.
func (a *app) Connect(providerName, model, apiKey string) (string, error) {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	if _, ok := providerKeyEnv[providerName]; !ok {
		return "", fmt.Errorf("provider desconocido: %q (anthropic|deepseek|kimi|openai)", providerName)
	}

	next := a.cfg.Clone()
	next.Provider = providerName
	if model != "" {
		next.Model = model
	}
	if apiKey != "" {
		next.SetKey(providerName, apiKey)
	}

	p, name, err := providerFromConfig(mergeEnvKeys(next))
	if err != nil {
		return "", err
	}

	a.cfg = next
	a.prov = p
	a.providerName = name
	a.rebuild(a.sess, a.sess.Messages)

	if err := a.cfg.Save(a.cfgPath); err != nil {
		return "", fmt.Errorf("conectado, pero no se pudo guardar en %s: %w", a.cfgPath, err)
	}

	extra := ""
	if apiKey != "" {
		extra = " · api key guardada"
	}
	return fmt.Sprintf("conectado: %s · modelo %s%s\nconfig: %s", name, p.Model(), extra, a.cfgPath), nil
}

// ListSubagents implements repl.Subagents.
func (a *app) ListSubagents() []string {
	var out []string
	for _, d := range a.subagents.All() {
		out = append(out, d.Name+": "+d.Description)
	}
	return out
}

// permission modes
const (
	modeNormal = "normal"
	modeAuto   = "auto"
	modePlan   = "plan"
)

// effectiveApprover is the gateway for the current mode.
func (a *app) effectiveApprover() approval.Approver {
	switch a.mode {
	case modeAuto:
		return approval.AllowAll{}
	case modePlan:
		return approval.ReadOnly{Allowed: map[string]bool{"read_file": true, "recall": true}}
	default:
		return a.baseApprover
	}
}

func modeAddendum(mode string) string {
	switch mode {
	case modePlan:
		return "\n\nMODO PLAN ACTIVO: no modifiques nada. Investigá solo con read_file y proponé un plan " +
			"detallado paso a paso. Las herramientas que escriben o ejecutan comandos van a ser denegadas."
	case modeAuto:
		return "\n\nMODO AUTO: las herramientas se ejecutan sin pedir confirmación. Cuidado con comandos destructivos."
	default:
		return ""
	}
}

// Mode implements command.Modes.
func (a *app) Mode() string { return a.mode }

// SetMode implements command.Modes: switch the permission mode and rebuild.
func (a *app) SetMode(name string) (string, error) {
	switch name = strings.ToLower(strings.TrimSpace(name)); name {
	case modeNormal, modeAuto, modePlan:
	case "bypass", "yolo":
		name = modeAuto
	default:
		return "", fmt.Errorf("modo desconocido: %q (normal|auto|plan)", name)
	}
	a.mode = name
	a.rebuild(a.sess, a.sess.Messages)
	return "modo: " + name, nil
}

// rebuild swaps in a fresh agent + persister for the given session/history.
func (a *app) rebuild(sess *session.Session, history []provider.Message) {
	opts := []agent.Option{
		agent.WithSystem(systemPrompt + modeAddendum(a.mode)),
		agent.WithHistory(history),
		agent.WithWarnFn(func(err error) { fmt.Fprintln(os.Stderr, "arnés:", err) }),
	}
	if a.autoCompactor != nil {
		opts = append(opts, agent.WithCompactor(a.autoCompactor), agent.WithCompactThreshold(a.compactAt))
	}
	if a.streaming {
		opts = append(opts, agent.WithStreaming(true))
		if a.deltas != nil {
			opts = append(opts, agent.WithDeltaFn(func(s string) { a.deltas <- s }))
		}
	}
	a.ag = agent.New(a.prov, a.tools, a.effectiveApprover(), opts...)
	a.sess = sess
	a.conv = session.NewPersisting(a.ag, a.store, sess, session.WithModelFn(func() string { return a.prov.Model() }))
}

// Run implements repl.Conversation, delegating to the live conversation.
func (a *app) Run(ctx context.Context, in string) (string, error) {
	return a.conv.Run(ctx, in)
}

// SetStrategy implements repl.Compaction: swap the strategy at runtime.
func (a *app) SetStrategy(name string) (string, error) {
	s, err := strategyByName(name, a.prov)
	if err != nil {
		return "", err
	}
	a.ag.SetCompactor(s)
	return "estrategia de compactación: " + s.Name(), nil
}

// Compact implements repl.Compaction: force compaction now and persist.
func (a *app) Compact() (string, error) {
	before, after, err := a.ag.Compact(context.Background())
	if err != nil {
		return "", err
	}
	a.sess.Messages = a.ag.History()
	if saveErr := a.store.Save(a.sess); saveErr != nil {
		return "", saveErr
	}
	return fmt.Sprintf("compactado con %s: ~%d → ~%d tokens", a.ag.CompactorName(), before, after), nil
}

// ListSessions implements repl.Sessions.
func (a *app) ListSessions() ([]session.Meta, error) { return a.store.List() }

// ResumeSession implements repl.Sessions: load by id or unique prefix, then swap.
func (a *app) ResumeSession(id string) (string, error) {
	s, err := a.resolve(id)
	if err != nil {
		return "", err
	}
	if s.Model != "" {
		a.prov.SetModel(s.Model)
	}
	a.rebuild(s, s.Messages)
	return fmt.Sprintf("reanudada %s (%d mensajes)", s.ID, len(s.Messages)), nil
}

// NewSession implements repl.Sessions.
func (a *app) NewSession() (string, error) {
	cwd, _ := os.Getwd()
	s := session.New(a.providerName, a.prov.Model(), cwd)
	a.rebuild(s, nil)
	return "sesión nueva: " + s.ID, nil
}

// resolve finds a session by exact id, or by an unambiguous id prefix.
func (a *app) resolve(id string) (*session.Session, error) {
	switch s, err := a.store.Load(id); {
	case err == nil:
		return s, nil
	case !errors.Is(err, session.ErrNotFound):
		return nil, err
	}

	metas, err := a.store.List()
	if err != nil {
		return nil, err
	}
	var match string
	for _, m := range metas {
		if !strings.HasPrefix(m.ID, id) {
			continue
		}
		if match != "" {
			return nil, fmt.Errorf("el prefijo %q es ambiguo", id)
		}
		match = m.ID
	}
	if match == "" {
		return nil, session.ErrNotFound
	}
	return a.store.Load(match)
}

// strategyByName resolves a /compact argument to a strategy.
func strategyByName(name string, p provider.Provider) (compact.Strategy, error) {
	switch strings.ToLower(name) {
	case "none", "off":
		return compact.None{}, nil
	case "sliding", "sliding-window":
		return compact.SlidingWindow{}, nil
	case "summarize", "summary":
		return compact.Summarize{Provider: p}, nil
	default:
		return nil, fmt.Errorf("estrategia desconocida: %q (none|sliding|summarize)", name)
	}
}

// compactionFromEnv reads ARNES_COMPACT / ARNES_COMPACT_AT into an auto-
// compaction strategy and threshold. Default: disabled.
func compactionFromEnv(p provider.Provider) (compact.Strategy, int, error) {
	name := strings.ToLower(cmp.Or(os.Getenv("ARNES_COMPACT"), "off"))
	if name == "off" || name == "none" || name == "" {
		return nil, 0, nil
	}
	s, err := strategyByName(name, p)
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

// providerKeyEnv maps a provider name to the env var holding its API key.
var providerKeyEnv = map[string]string{
	"anthropic": "ANTHROPIC_API_KEY",
	"deepseek":  "DEEPSEEK_API_KEY",
	"kimi":      "MOONSHOT_API_KEY",
	"openai":    "OPENAI_API_KEY",
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

// configPath is ARNES_CONFIG or the default location.
func configPath() (string, error) {
	if p := os.Getenv("ARNES_CONFIG"); p != "" {
		return p, nil
	}
	return config.DefaultPath()
}

// mergeEnvKeys returns a copy of cfg with any API key present in the environment
// layered on top. Provider/model are NOT touched here.
func mergeEnvKeys(cfg config.Config) config.Config {
	out := cfg.Clone()
	for prov, env := range providerKeyEnv {
		if v := os.Getenv(env); v != "" {
			out.SetKey(prov, v)
		}
	}
	return out
}

// providerFromConfig builds the provider named by cfg.Provider (default
// anthropic), using cfg.Model and cfg.Keys. Model defaults are placeholders --
// change them with /model or /connect.
func providerFromConfig(cfg config.Config) (provider.Provider, string, error) {
	name := strings.ToLower(cmp.Or(cfg.Provider, "anthropic"))
	model := cfg.Model
	key := func(p string) string { return cfg.Keys[p] }

	switch name {
	case "anthropic":
		var opts []option.RequestOption
		if k := key("anthropic"); k != "" {
			opts = append(opts, option.WithAPIKey(k))
		}
		p := provider.NewAnthropic(opts...)
		if model != "" {
			p.SetModel(model)
		}
		return p, name, nil
	case "deepseek":
		return provider.NewDeepSeek(key("deepseek"), cmp.Or(model, "deepseek-chat")), name, nil
	case "kimi":
		return provider.NewKimi(key("kimi"), cmp.Or(model, "moonshot-v1-8k")), name, nil
	case "openai":
		return provider.NewOpenAI(key("openai"), cmp.Or(model, "gpt-4o")), name, nil
	default:
		return nil, "", fmt.Errorf("provider desconocido: %q (anthropic|deepseek|kimi|openai)", name)
	}
}
