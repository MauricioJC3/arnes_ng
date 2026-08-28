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
)

// Config is what Model needs from the rest of the harness.
type Config struct {
	Conv      command.Conversation
	Provider  provider.Provider
	SessionID func() string // read live: it changes with /new and /resume
	Stats     func() int    // estimated context tokens; nil to hide
	Cost      func() string // running session cost, e.g. "$0.0421"; nil/"" hides it
	Approvals chan approval.Request
	Deltas    chan string // streamed text chunks; nil when streaming is off
	Notices   chan string // out-of-band lines (e.g. "update available"); nil to disable
	Theme     Theme
	Greeting  string
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
	prov      provider.Provider
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

	entries  []entry
	live     string // streamed text for the current turn, not yet committed
	busy     bool
	pending  *approval.Request
	menu     commandMenu
	connect  *connectForm
	quitting bool

	history []string // submitted inputs, for ↑/↓ recall
	histAt  int      // index into history; len(history) == showing the draft
	draft   string   // the input the user was typing before recalling

	goalCh           chan goalStepMsg
	goalIter, goalMax int // >0 while a /goal loop is running

	approvals chan approval.Request
	deltas    chan string
	notices   chan string
	results   chan runResult
	cancel    context.CancelFunc // cancels the in-flight agent turn

	md      *glamour.TermRenderer
	mdWidth int
}

// New builds the model. It does not start the program (see Run).
func New(cfg Config) Model {
	styles := cfg.Theme.Styles()

	ta := textarea.New()
	ta.Placeholder = "escribí un mensaje…  (/help · Ctrl+C para salir)"
	ta.Prompt = styles.Accent.Render("❯ ")
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	ta.CharLimit = 0
	ta.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(styles.Accent))

	m := Model{
		conv:      cfg.Conv,
		prov:      cfg.Provider,
		sessionID: cfg.SessionID,
		stats:     cfg.Stats,
		cost:      cfg.Cost,
		theme:     cfg.Theme,
		styles:    styles,
		ta:        ta,
		sp:        sp,
		approvals: cfg.Approvals,
		deltas:    cfg.Deltas,
		notices:   cfg.Notices,
		results:   make(chan runResult, 1),
		goalCh:    make(chan goalStepMsg, 4),
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
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}

		if m.connect != nil {
			return m.driveConnectForm(msg), nil
		}

		if m.menu.open {
			switch msg.String() {
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

		// Esc while a turn is running cancels it (without quitting the app).
		if msg.String() == "esc" && m.busy && m.pending == nil {
			if m.cancel != nil {
				m.cancel()
			}
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
			// history recall while navigating, or from an empty input when there
			// is history; otherwise scroll (empty input) or move the cursor.
			if m.histAt < len(m.history) || (m.ta.Value() == "" && len(m.history) > 0) {
				m.histPrev() // a no-op at the oldest entry; still consumes the key
				return m, nil
			}
			if m.ta.Value() == "" {
				m.vp.ScrollUp(1)
				return m, nil
			}
		case "down":
			if m.histAt < len(m.history) {
				m.histNext()
				return m, nil
			}
			if m.ta.Value() == "" {
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

	if !m.busy && m.pending == nil && m.connect == nil {
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

// driveConnectForm feeds one key to the /connect picker and acts on the outcome.
func (m Model) driveConnectForm(msg tea.KeyMsg) Model {
	done, cancelled := m.connect.update(msg)
	switch {
	case cancelled:
		m.connect = nil
		m.add(kindInfo, "/connect cancelado")
	case done != nil:
		m.connect = nil
		conn, ok := m.conv.(command.Connector)
		if !ok {
			m.add(kindError, "este arnés no soporta /connect")
			return m
		}
		out, err := conn.Connect(done.provider, done.model, done.key)
		if err != nil {
			m.add(kindError, err.Error())
		} else {
			m.add(kindInfo, out)
		}
	}
	return m
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

	if strings.HasPrefix(text, "/") {
		m.add(kindInfo, "» "+redactConnect(text))
		res, err := command.Dispatch(text, m.conv, m.prov)
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
	m.add(kindInfo, "objetivo: "+req.Text+"  ·  Esc para cortar")
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
