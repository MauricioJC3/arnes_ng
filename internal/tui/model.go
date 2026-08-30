// Package tui is the full-screen Bubble Tea front-end. The agent runs in a
// goroutine; results, streamed deltas and approval requests come back as
// tea.Msg values.
package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/command"
	goalpkg "github.com/MauricioJC3/arnes_ng/internal/goal"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

// Config is what Model needs from the rest of the harness.
type Config struct {
	Conv command.Conversation
	// Provider is the static provider; used only when ProviderFn is nil.
	Provider provider.Provider
	// ProviderFn returns the live provider. It wins over Provider and must be
	// used by anything that can change after /connect (status bar, /model).
	ProviderFn    func() provider.Provider
	SessionID     func() string // read live: it changes with /new and /resume
	Stats         func() int    // estimated context tokens; nil to hide
	Cost          func() string // running session cost, e.g. "$0.0421"; nil/"" hides it
	Approvals     chan approval.Request
	Deltas        chan string      // streamed text chunks; nil when streaming is off
	Activity      chan string      // live "what the agent is doing" lines (tool calls); nil to disable
	Notices       chan string      // out-of-band lines (e.g. "update available"); nil to disable
	Todos         chan []todo.Item // live task checklist; nil to disable the panel
	MouseOn       bool             // whether the mouse was captured at startup (Ctrl+O toggles)
	MarkdownStyle string           // glamour style name ("dark" default); never "auto" (queries the terminal)
	Theme         Theme
	Greeting      string
	// ListModels fetches a provider's model list for the /connect picker; nil
	// falls back to the offline list.
	ListModels ListModelsFunc
}

// runResult carries the outcome of one agent turn (or goal run) back to Update.
type runResult struct {
	text string
	err  error
	goal *goalpkg.Report // set when the turn was a /goal loop
}

// goalStepMsg is emitted before each goal iteration: [current, max].
type goalStepMsg [2]int

// approvalMsg wraps an approval.Request as a tea.Msg.
type approvalMsg approval.Request

// deltaMsg is one streamed text chunk.
type deltaMsg string

// flushLiveMsg is the timer tick that renders streamed text coalesced since the
// last flush (see deltaFlushInterval), so a burst that goes quiet before the
// next chunk still shows up promptly.
type flushLiveMsg struct{}

// activityMsg is one "the agent just ran tool X" status line for the transcript.
type activityMsg string

// noticeMsg is an out-of-band line to drop into the transcript (e.g. an
// update-available note from the daily check).
type noticeMsg string

// todosMsg is the new state of the task checklist.
type todosMsg []todo.Item

// uiState is the single top-level mode of the UI. It is derived from the model
// fields by state(); Update and View both switch on it so their branch order
// can never drift apart.
type uiState int

const (
	stateInput       uiState = iota // typing at the prompt (the command menu may be open)
	stateBusy                       // an agent turn or goal loop is running
	stateApproval                   // a tool call is waiting for y/n
	stateConnectForm                // the /connect picker is open
	stateModelForm                  // the /model picker is open
)

// state reports the current UI mode. Order matters: the pickers sit on top of
// everything, then a pending approval, then a running turn.
func (m Model) state() uiState {
	switch {
	case m.connect != nil:
		return stateConnectForm
	case m.model != nil:
		return stateModelForm
	case m.approvalPrompt.active():
		return stateApproval
	case m.busy:
		return stateBusy
	default:
		return stateInput
	}
}

// Model is the Bubble Tea model.
type Model struct {
	conv      command.Conversation
	prov      func() provider.Provider // live: survives /connect
	sessionID func() string
	stats     func() int
	cost      func() string
	theme     Theme
	styles    Styles

	transcript     // scrollback: entries, in-flight text, viewport, markdown renderer
	promptInput    // the text prompt, its "/…" command menu, and the ↑/↓ recall history
	turn           // in-flight agent work: busy flag, cancel, result/delta channels
	approvalPrompt // the tool call awaiting a y/n decision, if any

	sp spinner.Model

	width, height int

	connect        *connectForm
	model          *modelForm
	listModels     ListModelsFunc
	quitting       bool
	quitHint       bool // first Esc arms the "Esc again to quit" prompt
	mouseOn        bool // mouse capture state; Ctrl+O toggles it (off = terminal text selection)
	flushScheduled bool // a flushLiveMsg tick is already in flight; don't stack more

	approvals chan approval.Request
	activity  chan string
	notices   chan string
	todos     chan []todo.Item
	todoItems []todo.Item // current checklist; rendered as a panel above the input

	queued []string // messages typed while a turn was running; sent one per turn as it frees up
}

