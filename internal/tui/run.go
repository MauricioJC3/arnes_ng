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
	Notices    chan string      // out-of-band lines (e.g. "update available"); nil to disable
	Todos      chan []todo.Item // live task checklist; nil to disable the panel
	Theme      Theme
	Greeting   string
	ListModels ListModelsFunc // fetches a provider's model list for /connect; nil = offline list
}

// Run starts the full-screen program and blocks until the user quits.
func Run(o Options) error {
	m := New(Config{
		Conv:       o.Conv,
		Provider:   o.Provider,
		ProviderFn: o.ProviderFn,
		SessionID:  o.SessionID,
		Stats:      o.Stats,
		Cost:       o.Cost,
		Approvals:  o.Approvals,
		Deltas:     o.Deltas,
		Notices:    o.Notices,
		Todos:      o.Todos,
		Theme:      o.Theme,
		Greeting:   o.Greeting,
		ListModels: o.ListModels,
	})
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if mouseEnabled() {
		// Capturing the mouse gives wheel scroll over the transcript. It also
		// takes over the terminal's own text selection -- in most terminals you
		// hold Shift to select while the mouse is captured. Set ARNES_MOUSE=off
		// to disable it.
		opts = append(opts, tea.WithMouseCellMotion())
	}
	_, err := tea.NewProgram(m, opts...).Run()
	return err
}

// mouseEnabled reports whether to capture the mouse (wheel scroll). On by
// default; ARNES_MOUSE=off|0|false|no turns it off.
func mouseEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ARNES_MOUSE"))) {
	case "off", "0", "false", "no":
		return false
	default:
		return true
	}
}
