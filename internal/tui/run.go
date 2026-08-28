package tui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/command"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// Options bundles everything Run needs.
type Options struct {
	Conv      command.Conversation
	Provider  provider.Provider
	SessionID func() string
	Stats     func() int
	Cost      func() string
	Approvals chan approval.Request
	Deltas    chan string
	Notices   chan string // out-of-band lines (e.g. "update available"); nil to disable
	Theme     Theme
	Greeting  string
}

// Run starts the full-screen program and blocks until the user quits.
func Run(o Options) error {
	m := New(Config{
		Conv:      o.Conv,
		Provider:  o.Provider,
		SessionID: o.SessionID,
		Stats:     o.Stats,
		Cost:      o.Cost,
		Approvals: o.Approvals,
		Deltas:    o.Deltas,
		Notices:   o.Notices,
		Theme:     o.Theme,
		Greeting:  o.Greeting,
	})
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if mouseEnabled() {
		// Capturing the mouse enables wheel scroll but disables the terminal's
		// own text selection, so it is opt-in.
		opts = append(opts, tea.WithMouseCellMotion())
	}
	_, err := tea.NewProgram(m, opts...).Run()
	return err
}

func mouseEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ARNES_MOUSE"))) {
	case "1", "on", "true", "yes":
		return true
	default:
		return false
	}
}