// New builds the model. It does not start the program (see Run).
func New(cfg Config) Model {
	styles := cfg.Theme.Styles()

	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(styles.Accent))

	prov := cfg.ProviderFn
	if prov == nil {
		p := cfg.Provider
		prov = func() provider.Provider { return p }
	}

	m := Model{
		conv:        cfg.Conv,
		prov:        prov,
		sessionID:   cfg.SessionID,
		stats:       cfg.Stats,
		cost:        cfg.Cost,
		theme:       cfg.Theme,
		styles:      styles,
		transcript:  transcript{styles: styles, mdStyle: cfg.MarkdownStyle},
		promptInput: newPromptInput(styles),
		turn:        newTurn(cfg.Deltas),
		sp:          sp,
		approvals:   cfg.Approvals,
		activity:    cfg.Activity,
		notices:     cfg.Notices,
		todos:       cfg.Todos,
		mouseOn:     cfg.MouseOn,
		listModels:  cfg.ListModels,
	}
	if cfg.Greeting != "" {
		m.entries = append(m.entries, entry{kind: kindInfo, text: cfg.Greeting})
	}
	return m
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink, waitForApproval(m.approvals)}
	if m.deltas != nil {
		cmds = append(cmds, waitForDelta(m.deltas))
	}
	if m.activity != nil {
		cmds = append(cmds, waitForActivity(m.activity))
	}
	if m.notices != nil {
		cmds = append(cmds, waitForNotice(m.notices))
	}
	if m.todos != nil {
		cmds = append(cmds, waitForTodos(m.todos))
	}
	return tea.Batch(cmds...)
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()
		m.transcript.reflow() // re-render assistant markdown at the new width
		return m, nil

	case tea.MouseMsg:
		var c tea.Cmd
		m.vp, c = m.vp.Update(msg)
		return m, c

	case tea.KeyMsg:
		k := msg.String()

		// Any key other than a lone Esc disarms the "Esc again to quit" prompt.
		if k != "esc" {
			m.quitHint = false
		}

		// Ctrl+C never quits. It cancels whatever is in progress -- a running
		// turn, the /connect form, the command menu, a pending approval -- or
		// clears a half-typed message.
		if k == "ctrl+c" {
			return m.cancelWithCtrlC()
		}

		// Ctrl+O toggles mouse capture: on = wheel scroll, off = the terminal's
		// own text selection (so you can copy).
		if k == "ctrl+o" {
			m.mouseOn = !m.mouseOn
			if m.mouseOn {
				m.add(kindInfo, "mouse ON · rueda para scrollear (Ctrl+O para volver a seleccionar texto)")
				return m, tea.EnableMouseCellMotion
			}
			m.add(kindInfo, "mouse OFF · podés seleccionar y copiar texto (Ctrl+O para la rueda de scroll)")
			return m, tea.DisableMouse
		}

		switch m.state() {
		case stateConnectForm:
			return m.driveConnectForm(msg)
		case stateModelForm:
			return m.driveModelForm(msg)
		}

		if m.menu.open {
			switch k {
			case "up", "ctrl+p":
				m.menu.move(-1)
				return m, nil
			case "down", "ctrl+n":
				m.menu.move(1)
				return m, nil
			case "tab":
				if s, ok := m.menu.selected(); ok {
					m.ta.SetValue(s.Name + " ")
					m.ta.CursorEnd()
					m.menu.update(m.ta.Value())
				}
				return m, nil
			case "esc":
				m.menu = commandMenu{}
				return m, nil
			}
		}

		// Esc is the way out of the app, but only on a second press so it is
		// never an accident. It still just answers a pending approval, and it
		// does not quit mid-turn (Ctrl+C cancels the turn).
		if k == "esc" {
			if m.approvalPrompt.active() {
				return m.answerApproval(msg), nil
			}
			if m.busy {
				return m, nil
			}
			if m.quitHint {
				m.quitting = true
				return m, tea.Quit
			}
			m.quitHint = true
			return m, nil
		}

		switch msg.String() {
		case "shift+tab":
			return m.cycleMode(), nil
		case "pgup":
			m.vp.PageUp()
			return m, nil
		case "pgdown":
			m.vp.PageDown()
			return m, nil
		case "ctrl+u":
			m.vp.HalfViewUp()
			return m, nil
		case "ctrl+d":
			m.vp.HalfViewDown()
			return m, nil
		case "home":
			m.vp.GotoTop()
			return m, nil
		case "end":
			m.vp.GotoBottom()
			return m, nil
		case "up":
			switch m.promptInput.onUp(m.vp.AtBottom()) {
			case navConsumed:
				return m, nil
			case navScrollUp:
				m.vp.ScrollUp(1)
				return m, nil
			case navTextarea:
				// fall through to the textarea below
			}
		case "down":
			switch m.promptInput.onDown(m.vp.AtBottom()) {
			case navConsumed:
				return m, nil
			case navScrollDown:
				m.vp.ScrollDown(1)
				return m, nil
			case navTextarea:
				// fall through to the textarea below
			}
		}
		if m.approvalPrompt.active() {
			return m.answerApproval(msg), nil
		}
		if msg.Type == tea.KeyEnter && m.busy {
			return m.enqueueInput()
		}
		if msg.Type == tea.KeyEnter && !m.busy {
			if s, ok := m.menu.selected(); ok && m.ta.Value() != s.Name {
				m.ta.SetValue(s.Name)
				if s.Args == "" {
					return m.submit()
				}
				m.ta.SetValue(s.Name + " ")
				m.ta.CursorEnd()
				m.menu.update(m.ta.Value())
				return m, nil
			}
			return m.submit()
		}

	case spinner.TickMsg:
		if m.busy {
			var c tea.Cmd
			m.sp, c = m.sp.Update(msg)
			return m, c
		}
		return m, nil

	case deltaMsg:
		// Only the current turn's deltas matter. Anything that arrives after the
		// turn already finished (the delta/result channels race) is drained and
		// dropped, so it can't leak into the next message.
		if !m.busy {
			return m, m.turn.awaitDelta()
		}
		m.transcript.appendDelta(string(msg))
		cmds := []tea.Cmd{m.turn.awaitDelta()}
		if m.transcript.liveDirty && !m.flushScheduled {
			// This chunk was coalesced; make sure it still renders soon even if
			// the stream goes quiet before the next one.
			m.flushScheduled = true
			cmds = append(cmds, tea.Tick(deltaFlushInterval, func(time.Time) tea.Msg { return flushLiveMsg{} }))
		}
		return m, tea.Batch(cmds...)

	case flushLiveMsg:
		m.flushScheduled = false
		if m.busy {
			m.transcript.flushLive()
		}
		return m, nil

	case activityMsg:
		// Commit whatever the model streamed before this tool call so the
		// status line lands in chronological order (same move as approvalMsg).
		if m.busy {
			m.commitLive()
			m.add(kindActivity, string(msg))
		}
		return m, waitForActivity(m.activity)

	case noticeMsg:
		m.add(kindInfo, string(msg))
		return m, waitForNotice(m.notices)

	case todosMsg:
		m.todoItems = []todo.Item(msg)
		m.relayout() // the panel's height may have changed; resize the viewport
		return m, waitForTodos(m.todos)

	case connectModelsMsg:
		if m.connect != nil {
			m.connect.setModels(msg.models, msg.err)
		}
		return m, nil

	case modelGroupsMsg:
		if m.model != nil {
			m.model.setGroups(msg.groups)
		}
		return m, nil

	case goalStepMsg:
		// A new goal iteration is about to run: commit the previous one's text.
		m.commitLive()
		m.setContent(true)
		return m, m.turn.stepGoal(msg)

	case approvalMsg:
		// Commit the pre-tool text the model streamed so far as its own entry.
		m.commitLive()
		m.approvalPrompt.open(approval.Request(msg))
		return m, waitForApproval(m.approvals) // re-arm for the next request

	case runResult:
		m.turn.end()
		m.flushScheduled = false
		switch {
		case msg.goal != nil:
			m.commitLive() // the last iteration's text
			if msg.err != nil {
				m.add(kindError, msg.err.Error())
			}
			m.add(kindInfo, msg.goal.Summary())
		case errors.Is(msg.err, context.Canceled):
			// Keep whatever was streamed so far, then note the interruption.
			m.commitLive()
			m.add(kindInfo, "⨯ turno interrumpido")
		case isIncomplete(msg.err):
			// Stopped on a safety limit, not a failure: keep the partial answer
			// and show the reason as a notice, not a red error.
			m.transcript.dropLive()
			if strings.TrimSpace(msg.text) != "" {
				m.add(kindAssistant, msg.text)
			}
			m.add(kindInfo, "⨯ "+msg.err.Error())
		case msg.err != nil:
			m.transcript.dropLive()
			m.add(kindError, msg.err.Error())
		default:
			// The result text is authoritative; drop the partial live buffer.
			m.transcript.dropLive()
			if strings.TrimSpace(msg.text) != "" {
				m.add(kindAssistant, msg.text)
			}
		}
		m.setContent(true)
		m.ta.Focus()
		// A message typed while this turn ran? Send it now as its own turn.
		// Cancelled turns clear the queue in cancelWithCtrlC, so this only
		// fires after a turn that finished on its own.
		if len(m.queued) > 0 {
			next := m.queued[0]
			m.queued = m.queued[1:]
			m.add(kindUser, next)
			m.transcript.dropLive()
			return m, tea.Batch(m.sp.Tick, m.turn.startAgent(m.conv, next))
		}
		return m, textarea.Blink
	}

	// The prompt stays live during a running turn too, so the user can type the
	// next message (Enter queues it -- see enqueueInput). The pickers and a
	// pending approval still own the keyboard.
	if s := m.state(); s == stateInput || s == stateBusy {
		var c tea.Cmd
		m.ta, c = m.ta.Update(msg)
		cmds = append(cmds, c)
		m.menu.update(m.ta.Value())
		if _, isKey := msg.(tea.KeyMsg); isKey {
			// editing the input detaches it from history navigation
			m.promptInput.detachHistory()
		}
	}
	return m, tea.Batch(cmds...)
}

