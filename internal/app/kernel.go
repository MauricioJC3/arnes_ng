// Package app is the application core: it owns the live conversation and every
// use case the front-ends drive through the command interfaces (switch provider,
// change mode, resume a session, compact, rewind, report cost, self-update).
//
// The composition root (cmd/arnes) builds the ports -- provider, session store,
// approval gateway, tool pool, memory, checkpoints -- and hands them to New as
// Deps. Nothing here reads the environment or the filesystem layout directly;
// that stays in cmd/arnes.
//
// The kernel is rebuild: every mutation of provider, mode, session or compaction
// ends by calling it, and it is the only place the agent and its persister are
// constructed. The live state (sess, ag, conv, token counters) is what those
// use cases mutate.
package app

import (
	"context"
	"fmt"
	"os"

	"github.com/MauricioJC3/arnes_ng/internal/agent"
	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/checkpoint"
	"github.com/MauricioJC3/arnes_ng/internal/command"
	"github.com/MauricioJC3/arnes_ng/internal/compact"
	"github.com/MauricioJC3/arnes_ng/internal/config"
	"github.com/MauricioJC3/arnes_ng/internal/memory"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/session"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
)

const systemPrompt = `Sos un agente de programación que corre en la terminal del usuario, dentro de un arnés.
Trabajás sobre el código del proyecto en el directorio actual.

## Cómo trabajás

- Antes de cambiar algo, LEELO. Usá read_file / grep / glob para entender el código y sus
  convenciones. No adivines nombres de funciones, rutas ni APIs.
- Herramientas independientes van juntas: pedí varias lecturas o búsquedas en una sola
  respuesta en lugar de una por vuelta.
- Los cambios son quirúrgicos: el diff más chico que resuelve el problema, respetando el estilo
  del archivo (indentación, naming, densidad de comentarios).
- Verificá lo que hacés: antes de dar algo por terminado corré la verificación del proyecto
  (tests y, según el lenguaje, vet/lint/typecheck). Si algo falla, decilo con la salida real;
  no lo maquilles.
- Si una herramienta falla dos veces por la misma razón, pará y explicá el bloqueo. No repitas
  el mismo intento a ciegas.
- Terminá cuando la tarea está hecha. No agregues features, refactors ni "mejoras" que no se
  pidieron.

## Delegación

- delegate + research: para EXPLORAR o mapear código amplio ("dónde está X", "cómo funciona Y",
  varios archivos) sin llenarte el contexto. Devuelve un resumen con archivos y líneas.
- delegate + test-writer: para escribir los tests de un archivo puntual.
- Lo que resolvés en una o dos lecturas hacelo vos; no delegues tareas chicas.

## Herramientas

- grep: buscar texto/patrones en el código. NO uses bash con grep/find/rg para esto.
- glob: encontrar archivos por patrón (ej. "internal/**/*_test.go").
- read_file: leer un archivo completo.
- edit_file: cambio puntual (reemplazo exacto de un fragmento). Es lo que usás para editar.
  Para varios cambios en el mismo archivo pasá el array "edits" y se aplican en una sola llamada.
- write_file: SOLO para archivos nuevos o reescrituras completas. Para editar algo existente,
  edit_file.
- bash: ejecutar cosas (tests, build, git, binarios). Un exit code distinto de cero no es un
  error de la herramienta: se reporta en la salida y vos decidís cómo seguir.
- todo_write: la lista de tareas del trabajo actual, visible para el usuario. Para tareas de
  varios pasos, armá la lista al principio y actualizala (pasando SIEMPRE la lista completa) a
  medida que avanzás: un solo ítem in_progress por vez, marcá completed apenas terminás cada uno.
  Para tareas triviales de un paso no la uses.
- lsp: consultá el language server sobre un archivo — diagnósticos (errores/warnings),
  definición o hover (tipo/doc) de un símbolo. Después de editar, lsp con action "diagnostics"
  sobre el archivo es un chequeo rápido antes de correr toda la suite. Puede no estar
  configurado para el lenguaje del archivo.
- skill: si la tarea coincide con un skill de la lista (la ves en la descripción de la tool),
  invocá skill con su nombre ANTES de encararla y seguí esas instrucciones en lugar de tu
  enfoque por defecto. Si ninguno aplica, no la uses.
- remember / recall: memoria persistente entre sesiones, POR PROYECTO. Guardá decisiones,
  convenciones y datos del proyecto que no sean obvios del código, apenas los descubrís o el
  usuario los define — no esperes a que te lo pidan. Consultá con recall cuando el usuario haga
  referencia a algo previo. Si arriba hay una sección "Memoria del proyecto", es lo ya guardado.

## Permisos

Cada uso de una herramienta pasa por aprobación humana. Si el usuario deniega una, no insistas:
adaptá el plan y seguí con lo que sí podés hacer, o explicá qué te falta. Un hook del proyecto
también puede bloquear una llamada (p. ej. correr tests antes de un commit): resolvé lo que el
hook pide y reintentá, no lo esquives.

## Estilo

Conciso y directo. Nada de preámbulos ("Voy a...", "Perfecto, entonces...") ni resúmenes al
final salvo que se pidan. Respondé en el idioma del usuario.`

