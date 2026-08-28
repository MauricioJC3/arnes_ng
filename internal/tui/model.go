// Package tui is the full-screen Bubble Tea front-end. The agent runs in a
// goroutine; results, streamed deltas and approval requests come back as
// tea.Msg values.
package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"

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
	ProviderFn func() provider.Provider
	SessionID  func() string // read live: it changes with /new and /resume
	Stats      func() int    // estimated context tokens; nil to hide
	Cost       func() string // running session cost, e.g. "$0.0421"; nil/"" hides it
	Approvals  chan approval.Request
	Deltas     chan string      // streamed text chunks; nil when streaming is off
	Notices    chan string      // out-of-band lines (e.g. "update available"); nil to disable
	Todos      chan []todo.Item // live task checklist; nil to disable the panel
	MouseOn    bool             // whether the mouse was captured at startup (Ctrl+O toggles)
	Theme      Theme
	Greeting   string
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

// noticeMsg is an out-of-band line to drop into the transcript (e.g. an
// update-available note from the daily check).
type noticeMsg string

// todosMsg is the new state of the task checklist.
type todosMsg []todo.Item

// entryKind classifies a transcript line for styling.
type entryKind int

const (
	kindUser entryKind = iota
	kindAssistant
	kindInfo
	kindError
)

type entry struct {
	kind     entryKind
	text     string
	rendered string // cached display form (e.g. glamour markdown for assistant)
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

	vp viewport.Model
	ta textarea.Model
	sp spinner.Model

	width, height int
	ready         bool

	entries    []entry
	live       string // streamed text for the current turn, not yet committed
	busy       bool
	pending    *approval.Request
	menu       commandMenu
	connect    *connectForm
	model      *modelForm
	listModels ListModelsFunc
	quitting   bool
	quitHint   bool // first Esc arms the "Esc again to quit" prompt
	mouseOn    bool // mouse capture state; Ctrl+O toggles it (off = terminal text selection)

	history []string // submitted inputs, for ↑/↓ recall
	histAt  int      // index into history; len(history) == showing the draft
	draft   string   // the input the user was typing before recalling

	goalCh            chan goalStepMsg
	goalIter, goalMax int // >0 while a /goal loop is running

	approvals chan approval.Request
	deltas    chan string
	notices   chan string
	todos     chan []todo.Item
	todoItems []todo.Item // current checklist; rendered as a panel above the input
	results   chan runResult
	cancel    context.CancelFunc // cancels the in-flight agent turn

	md      *glamour.TermRenderer
	mdWidth int
}

