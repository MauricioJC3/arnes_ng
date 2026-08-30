package tui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/command"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

// Options bundles everything Run needs.
type Options struct {
	Conv       command.Conversation
	Provider   provider.Provider        // static; used only when ProviderFn is nil
	ProviderFn func() provider.Provider // live provider getter (survives /connect)
	SessionID  func() string
	Stats      func() int
	Cost       func() string
	Approvals  chan approval.Request
	Deltas     chan string
	Activity   chan string      // live "what the agent is doing" lines (tool calls); nil to disable
	Notices    chan string      // out-of-band lines (e.g. "update available"); nil to disable
	Todos      chan []todo.Item // live task checklist; nil to disable the panel
	Theme      Theme
	Greeting   string
	ListModels ListModelsFunc // fetches a provider's model list for /connect; nil = offline list
}

// Run starts the full-screen program and blocks until the user quits.
func Run(o Options) error {
	m := New(Config{
		Conv:          o.Conv,
		Provider:      o.Provider,
		ProviderFn:    o.ProviderFn,
		SessionID:     o.SessionID,
		Stats:         o.Stats,
		Cost:          o.Cost,
		Approvals:     o.Approvals,
		Deltas:        o.Deltas,
		Activity:      o.Activity,
		Notices:       o.Notices,
		Todos:         o.Todos,
		Theme:         o.Theme,
		Greeting:      o.Greeting,
		ListModels:    o.ListModels,
		MouseOn:       mouseEnabled(),
		MarkdownStyle: markdownStyle(),
	})
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if mouseEnabled() {
		// Capturing the mouse gives wheel scroll, but it takes over the
		// terminal's own click-drag text selection. Off by default so copying
		// just works; toggle it at runtime with Ctrl+O, or start with
		// ARNES_MOUSE=on.
		opts = append(opts, tea.WithMouseCellMotion())
	}
	_, err := tea.NewProgram(m, opts...).Run()
	return err
}

// mouseEnabled reports whether to capture the mouse (wheel scroll) at startup.
// Off by default; ARNES_MOUSE=on|1|true|yes turns it on.
func mouseEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ARNES_MOUSE"))) {
	case "on", "1", "true", "yes":
		return true
	default:
		return false
	}
}

// markdownStyle is the glamour theme for rendered assistant text. It is never
// "auto": auto-detection queries the terminal over stdin and races the TUI for
// input, leaking the reply into the prompt. ARNES_MD_STYLE overrides; default
// "dark".
func markdownStyle() string {
	if s := strings.ToLower(strings.TrimSpace(os.Getenv("ARNES_MD_STYLE"))); s != "" && s != "auto" {
		return s
	}
	return "dark"
}
