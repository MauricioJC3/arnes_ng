package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
)

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

// transcript owns the scrollback: the committed entries, the in-flight streamed
// text for the current turn, the viewport that displays them, and the glamour
// renderer (cached per width) used to format assistant markdown. Keeping the
// renderer cache in here isolates every rendering side effect from the rest of
// the model.
type transcript struct {
	vp      viewport.Model
	entries []entry
	live    string // streamed text for the current turn, not yet committed

	styles  Styles
	mdStyle string // glamour style name; never "auto" (that queries the terminal)
	md      *glamour.TermRenderer
	mdWidth int

	width int // last known terminal width, for renderer sizing
	ready bool
}

// resize (re)creates or resizes the viewport for the given dimensions and
// re-renders the content pinned to the bottom.
func (t *transcript) resize(width, height int) {
	t.width = width
	if !t.ready {
		t.vp = viewport.New(width, height)
		t.ready = true
	} else {
		t.vp.Width = width
		t.vp.Height = height
	}
	t.setContent(true)
}

// add appends an entry. The scroll position is kept unless the viewport was
// already at the bottom (so reading history isn't interrupted); a user message
// always jumps to the bottom.
func (t *transcript) add(k entryKind, text string) {
	e := entry{kind: k, text: text}
	if k == kindAssistant {
		e.rendered = t.markdown(text)
	}
	atBottom := !t.ready || t.vp.AtBottom() || k == kindUser
	t.entries = append(t.entries, e)
	t.setContent(atBottom)
}

// commitLive moves the streamed text of this turn into a permanent entry.
func (t *transcript) commitLive() {
	if t.live == "" {
		return
	}
	t.add(kindAssistant, t.live)
	t.live = ""
}

// appendDelta adds one streamed chunk to the in-flight text, keeping the
// viewport pinned to the bottom only if it was already there.
func (t *transcript) appendDelta(s string) {
	atBottom := !t.ready || t.vp.AtBottom()
	t.live += s
	t.setContent(atBottom)
}

// dropLive discards the partial streamed buffer (the result text is
// authoritative).
func (t *transcript) dropLive() { t.live = "" }

// reflow rebuilds every assistant entry through a fresh renderer, for use after
// a width change.
func (t *transcript) reflow() {
	t.md = nil
	for i := range t.entries {
		if t.entries[i].kind == kindAssistant {
			t.entries[i].rendered = t.markdown(t.entries[i].text)
		}
	}
	t.setContent(t.vp.AtBottom())
}

// setContent re-renders the viewport, scrolling to the bottom only when asked
// (so a user reading history isn't yanked down by streaming).
func (t *transcript) setContent(gotoBottom bool) {
	if !t.ready {
		return
	}
	t.vp.SetContent(t.renderTranscript())
	if gotoBottom {
		t.vp.GotoBottom()
	}
}

// markdown renders s through glamour, caching the renderer per width. On any
// error it returns the raw text.
func (t *transcript) markdown(s string) string {
	w := t.vp.Width - 2
	if w < 20 {
		w = 20
	}
	if t.md == nil || t.mdWidth != w {
		style := t.mdStyle
		if style == "" || style == "auto" {
			// "auto" queries the terminal background over stdin, which races
			// bubbletea for the event loop and leaks the OSC 11 reply into the
			// input. Pick an explicit style instead.
			style = "dark"
		}
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle(style), glamour.WithWordWrap(w))
		if err != nil {
			return s
		}
		t.md, t.mdWidth = r, w
	}
	out, err := t.md.Render(s)
	if err != nil {
		return s
	}
	return strings.Trim(out, "\n")
}

// renderTranscript styles every entry (plus any in-flight streamed text). Each
// entry is wrapped to the viewport width individually -- assistant entries are
// already wrapped by glamour, so they are left as-is.
func (t *transcript) renderTranscript() string {
	w := t.vp.Width
	if w <= 0 {
		w = t.width
	}
	if w <= 0 {
		w = 80
	}

	lines := make([]string, 0, len(t.entries)+1)
	for _, e := range t.entries {
		lines = append(lines, t.renderEntry(e, w))
	}
	if t.live != "" {
		lines = append(lines, wrapTo(t.styles.Assistant.Render(t.live), w))
	}
	return strings.Join(lines, "\n\n")
}

func (t *transcript) renderEntry(e entry, w int) string {
	switch e.kind {
	case kindUser:
		return wrapTo(t.styles.Accent.Render("▌")+" "+t.styles.User.Render(e.text), w)
	case kindAssistant:
		if e.rendered != "" {
			return e.rendered // glamour output, already wrapped
		}
		return wrapTo(t.styles.Assistant.Render(e.text), w)
	case kindError:
		return wrapTo(t.styles.Error.Render("✗ "+e.text), w)
	default: // kindInfo
		return wrapTo(t.styles.Muted.Render(e.text), w)
	}
}
