// Package app is the application core: it owns the live conversation and every
// use case the front-ends drive through the command interfaces (switch provider,
// change mode, resume a session, compact, rewind, report cost, self-update).
//
// The composition root (cmd/arnes) builds the ports -- provider, session store,
// approval gateway, tool pool, memory, checkpoints -- and hands them to New as
// Deps. Nothing here reads the environment or the filesystem layout directly;
// that stays in cmd/arnes.
//
// # The rebuild invariant
//
// rebuild is the kernel. It is the ONLY place a.ag (the agent) and a.conv (the
// persister) are constructed. Every use case that changes something the agent
// was built from -- the provider (Connect), the permission mode (SetMode), the
// session or its history (NewSession, ResumeSession, Rewind), the compaction
// strategy is the exception: SetStrategy mutates the live agent in place -- MUST
// end by calling rebuild(sess, history). rebuild re-seeds the new agent with the
// running token totals so cost survives the swap.
//
// The live state guarded by that invariant is: sess, ag, conv, usedIn, usedOut.
package app

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"strings"

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
	"github.com/MauricioJC3/arnes_ng/internal/todo"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
)

// Deps is everything the app needs from the composition root. The caller builds
// these (reading the environment, the config file and the default paths) and
// passes them to New; the app never touches those itself.
type Deps struct {
	ProviderName    string
	Provider        provider.Provider
	Cfg             config.Config
	CfgPath         string
	Store           session.Store
	BaseApprover    approval.Approver
	Mode            string
	AutoCompactor   compact.Strategy
	CompactAt       int
	MaxSteps        int // tool round-trips per turn; <=0 uses agent.DefaultMaxSteps
	MaxTokens       int // output-token cap per model call; <=0 uses agent.DefaultMaxTokens
	MaxToolResult   int // byte cap on one tool result; <=0 uses agent.DefaultMaxToolResult
	ContextGuard    int // emergency token ceiling for mid-turn compaction; <=0 uses agent.DefaultContextGuard
	ProviderRetries int // extra attempts on a transient model-call failure; <0 uses agent.DefaultProviderRetries, 0 disables
	Streaming       bool
	Deltas          chan string
	Activity        chan string       // live "what the agent is doing" lines (tool calls); nil to disable
	Hooks           agent.Hooks       // pre/post tool-call hooks; nil when none configured
	Checkpoints     *checkpoint.Store // per-turn restore points for /rewind
	Mem             memory.Store      // project-scoped persistent memory (for the prompt digest)
	Rules           string            // project rules, already wrapped for the system prompt
	Subagents       *subagent.Registry
	Version         string      // running binary version, for /update-arnes
	Repo            string      // GitHub owner/name the self-updater pulls from
	Todos           *todo.Store // task checklist; persisted per session and restored on resume
	CheckCommand    string      // project verification for the completion gate; empty disables it
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
	maxSteps      int
	maxTokens     int
	maxToolResult int
	contextGuard  int
	streaming     bool
	deltas        chan string
	activity      chan string     // live tool-activity lines for the TUI; nil disables the feed
	turnCtx       context.Context // the in-flight turn's context; set by Run, read by emitDelta
	hooks         agent.Hooks
	checkpoints   *checkpoint.Store
	mem           memory.Store
	rules         string
	version       string
	repo          string

	todos           *todo.Store
	checkCommand    string
	providerRetries int
	firstTask       string // the session's original user request, kept for the completion-gate anchor
	sess            *session.Session
	ag              *agent.Agent
	conv            *session.Persisting
	usedIn, usedOut int // cumulative token usage; input is cache-weighted for cost (see provider.Usage.EffectiveInputTokens)
}

// New builds an App from its dependencies. The tool pool is set separately with
// SetTools, because the delegate tool needs a reference to the App itself.
func New(d Deps) *App {
	return &App{
		providerName:    d.ProviderName,
		prov:            d.Provider,
		cfg:             d.Cfg,
		cfgPath:         d.CfgPath,
		store:           d.Store,
		baseApprover:    d.BaseApprover,
		mode:            d.Mode,
		autoCompactor:   d.AutoCompactor,
		compactAt:       d.CompactAt,
		maxSteps:        cmp.Or(d.MaxSteps, agent.DefaultMaxSteps),
		maxTokens:       cmp.Or(d.MaxTokens, agent.DefaultMaxTokens),
		maxToolResult:   d.MaxToolResult, // 0 -> agentOptions skips it, agent.New applies its own default
		contextGuard:    d.ContextGuard,  // 0 -> agentOptions skips it, agent.New applies its own default
		streaming:       d.Streaming,
		deltas:          d.Deltas,
		activity:        d.Activity,
		hooks:           d.Hooks,
		checkpoints:     d.Checkpoints,
		mem:             d.Mem,
		rules:           d.Rules,
		subagents:       d.Subagents,
		version:         d.Version,
		repo:            d.Repo,
		todos:           d.Todos,
		checkCommand:    d.CheckCommand,
		providerRetries: providerRetriesOr(d.ProviderRetries),
	}
}