// New builds the model. It does not start the program (see Run).
func New(cfg Config) Model {
	styles := cfg.Theme.Styles()

	ta := textarea.New()
	ta.Placeholder = "escribí un mensaje…  (/help · Esc Esc para salir)"
	ta.Prompt = styles.Accent.Render("❯ ")
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	ta.CharLimit = 0
	ta.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(styles.Accent))

	prov := cfg.ProviderFn
	if prov == nil {
		p := cfg.Provider
		prov = func() provider.Provider { return p }
	}

	m := Model{
		conv:       cfg.Conv,
		prov:       prov,
		sessionID:  cfg.SessionID,
		stats:      cfg.Stats,
		cost:       cfg.Cost,
		theme:      cfg.Theme,
		styles:     styles,
		ta:         ta,
		sp:         sp,
		approvals:  cfg.Approvals,
		deltas:     cfg.Deltas,
		notices:    cfg.Notices,
		todos:      cfg.Todos,
		mouseOn:    cfg.MouseOn,
		listModels: cfg.ListModels,
		results:    make(chan runResult, 1),
		goalCh:     make(chan goalStepMsg, 4),
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
		m.md = nil // force a renderer at the new width
		for i := range m.entries {
			if m.entries[i].kind == kindAssistant {
				m.entries[i].rendered = m.markdown(m.entries[i].text)
			}
		}
		m.setContent(m.vp.AtBottom())
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

		if m.connect != nil {
			return m.driveConnectForm(msg)
		}

		if m.model != nil {
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
			if m.pending != nil {
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
			// Priority: continue an in-progress history recall; then, with a
			// non-empty input, let the textarea move the cursor; then, if the
			// transcript is scrolled up, keep scrolling it; otherwise (at the
			// prompt) start history recall, or scroll when there is no history.
			switch {
			case m.histAt < len(m.history):
				m.histPrev()
				return m, nil
			case m.ta.Value() != "":
				// fall through to the textarea
			case !m.vp.AtBottom():
				m.vp.ScrollUp(1)
				return m, nil
			case len(m.history) > 0:
				m.histPrev()
				return m, nil
			default:
				m.vp.ScrollUp(1)
				return m, nil
			}
		case "down":
			switch {
			case m.histAt < len(m.history):
				m.histNext()
				return m, nil
			case m.ta.Value() != "":
				// fall through to the textarea
			case !m.vp.AtBottom():
				m.vp.ScrollDown(1)
				return m, nil
			}
		}
		if m.pending != nil {
			return m.answerApproval(msg), nil
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
		if m.busy {
			atBottom := !m.ready || m.vp.AtBottom()
			m.live += string(msg)
			m.setContent(atBottom)
		}
		return m, waitForDelta(m.deltas)

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
		m.goalIter, m.goalMax = msg[0], msg[1]
		m.setContent(true)
		return m, waitForGoal(m.goalCh)

	case approvalMsg:
		// Commit the pre-tool text the model streamed so far as its own entry.
		m.commitLive()
		req := approval.Request(msg)
		m.pending = &req
		return m, waitForApproval(m.approvals) // re-arm for the next request

	case runResult:
		m.busy = false
		m.cancel = nil
		m.goalIter, m.goalMax = 0, 0
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
		case msg.err != nil:
			m.live = ""
			m.add(kindError, msg.err.Error())
		default:
			// The result text is authoritative; drop the partial live buffer.
			m.live = ""
			if strings.TrimSpace(msg.text) != "" {
				m.add(kindAssistant, msg.text)
			}
		}
		m.setContent(true)
		m.ta.Focus()
		return m, textarea.Blink
	}

	if !m.busy && m.pending == nil && m.connect == nil && m.model == nil {
		var c tea.Cmd
		m.ta, c = m.ta.Update(msg)
		cmds = append(cmds, c)
		m.menu.update(m.ta.Value())
		if _, isKey := msg.(tea.KeyMsg); isKey {
			// editing the input detaches it from history navigation
			m.histAt = len(m.history)
		}
	}
	return m, tea.Batch(cmds...)
}

// histPrev recalls an older input into the textarea. Returns false when there is
// nothing older.
func (m *Model) histPrev() bool {
	if len(m.history) == 0 || m.histAt == 0 {
		return false
	}
	if m.histAt == len(m.history) {
		m.draft = m.ta.Value()
	}
	m.histAt--
	m.ta.SetValue(m.history[m.histAt])
	m.ta.CursorEnd()
	return true
}

// histNext moves toward the newest input, restoring the saved draft past the end.
func (m *Model) histNext() bool {
	if m.histAt >= len(m.history) {
		return false
	}
	m.histAt++
	if m.histAt == len(m.history) {
		m.ta.SetValue(m.draft)
	} else {
		m.ta.SetValue(m.history[m.histAt])
	}
	m.ta.CursorEnd()
	return true
}

// remember appends a submitted input to the recall history (skips duplicates of
// the immediately previous entry).
func (m *Model) remember(text string) {
	if n := len(m.history); n == 0 || m.history[n-1] != text {
		m.history = append(m.history, text)
	}
	m.histAt = len(m.history)
	m.draft = ""
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
	m.busy = true
	m.live = ""
	m.ta.Blur()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go func() {
		out, err := m.conv.Run(ctx, text)
		m.results <- runResult{text: out, err: err}
	}()
	return m, tea.Batch(m.sp.Tick, waitForResult(m.results))
}