// driveConnectForm feeds one key to the /connect picker and acts on the outcome:
// a cancel, a request to fetch the model list, or a finished result.
func (m Model) driveConnectForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	done, cancelled, fetch := m.connect.update(msg)
	switch {
	case cancelled:
		m.connect = nil
		m.add(kindInfo, "/connect cancelado")
	case fetch:
		if m.listModels == nil {
			m.connect.setModels(nil, nil) // offline list only
			return m, nil
		}
		return m, fetchConnectModels(m.listModels, m.connect.provider, m.connect.key)
	case done != nil:
		m.connect = nil
		conn, ok := m.conv.(command.Connector)
		if !ok {
			m.add(kindError, "este arnés no soporta /connect")
			return m, nil
		}
		out, err := conn.Connect(done.provider, done.model, done.key)
		if err != nil {
			m.add(kindError, err.Error())
		} else {
			m.add(kindInfo, out)
		}
	}
	return m, nil
}

// driveModelForm feeds one key to the /model picker and applies the pick: a
// same-provider model change, or a switch to a model on another keyed provider.
func (m Model) driveModelForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	done, cancelled := m.model.update(msg)
	switch {
	case cancelled:
		m.model = nil
		m.add(kindInfo, "/model cancelado")
	case done != nil:
		active := m.model.active
		m.model = nil
		var out string
		var err error
		if done.provider == "" || done.provider == active {
			md, ok := m.conv.(command.Modeler)
			if !ok {
				m.add(kindError, "este arnés no soporta /model")
				return m, nil
			}
			out, err = md.SetModel(done.model)
		} else {
			conn, ok := m.conv.(command.Connector)
			if !ok {
				m.add(kindError, "este arnés no soporta cambiar de proveedor")
				return m, nil
			}
			out, err = conn.Connect(done.provider, done.model, "")
		}
		if err != nil {
			m.add(kindError, err.Error())
		} else {
			m.add(kindInfo, out)
		}
	}
	return m, nil
}

