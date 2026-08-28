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
//	ARNES_HOOKS         path to a hooks.json file (default ~/.arnes/hooks.json)
//	ARNES_UI            tui (default) | plain
//	ARNES_STREAM        off to disable live token streaming in the TUI
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
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/MauricioJC3/arnes_ng/internal/agent"
	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/checkpoint"
	"github.com/MauricioJC3/arnes_ng/internal/command"
	"github.com/MauricioJC3/arnes_ng/internal/compact"
	"github.com/MauricioJC3/arnes_ng/internal/config"
	"github.com/MauricioJC3/arnes_ng/internal/hook"
	"github.com/MauricioJC3/arnes_ng/internal/mcp"
	"github.com/MauricioJC3/arnes_ng/internal/memory"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/repl"
	"github.com/MauricioJC3/arnes_ng/internal/rules"
	"github.com/MauricioJC3/arnes_ng/internal/session"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
	"github.com/MauricioJC3/arnes_ng/internal/tui"
	"github.com/MauricioJC3/arnes_ng/internal/update"
)

const systemPrompt = `Sos un agente de programación que corre en la terminal del usuario, dentro de un arnés.
Trabajás sobre el código del proyecto en el directorio actual.

## Cómo trabajás

- Antes de cambiar algo, LEELO. Usá read_file / grep / glob para entender el código y sus
  convenciones. No adivines nombres de funciones, rutas ni APIs.
- Los cambios son quirúrgicos: el diff más chico que resuelve el problema, respetando el estilo
  del archivo (indentación, naming, densidad de comentarios).
- Verificá lo que hacés: corré los tests y el build relevantes con bash antes de dar algo por
  terminado. Si algo falla, decilo con la salida real; no lo maquilles.
- Terminá cuando la tarea está hecha. No agregues features, refactors ni "mejoras" que no se
  pidieron.

## Herramientas

- grep: buscar texto/patrones en el código. NO uses bash con grep/find/rg para esto.
- glob: encontrar archivos por patrón (ej. "internal/**/*_test.go").
- read_file: leer un archivo completo.
- edit_file: cambio puntual (reemplazo exacto de un fragmento). Es lo que usás para editar.
- write_file: SOLO para archivos nuevos o reescrituras completas. Para editar algo existente,
  edit_file.
- bash: ejecutar cosas (tests, build, git, binarios). Un exit code distinto de cero no es un
  error de la herramienta: se reporta en la salida y vos decidís cómo seguir.
- todo_write: la lista de tareas del trabajo actual, visible para el usuario. Para tareas de
  varios pasos, armá la lista al principio y actualizala (pasando SIEMPRE la lista completa) a
  medida que avanzás: un solo ítem in_progress por vez, marcá completed apenas terminás cada uno.
  Para tareas triviales de un paso no la uses.
- remember / recall: memoria persistente entre sesiones. Guardá decisiones, convenciones y
  datos del proyecto que no sean obvios del código. Consultala cuando el usuario haga
  referencia a algo previo.

## Permisos

Cada uso de una herramienta pasa por aprobación humana. Si el usuario deniega una, no insistas:
adaptá el plan y seguí con lo que sí podés hacer, o explicá qué te falta.

## Estilo

Conciso y directo. Nada de preámbulos ("Voy a...", "Perfecto, entonces...") ni resúmenes al
final salvo que se pidan. Respondé en el idioma del usuario.`

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

	// A once-a-day background check for a newer release. It never blocks startup;
	// the result (if any) is surfaced on notices.
	notices := make(chan string, 1)
	go checkForUpdate(cfg.AutoUpdate || isTruthy(os.Getenv("ARNES_AUTO_UPDATE")), notices)

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
	// todo_write only mutates the in-memory checklist, so it never needs a prompt.
	approver = approval.NewSafe(approver, "todo_write")

	// The task checklist: the model keeps it via todo_write, the TUI renders it
	// live. A buffered, latest-wins channel decouples the two goroutines.
	todoStore := todo.NewStore()
	todos := make(chan []todo.Item, 1)
	todoStore.OnChange(func(items []todo.Item) {
		for {
			select {
			case todos <- items:
				return
			default:
				select {
				case <-todos:
				default:
				}
			}
		}
	})

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
		checkpoints:   checkpoint.NewStore(),
	}

	subDefs, err := loadSubagents()
	if err != nil {
		return err
	}
	a.subagents = subagent.NewRegistry(subDefs...)

	hookCfg, err := loadHooks()
	if err != nil {
		return err
	}
	hookCount := len(hookCfg.PreTool) + len(hookCfg.PostTool)
	if !hookCfg.Empty() {
		a.hooks = hook.New(hookCfg, 30*time.Second)
	}

	cwd, _ := os.Getwd()
	rulesText, rulesSrc, err := rules.Load(cwd, os.Getenv("ARNES_RULES"))
	if err != nil {
		return err
	}
	a.rules = rules.Wrap(rulesText, rulesSrc)
	rulesLabel := "sin reglas"
	if rulesSrc != "" {
		rulesLabel = "reglas " + rulesSrc
	}

	// The base pool has every tool except delegate; the agent's registry is the
	// base plus delegate. Subagents draw from the base only (no recursion).
	base := tool.NewRegistry(
		tool.Bash{Timeout: 30 * time.Second},
		tool.Grep{},
		tool.Glob{},
		tool.ReadFile{},
		tool.WriteFile{},
		tool.EditFile{},
		tool.TodoWrite{Store: todoStore},
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

	summary := fmt.Sprintf("proveedor %s · modelo %s · modo %s · %s · sesión %s · compactación %s · subagentes %d · mcp %d tools · hooks %d",
		a.providerName, prov.Model(), a.mode, rulesLabel, a.sess.ID, a.ag.CompactorName(), a.subagents.Len(), mcpTools, hookCount)

	if uiMode == "tui" {
		theme, err := loadTheme()
		if err != nil {
			return err
		}
		return tui.Run(tui.Options{
			Conv:       a,
			ProviderFn: func() provider.Provider { return a.prov },
			SessionID:  func() string { return a.sess.ID },
			Stats:      func() int { return compact.EstimateTokens(a.ag.History()) },
			Cost: func() string {
				in, out := a.SessionUsage()
				return costLine(a.prov.Model(), in, out)
			},
			Approvals: approvals,
			Deltas:    deltas,
			Notices:   notices,
			Todos:     todos,
			Theme:     theme,
			Greeting:  summary,
			ListModels: func(ctx context.Context, providerName, apiKey string) ([]string, error) {
				return listModels(ctx, mergeEnvKeys(startup), providerName, apiKey)
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
	hooks         agent.Hooks       // pre/post tool-call hooks; nil when none configured
	checkpoints   *checkpoint.Store // per-turn restore points for /rewind
	rules         string            // project rules, already wrapped for the system prompt

	sess            *session.Session
	ag              *agent.Agent
	conv            *session.Persisting
	usedIn, usedOut int // cumulative token usage for the current session
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

// ActiveProvider implements command.Modeler.
func (a *app) ActiveProvider() string { return a.providerName }

// Model implements command.Modeler.
func (a *app) Model() string { return a.prov.Model() }

// KeyedProviders implements command.Modeler: the active provider first, then any
// other provider that has an API key configured (file or environment).
func (a *app) KeyedProviders() []string {
	merged := mergeEnvKeys(a.cfg)
	var rest []string
	for name := range providerKeyEnv {
		if name == a.providerName {
			continue
		}
		if strings.TrimSpace(merged.Keys[name]) != "" {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	return append([]string{a.providerName}, rest...)
}

// SetModel implements command.Modeler: change the model on the active provider
// and persist it to the config file.
func (a *app) SetModel(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", errors.New("modelo vacío")
	}
	a.prov.SetModel(model)
	a.cfg.Model = model
	if err := a.cfg.Save(a.cfgPath); err != nil {
		return "", fmt.Errorf("modelo cambiado a %s, pero no se pudo guardar en %s: %w", a.prov.Model(), a.cfgPath, err)
	}
	return fmt.Sprintf("modelo: %s (%s)", a.prov.Model(), a.providerName), nil
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
		agent.WithSystem(systemPrompt + a.rules + modeAddendum(a.mode)),
		agent.WithHistory(history),
		agent.WithWarnFn(func(err error) { fmt.Fprintln(os.Stderr, "arnés:", err) }),
	}
	if a.autoCompactor != nil {
		opts = append(opts, agent.WithCompactor(a.autoCompactor), agent.WithCompactThreshold(a.compactAt))
	}
	if a.hooks != nil {
		opts = append(opts, agent.WithHooks(a.hooks))
	}
	if a.checkpoints != nil {
		opts = append(opts, agent.WithToolObserver(a.checkpoints.Observe))
	}
	if a.streaming {
		opts = append(opts, agent.WithStreaming(true))
		if a.deltas != nil {
			opts = append(opts, agent.WithDeltaFn(func(s string) { a.deltas <- s }))
		}
	}
	// Seed the new agent with the session's running total so /mode, /compact and
	// /model don't reset the cost.
	opts = append(opts, agent.WithUsage(a.usedIn, a.usedOut))

	a.ag = agent.New(a.prov, a.tools, a.effectiveApprover(), opts...)
	a.sess = sess
	a.conv = session.NewPersisting(a.ag, a.store, sess, session.WithModelFn(func() string { return a.prov.Model() }))
}

// FreshConversation implements command.FreshFactory: a bare agent with empty
// history (same provider, tools, approver, mode) for /goal --fresh. Not
// persisted -- state for the fresh Ralph loop lives in files and git.
func (a *app) FreshConversation() command.Conversation {
	opts := []agent.Option{
		agent.WithSystem(systemPrompt + a.rules + modeAddendum(a.mode)),
		agent.WithWarnFn(func(err error) { fmt.Fprintln(os.Stderr, "arnés:", err) }),
	}
	if a.hooks != nil {
		opts = append(opts, agent.WithHooks(a.hooks))
	}
	if a.checkpoints != nil {
		opts = append(opts, agent.WithToolObserver(a.checkpoints.Observe))
	}
	if a.streaming {
		opts = append(opts, agent.WithStreaming(true))
		if a.deltas != nil {
			opts = append(opts, agent.WithDeltaFn(func(s string) { a.deltas <- s }))
		}
	}
	return agent.New(a.prov, a.tools, a.effectiveApprover(), opts...)
}

// SelfUpdate implements command.Updater: check GitHub for a newer release and,
// if there is one, replace this binary with it. It blocks while downloading.
func (a *app) SelfUpdate(ctx context.Context) (string, error) {
	rel, newer, err := update.Check(ctx, update.GitHub{Repo: repo}, version)
	if err != nil {
		return "", err
	}
	if !newer {
		return "arnes " + version + " ya está al día", nil
	}
	self, err := update.SelfPath()
	if err != nil {
		return "", err
	}
	if err := update.Apply(ctx, rel, self); err != nil {
		return "", err
	}
	return fmt.Sprintf("actualizado %s → %s · reiniciá arnes para usar la nueva versión", version, rel.Version), nil
}

// Run implements repl.Conversation. It snapshots a restore point, delegates to
// the live conversation, and keeps the session's cumulative token usage in sync
// with the agent.
func (a *app) Run(ctx context.Context, in string) (string, error) {
	if a.checkpoints != nil {
		a.checkpoints.Begin(a.ag.History(), in)
	}
	out, err := a.conv.Run(ctx, in)
	a.usedIn, a.usedOut = a.ag.Usage()
	return out, err
}

// ListCheckpoints implements command.Rewinder.
func (a *app) ListCheckpoints() string { return a.checkpoints.Summary() }

// Rewind implements command.Rewinder: restore the files captured since
// checkpoint n and rebuild the agent from that checkpoint's history.
func (a *app) Rewind(n int) (string, error) {
	cp, err := a.checkpoints.Rewind(n)
	if cp == nil {
		return "", err
	}
	hist := cp.History()
	a.sess.Messages = hist
	a.rebuild(a.sess, hist)
	if saveErr := a.store.Save(a.sess); saveErr != nil {
		return "", fmt.Errorf("rewind aplicado, pero no se pudo guardar la sesión: %w", saveErr)
	}
	msg := fmt.Sprintf("rewind al checkpoint %d · %d archivo(s) restaurado(s) · historial en %d mensajes",
		n, cp.Files(), len(hist))
	if err != nil {
		return msg, err // partial: some files failed to restore
	}
	return msg, nil
}

// SessionUsage returns the cumulative token usage since this session started.
func (a *app) SessionUsage() (in, out int) { return a.usedIn, a.usedOut }

// CostReport implements command.Coster: the current session's spend plus a
// per-session history, with a total for the models that have a known price.
func (a *app) CostReport() (string, error) {
	metas, err := a.store.List()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "sesión actual %s · %s · %s\n", a.sess.ID, a.prov.Model(), usageStr(a.prov.Model(), a.usedIn, a.usedOut))

	if len(metas) > 0 {
		b.WriteString("\nhistorial:\n")
		var total float64
		var haveTotal bool
		for _, m := range metas {
			mark := ""
			if m.ID == a.sess.ID {
				mark = "  ← actual"
			}
			fmt.Fprintf(&b, "  %s  %-16s  %8s tok  %s%s\n",
				m.ID, m.Model, humanCount(m.UsageIn+m.UsageOut), usageStr(m.Model, m.UsageIn, m.UsageOut), mark)
			if usd, ok := provider.Cost(m.Model, m.UsageIn, m.UsageOut); ok {
				total += usd
				haveTotal = true
			}
		}
		if haveTotal {
			fmt.Fprintf(&b, "\ntotal (modelos con tarifa conocida): $%.4f\n", total)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// usageStr renders a token pair as a dollar figure, or "sin tarifa" when the
// model has no known price.
func usageStr(model string, in, out int) string {
	if usd, ok := provider.Cost(model, in, out); ok {
		return fmt.Sprintf("$%.4f", usd)
	}
	return "sin tarifa"
}

// humanCount abbreviates a token count: 1234 -> "1.2k", 2_000_000 -> "2.0M".
func humanCount(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
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
	a.usedIn, a.usedOut = s.UsageIn, s.UsageOut // continue the session's spend
	a.rebuild(s, s.Messages)
	return fmt.Sprintf("reanudada %s (%d mensajes)", s.ID, len(s.Messages)), nil
}

// NewSession implements repl.Sessions.
func (a *app) NewSession() (string, error) {
	cwd, _ := os.Getwd()
	s := session.New(a.providerName, a.prov.Model(), cwd)
	a.usedIn, a.usedOut = 0, 0
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

// listModels builds a throwaway provider for providerName -- using apiKey, or
// the key already known in base when apiKey is empty -- and asks it for the
// model ids it can serve. Used by the /connect picker.
func listModels(ctx context.Context, base config.Config, providerName, apiKey string) ([]string, error) {
	keys := map[string]string{}
	for k, v := range base.Keys {
		keys[k] = v
	}
	if apiKey != "" {
		keys[providerName] = apiKey
	}
	p, _, err := providerFromConfig(config.Config{Provider: providerName, Keys: keys})
	if err != nil {
		return nil, err
	}
	lister, ok := p.(provider.ModelLister)
	if !ok {
		return nil, fmt.Errorf("%s no permite listar modelos", providerName)
	}
	return lister.ListModels(ctx)
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
		return provider.NewDeepSeek(key("deepseek"), cmp.Or(model, "deepseek-v4-flash")), name, nil
	case "kimi":
		return provider.NewKimi(key("kimi"), cmp.Or(model, "moonshot-v1-8k")), name, nil
	case "openai":
		return provider.NewOpenAI(key("openai"), cmp.Or(model, "gpt-4o")), name, nil
	default:
		return nil, "", fmt.Errorf("provider desconocido: %q (anthropic|deepseek|kimi|openai)", name)
	}
}