// startGoal kicks off a Ralph-style goal loop as a cancellable background turn.
func (m Model) startGoal(req *command.GoalRequest) (tea.Model, tea.Cmd) {
	m.add(kindInfo, "objetivo: "+req.Text+"  ·  Ctrl+C para cortar")
	m.busy = true
	m.live = ""
	m.ta.Blur()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	conv := m.conv
	ch := m.goalCh

	cfg := goalpkg.Config{
		MaxIterations: req.MaxIter,
		Progress:      func(n, max int) { ch <- goalStepMsg{n, max} },
	}
	if req.Fresh {
		if ff, ok := m.conv.(command.FreshFactory); ok {
			cfg.NewConversation = func() goalpkg.Conversation { return ff.FreshConversation() }
		}
	}

	go func() {
		rep, err := goalpkg.Run(ctx, conv, req.Text, cfg)
		m.results <- runResult{goal: &rep, err: err}
	}()
	return m, tea.Batch(m.sp.Tick, waitForResult(m.results), waitForGoal(ch))
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
	case m.pending != nil:
		return m.answerApproval(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}), nil
	case m.menu.open:
		m.menu = commandMenu{}
	case m.busy:
		if m.cancel != nil {
			m.cancel()
		}
	case m.ta.Value() != "":
		m.ta.SetValue("")
		m.menu.update("")
		m.histAt = len(m.history)
	}
	return m, nil
}

// answerApproval consumes a y/n keypress while a tool call is pending.
func (m Model) answerApproval(msg tea.KeyMsg) Model {
	name := m.pending.Call.Name
	switch strings.ToLower(msg.String()) {
	case "y", "s", "enter":
		m.pending.Reply(true)
		m.add(kindInfo, "✓ "+name+" permitido")
		m.pending = nil
	case "n", "esc":
		m.pending.Reply(false)
		m.add(kindInfo, "✗ "+name+" denegado")
		m.pending = nil
	}
	return m
}

// add appends an entry. The scroll position is kept unless the viewport was
// already at the bottom (so reading history isn't interrupted); a user message
// always jumps to the bottom.
func (m *Model) add(k entryKind, text string) {
	e := entry{kind: k, text: text}
	if k == kindAssistant {
		e.rendered = m.markdown(text)
	}
	atBottom := !m.ready || m.vp.AtBottom() || k == kindUser
	m.entries = append(m.entries, e)
	m.setContent(atBottom)
}

// commitLive moves the streamed text of this turn into a permanent entry.
func (m *Model) commitLive() {
	if m.live == "" {
		return
	}
	m.add(kindAssistant, m.live)
	m.live = ""
}

// markdown renders s through glamour, caching the renderer per width. On any
// error it returns the raw text.
func (m *Model) markdown(s string) string {
	w := m.vp.Width - 2
	if w < 20 {
		w = 20
	}
	if m.md == nil || m.mdWidth != w {
		r, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(w))
		if err != nil {
			return s
		}
		m.md, m.mdWidth = r, w
	}
	out, err := m.md.Render(s)
	if err != nil {
		return s
	}
	return strings.Trim(out, "\n")
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

// setContent re-renders the viewport, scrolling to the bottom only when asked
// (so a user reading history isn't yanked down by streaming).
func (m *Model) setContent(gotoBottom bool) {
	if !m.ready {
		return
	}
	m.vp.SetContent(m.renderTranscript())
	if gotoBottom {
		m.vp.GotoBottom()
	}
}

// waitFor* are tea.Cmds that block on a channel.

func waitForApproval(ch chan approval.Request) tea.Cmd {
	return func() tea.Msg { return approvalMsg(<-ch) }
}

func waitForResult(ch chan runResult) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func waitForDelta(ch chan string) tea.Cmd {
	return func() tea.Msg { return deltaMsg(<-ch) }
}

func waitForNotice(ch chan string) tea.Cmd {
	return func() tea.Msg { return noticeMsg(<-ch) }
}

func waitForTodos(ch chan []todo.Item) tea.Cmd {
	return func() tea.Msg { return todosMsg(<-ch) }
}

func waitForGoal(ch chan goalStepMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
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