// Deps is everything the app needs from the composition root. The caller builds
// these (reading the environment, the config file and the default paths) and
// passes them to New; the app never touches those itself.
type Deps struct {
	ProviderName  string
	Provider      provider.Provider
	Cfg           config.Config
	CfgPath       string
	Store         session.Store
	BaseApprover  approval.Approver
	Mode          string
	AutoCompactor compact.Strategy
	CompactAt     int
	Streaming     bool
	Deltas        chan string
	Hooks         agent.Hooks       // pre/post tool-call hooks; nil when none configured
	Checkpoints   *checkpoint.Store // per-turn restore points for /rewind
	Mem           memory.Store      // project-scoped persistent memory (for the prompt digest)
	Rules         string            // project rules, already wrapped for the system prompt
	Subagents     *subagent.Registry
	Version       string // running binary version, for /update-arnes
	Repo          string // GitHub owner/name the self-updater pulls from
}

// App holds the machinery to (re)build a conversation and owns the live one. It
// is the command.Conversation, command.Sessions, command.Compaction and the rest
// of the command interfaces the front-ends talk to.
type App struct {
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
	hooks         agent.Hooks
	checkpoints   *checkpoint.Store
	mem           memory.Store
	rules         string
	version       string
	repo          string

	sess            *session.Session
	ag              *agent.Agent
	conv            *session.Persisting
	usedIn, usedOut int // cumulative token usage for the current session
}

// New builds an App from its dependencies. The tool pool is set separately with
// SetTools, because the delegate tool needs a reference to the App itself.
func New(d Deps) *App {
	return &App{
		providerName:  d.ProviderName,
		prov:          d.Provider,
		cfg:           d.Cfg,
		cfgPath:       d.CfgPath,
		store:         d.Store,
		baseApprover:  d.BaseApprover,
		mode:          d.Mode,
		autoCompactor: d.AutoCompactor,
		compactAt:     d.CompactAt,
		streaming:     d.Streaming,
		deltas:        d.Deltas,
		hooks:         d.Hooks,
		checkpoints:   d.Checkpoints,
		mem:           d.Mem,
		rules:         d.Rules,
		subagents:     d.Subagents,
		version:       d.Version,
		repo:          d.Repo,
	}
}

// SetTools installs the agent's tool pool. Call once, after New, before the
// first session is opened.
func (a *App) SetTools(tools *tool.Registry) { a.tools = tools }

// Provider is the live provider. Used by the composition root to wire the TUI
// status bar and the delegate tool.
func (a *App) Provider() provider.Provider { return a.prov }

// History is the live agent's message history (nil before the first session).
func (a *App) History() []provider.Message {
	if a.ag == nil {
		return nil
	}
	return a.ag.History()
}

// SessionID is the id of the live session.
func (a *App) SessionID() string {
	if a.sess == nil {
		return ""
	}
	return a.sess.ID
}

// CompactorName is the name of the live agent's compaction strategy.
func (a *App) CompactorName() string { return a.ag.CompactorName() }

// buildSystem assembles the full system prompt: the base, the project rules,
// the project-memory digest (so a fresh session or a model switch keeps the
// accumulated context), and the mode addendum.
func (a *App) buildSystem() string {
	s := systemPrompt + a.rules
	if a.mem != nil {
		if d := memory.Digest(a.mem, 15); d != "" {
			s += "\n\n" + d
		}
	}
	return s + modeAddendum(a.mode)
}

// agentOptions is the shared set of agent.Option that both rebuild and
// FreshConversation apply: system prompt, warn sink, hooks, checkpoint observer
// and streaming. History and usage seeding are added by rebuild only.
func (a *App) agentOptions() []agent.Option {
	opts := []agent.Option{
		agent.WithSystem(a.buildSystem()),
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
	return opts
}

// rebuild swaps in a fresh agent + persister for the given session/history.
func (a *App) rebuild(sess *session.Session, history []provider.Message) {
	opts := a.agentOptions()
	opts = append(opts, agent.WithHistory(history))
	if a.autoCompactor != nil {
		opts = append(opts, agent.WithCompactor(a.autoCompactor), agent.WithCompactThreshold(a.compactAt))
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
func (a *App) FreshConversation() command.Conversation {
	return agent.New(a.prov, a.tools, a.effectiveApprover(), a.agentOptions()...)
}

// Run implements command.Conversation. It snapshots a restore point, delegates
// to the live conversation, and keeps the session's cumulative token usage in
// sync with the agent.
func (a *App) Run(ctx context.Context, in string) (string, error) {
	if a.checkpoints != nil {
		a.checkpoints.Begin(a.ag.History(), in)
	}
	out, err := a.conv.Run(ctx, in)
	a.usedIn, a.usedOut = a.ag.Usage()
	return out, err
}