// submit handles Enter: a slash command runs synchronously; plain text starts an
// agent turn in a goroutine.
func (m Model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.ta.Value())
	if text == "" {
		return m, nil
	}
	m.remember(text)
	m.ta.Reset()
	m.menu = commandMenu{}

	if text == "/connect" {
		m.connect = newConnectForm()
		return m, nil
	}

	// A bare /model opens the picker when the harness can enumerate models;
	// otherwise it falls through to Dispatch (which just prints the current one).
	if text == "/model" {
		if md, ok := m.conv.(command.Modeler); ok && m.listModels != nil {
			m.model = newModelForm(md.ActiveProvider(), md.Model())
			return m, fetchModelGroups(m.listModels, md.KeyedProviders())
		}
	}

	if strings.HasPrefix(text, "/") {
		m.add(kindInfo, "» "+redactConnect(text))
		res, err := command.Dispatch(text, m.conv, m.prov())
		switch {
		case err != nil:
			m.add(kindError, err.Error())
		case res.Exit:
			m.quitting = true
			return m, tea.Quit
		case res.Goal != nil:
			return m.startGoal(res.Goal)
		case res.Output != "":
			m.add(kindInfo, res.Output)
		}
		return m, nil
	}

	m.add(kindUser, text)
	m.transcript.dropLive()
	// The prompt stays focused during the turn so the user can type ahead;
	// Enter on that text queues it (enqueueInput).
	return m, tea.Batch(m.sp.Tick, m.turn.startAgent(m.conv, text))
}

