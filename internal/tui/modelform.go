package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// modelProviderGroup is one provider's model list, or the error fetching it.
type modelProviderGroup struct {
	provider string
	models   []string
	err      error
}

// modelGroupsMsg carries the result of the multi-provider /models lookup.
type modelGroupsMsg struct {
	groups []modelProviderGroup
}

// fetchModelGroups looks up the model list of every given provider (with the
// already-saved key, so apiKey ""), sequentially, off the UI goroutine.
func fetchModelGroups(fn ListModelsFunc, providers []string) tea.Cmd {
	return func() tea.Msg {
		groups := make([]modelProviderGroup, 0, len(providers))
		for _, p := range providers {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			models, err := fn(ctx, p, "")
			cancel()
			groups = append(groups, modelProviderGroup{provider: p, models: models, err: err})
		}
		return modelGroupsMsg{groups: groups}
	}
}

// modelRow is one selectable line in the picker.
type modelRow struct {
	provider string
	model    string
	current  bool
	manual   bool // the "escribir a mano" row; uses the active provider
}

// modelResult is the picked model. provider may differ from the active one, in
// which case the caller switches providers.
type modelResult struct {
	provider, model string
}

// modelForm is the picker opened by a bare /model. It lists the models of every
// provider the user has a key for, grouped, with the current one marked.
type modelForm struct {
	active  string
	current string

	rows    []modelRow
	idx     int
	loading bool
	note    string
	manual  bool
	input   textinput.Model
}

func newModelForm(active, current string) *modelForm {
	ti := textinput.New()
	ti.Prompt = "  "
	return &modelForm{active: active, current: current, loading: true, input: ti}
}

// setGroups turns the lookup result into selectable rows. A provider whose
// lookup failed (or returned nothing) falls back to its offline list.
func (f *modelForm) setGroups(groups []modelProviderGroup) {
	f.loading = false
	f.rows = nil
	failed := false
	for _, g := range groups {
		models := g.models
		if g.err != nil || len(models) == 0 {
			models = connectModels[g.provider]
			if g.err != nil {
				failed = true
			}
		}
		for _, m := range models {
			f.rows = append(f.rows, modelRow{
				provider: g.provider,
				model:    m,
				current:  g.provider == f.active && m == f.current,
			})
		}
	}
	f.rows = append(f.rows, modelRow{provider: f.active, manual: true})
	if failed {
		f.note = "alguna lista no se pudo consultar; modelos locales"
	}
	for i, r := range f.rows {
		if r.current {
			f.idx = i
			break
		}
	}
}

// update handles one key. At most one of (done, cancelled) is set.
func (f *modelForm) update(msg tea.KeyMsg) (done *modelResult, cancelled bool) {
	key := msg.String()
	if key == "esc" {
		return nil, true
	}
	if f.loading {
		return nil, false
	}

	if f.manual {
		if key == "enter" {
			if m := strings.TrimSpace(f.input.Value()); m != "" {
				return &modelResult{provider: f.active, model: m}, false
			}
			return nil, false
		}
		f.input, _ = f.input.Update(msg)
		return nil, false
	}

	switch key {
	case "up", "ctrl+p":
		f.idx = (f.idx - 1 + len(f.rows)) % len(f.rows)
	case "down", "ctrl+n":
		f.idx = (f.idx + 1) % len(f.rows)
	case "enter":
		r := f.rows[f.idx]
		if r.manual {
			f.manual = true
			f.input.EchoMode = textinput.EchoNormal
			f.input.SetValue("")
			f.input.Focus()
			return nil, false
		}
		return &modelResult{provider: r.provider, model: r.model}, false
	}
	return nil, false
}

// visibleWindow returns the [top, end) slice of rows to render so the selection
// (f.idx) always stays on screen, scrolling when the list is taller than
// maxRows. With maxRows >= len(rows) the whole list shows.
func (f *modelForm) visibleWindow(maxRows int) (top, end int) {
	n := len(f.rows)
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows >= n {
		return 0, n
	}
	top = f.idx - maxRows/2
	if top < 0 {
		top = 0
	}
	if top > n-maxRows {
		top = n - maxRows
	}
	return top, top + maxRows
}

func (f *modelForm) view(s Styles, maxRows int) string {
	if f.loading {
		return s.Accent.Render("modelo") + "\n\n" + s.Muted.Render("buscando modelos…")
	}
	if f.manual {
		return s.Accent.Render("modelo para "+f.active) + "\n\n" +
			f.input.View() + "\n\n" +
			s.Muted.Render("enter confirma · esc cancela")
	}

	var b strings.Builder
	b.WriteString(s.Accent.Render("elegí un modelo") + "\n")
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

	top, end := f.visibleWindow(maxRows)
	if top > 0 {
		b.WriteString(s.Muted.Render(fmt.Sprintf("  ↑ %d más", top)) + "\n")
	}
	lastProv := ""
	for i := top; i < end; i++ {
		r := f.rows[i]
		if r.manual {
			b.WriteString(cur(i == f.idx) + "✎ escribir a mano  " + s.Muted.Render("("+f.active+")") + "\n")
			continue
		}
		if r.provider != lastProv {
			// header aligned to the same 2-col gutter as the model rows
			b.WriteString("  " + s.Muted.Render(r.provider) + "\n")
			lastProv = r.provider
		}
		line := r.model
		if r.current {
			line += s.Muted.Render("  ● actual")
		}
		b.WriteString(cur(i == f.idx) + line + "\n")
	}
	if end < len(f.rows) {
		b.WriteString(s.Muted.Render(fmt.Sprintf("  ↓ %d más", len(f.rows)-end)) + "\n")
	}
	b.WriteString("\n" + s.Muted.Render("↑↓ elegir · enter · esc cancela"))
	return b.String()
}
