package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MauricioJC3/arnes_ng/internal/session"
)

// sessionRow is one selectable line in the resume picker.
type sessionRow struct {
	id      string
	title   string
	msgs    int
	todo    string // progress label, e.g. "2/5 tareas" or "✓ tareas"; "" when none
	updated time.Time
	current bool
}

// sessionResult is the picked session.
type sessionResult struct{ id string }

// sessionForm is the picker opened by a bare /sessions (or /ls): the saved
// sessions newest-first, each with its task progress, so you can spot the one
// you left half-finished.
type sessionForm struct {
	rows []sessionRow
	idx  int
	note string
}

func newSessionForm(metas []session.Meta, currentID string) *sessionForm {
	f := &sessionForm{}
	for _, m := range metas {
		title := m.Title
		if title == "" {
			title = "(sin título)"
		}
		f.rows = append(f.rows, sessionRow{
			id:      m.ID,
			title:   title,
			msgs:    m.Messages,
			todo:    m.Todo.Label(),
			updated: m.UpdatedAt,
			current: m.ID == currentID,
		})
	}
	for i, r := range f.rows {
		if r.current {
			f.idx = i
			break
		}
	}
	return f
}

// update handles one key. At most one of (done, cancelled) is set.
func (f *sessionForm) update(msg tea.KeyMsg) (done *sessionResult, cancelled bool) {
	switch msg.String() {
	case "esc":
		return nil, true
	case "up", "ctrl+p":
		if len(f.rows) > 0 {
			f.idx = (f.idx - 1 + len(f.rows)) % len(f.rows)
		}
	case "down", "ctrl+n":
		if len(f.rows) > 0 {
			f.idx = (f.idx + 1) % len(f.rows)
		}
	case "enter":
		if len(f.rows) == 0 {
			return nil, true
		}
		return &sessionResult{id: f.rows[f.idx].id}, false
	}
	return nil, false
}

// view renders the picker. maxRows is how many session entries fit at once
// (each entry is two lines); the list scrolls inside that window so the
// selected row and its ❯ never overflow the screen.
func (f *sessionForm) view(s Styles, maxRows int) string {
	var b strings.Builder
	b.WriteString(s.Accent.Render("reanudar una sesión") + "\n")
	if f.note != "" {
		b.WriteString(s.Muted.Render("("+f.note+")") + "\n")
	}
	b.WriteString("\n")

	cur := func(on bool) string {
		if on {
			return s.Accent.Render("❯ ")
		}
		return "  "
	}
	top, end := listWindow(f.idx, len(f.rows), maxRows)
	if top > 0 {
		b.WriteString(s.Muted.Render(fmt.Sprintf("  ↑ %d más", top)) + "\n")
	}
	for i := top; i < end; i++ {
		r := f.rows[i]
		line := r.title + s.Muted.Render(fmt.Sprintf("  %d msg", r.msgs))
		if r.todo != "" {
			line += s.Muted.Render("  · " + r.todo)
		}
		if r.current {
			line += s.Muted.Render("  ● actual")
		}
		b.WriteString(cur(i == f.idx) + line + "\n")
		b.WriteString("    " + s.Muted.Render(r.id+"  "+humanAge(r.updated)) + "\n")
	}
	if end < len(f.rows) {
		b.WriteString(s.Muted.Render(fmt.Sprintf("  ↓ %d más", len(f.rows)-end)) + "\n")
	}
	b.WriteString("\n" + s.Muted.Render("↑↓ elegir · enter reanuda · esc cancela"))
	return b.String()
}

// humanAge is a compact "how long ago" for a session's last activity.
func humanAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "recién"
	case d < time.Hour:
		return fmt.Sprintf("hace %dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("hace %dh", int(d.Hours()))
	default:
		return fmt.Sprintf("hace %dd", int(d.Hours()/24))
	}
}