// enqueueInput handles Enter while a turn is running: it parks the typed text on
// the queue instead of blocking, and it is sent as its own turn once the
// current one finishes (see the runResult handler). Slash commands are refused
// here -- running one mid-turn would mutate the live agent from under it.
func (m Model) enqueueInput() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.ta.Value())
	if text == "" {
		return m, nil
	}
	if strings.HasPrefix(text, "/") {
		m.add(kindInfo, "los comandos no se encolan: esperá a que termine el turno")
		return m, nil
	}
	m.queued = append(m.queued, text)
	m.remember(text)
	m.ta.Reset()
	m.menu = commandMenu{}
	m.promptInput.detachHistory()
	m.add(kindInfo, "⏳ en cola ("+strconv.Itoa(len(m.queued))+"): "+text)
	return m, nil
}

// startGoal kicks off a Ralph-style goal loop as a cancellable background turn.
func (m Model) startGoal(req *command.GoalRequest) (tea.Model, tea.Cmd) {
	m.add(kindInfo, "objetivo: "+req.Text+"  ·  Ctrl+C para cortar")
	m.transcript.dropLive()
	return m, tea.Batch(m.sp.Tick, m.turn.startGoal(m.conv, req))
}

// cancelWithCtrlC handles Ctrl+C: cancel the in-flight work, or clear a
// half-typed message. It never quits the app -- pressing Esc twice does that.
func (m Model) cancelWithCtrlC() (tea.Model, tea.Cmd) {
	switch {
	case m.connect != nil:
		m.connect = nil
		m.add(kindInfo, "/connect cancelado")
	case m.model != nil:
		m.model = nil
		m.add(kindInfo, "/model cancelado")
	case m.approvalPrompt.active():
		return m.answerApproval(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}), nil
	case m.menu.open:
		m.menu = commandMenu{}
	case m.busy:
		m.turn.interrupt()
		if len(m.queued) > 0 {
			m.queued = nil
			m.add(kindInfo, "cola de mensajes descartada")
		}
	case m.ta.Value() != "":
		m.ta.SetValue("")
		m.menu.update("")
		m.promptInput.detachHistory()
	}
	return m, nil
}

// answerApproval consumes a y/n keypress while a tool call is pending and logs
// the outcome. A key that is not a decision leaves the request pending.
func (m Model) answerApproval(msg tea.KeyMsg) Model {
	if line := m.approvalPrompt.answer(msg.String()); line != "" {
		m.add(kindInfo, line)
	}
	return m
}

// cycleMode rotates normal -> auto -> plan -> normal when the conversation
// supports modes (shift+tab).
func (m Model) cycleMode() Model {
	md, ok := m.conv.(command.Modes)
	if !ok {
		return m
	}
	next := map[string]string{"normal": "auto", "auto": "plan", "plan": "normal"}[md.Mode()]
	if next == "" {
		next = "normal"
	}
	if out, err := md.SetMode(next); err == nil {
		m.add(kindInfo, out)
	}
	return m
}

// waitFor* are tea.Cmds that block on a channel. The turn-owned ones
// (waitForResult, waitForDelta, waitForGoal) live in turn.go.

func waitForApproval(ch chan approval.Request) tea.Cmd {
	return func() tea.Msg { return approvalMsg(<-ch) }
}

func waitForActivity(ch chan string) tea.Cmd {
	return func() tea.Msg { return activityMsg(<-ch) }
}

func waitForNotice(ch chan string) tea.Cmd {
	return func() tea.Msg { return noticeMsg(<-ch) }
}

func waitForTodos(ch chan []todo.Item) tea.Cmd {
	return func() tea.Msg { return todosMsg(<-ch) }
}

// isIncomplete reports whether err is the agent loop stopping on a safety limit
// (step budget, repeated call, repeated truncation) rather than a real failure.
func isIncomplete(err error) bool {
	var inc *provider.IncompleteError
	return errors.As(err, &inc)
}

// redactConnect masks the api-key argument of /connect so it never lands in the
// scrollback.
func redactConnect(line string) string {
	f := strings.Fields(line)
	if len(f) >= 4 && f[0] == "/connect" {
		f[3] = "••••••"
		return strings.Join(f[:4], " ")
	}
	return line
}