// providerRetriesOr maps the Deps sentinel (<0) to the agent default, and passes
// through 0 (disabled) or a positive override unchanged.
func providerRetriesOr(n int) int {
	if n < 0 {
		return agent.DefaultProviderRetries
	}
	return n
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
	if a.maxSteps > 0 {
		opts = append(opts, agent.WithMaxSteps(a.maxSteps))
	}
	if a.maxTokens > 0 {
		opts = append(opts, agent.WithMaxTokens(a.maxTokens))
	}
	if a.maxToolResult > 0 {
		opts = append(opts, agent.WithMaxToolResult(a.maxToolResult))
	}
	if a.contextGuard > 0 {
		opts = append(opts, agent.WithContextGuard(a.contextGuard))
	}
	if a.hooks != nil {
		opts = append(opts, agent.WithHooks(a.hooks))
	}
	if obs := a.toolObserver(); obs != nil {
		opts = append(opts, agent.WithToolObserver(obs))
	}
	if a.streaming {
		opts = append(opts, agent.WithStreaming(true))
		if a.deltas != nil {
			opts = append(opts, agent.WithDeltaFn(a.emitDelta))
		}
	}
	// Completion gate: verify before a turn ends, keep the task + plan anchored
	// in the prompt, and nudge on an abandoned checklist.
	if v := a.verifier(); v != nil {
		opts = append(opts, agent.WithVerifier(v))
	}
	opts = append(opts,
		agent.WithAnchorFn(a.anchorText),
		agent.WithOpenTodosFn(a.openTodos),
		agent.WithProviderRetries(a.providerRetries),
	)
	return opts
}

// emitDelta forwards one streamed text chunk to the TUI without ever wedging the
// agent turn. It is called synchronously on the turn goroutine (Run ->
// agent.Run -> StreamMessage -> onDelta), the same goroutine that sets turnCtx,
// so the field read below needs no synchronization.
//
// A plain `a.deltas <- s` is what froze long turns: once the TUI stalled and the
// buffer filled, the send parked forever, and Ctrl+C (which only cancels the
// context) could not free it. Selecting on turnCtx.Done() means a cancelled turn
// always unblocks. A chunk lost this way is cosmetic -- the final response text
// is authoritative and replaces the live preview.
func (a *App) emitDelta(s string) {
	if a.turnCtx == nil {
		select {
		case a.deltas <- s:
		default:
		}
		return
	}
	select {
	case a.deltas <- s:
	case <-a.turnCtx.Done():
	}
}

// rebuild swaps in a fresh agent + persister for the given session/history. It
// is the only constructor of a.ag and a.conv; see the package doc for the
// invariant every mutating use case upholds by calling it.
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

	popts := []session.PersistingOption{
		session.WithModelFn(func() string { return a.prov.Model() }),
	}
	if a.todos != nil {
		popts = append(popts, session.WithTodosFn(a.todos.Get))
	}
	a.conv = session.NewPersisting(a.ag, a.store, sess, popts...)
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
	a.turnCtx = ctx // so emitDelta can bail on Ctrl+C instead of parking on a full channel
	if a.firstTask == "" {
		// The session's original task anchors the prompt for the rest of the run.
		// On resume it is recovered from the restored history; on a fresh session
		// this input is it.
		if h := firstUserText(a.ag.History()); h != "" {
			a.firstTask = h
		} else {
			a.firstTask = strings.TrimSpace(in)
		}
	}
	if a.checkpoints != nil {
		a.checkpoints.Begin(a.ag.History(), in)
	}
	out, err := a.conv.Run(ctx, in)
	a.usedIn, a.usedOut = a.ag.Usage()
	return out, err
}
