package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
)

// promptInput owns the text prompt: the textarea itself, the "/…" command menu
// that autocompletes on top of it, and the recall history for ↑/↓.
type promptInput struct {
	ta   textarea.Model
	menu commandMenu

	history []string // submitted inputs, newest last
	histAt  int      // index into history; len(history) == showing the draft
	draft   string   // what the user was typing before starting a recall
}

// newPromptInput builds the textarea with its placeholder, prompt glyph and
// sizing.
func newPromptInput(styles Styles) promptInput {
	ta := textarea.New()
	ta.Placeholder = "escribí un mensaje…  (/help · Esc Esc para salir)"
	ta.Prompt = styles.Accent.Render("❯ ")
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	ta.CharLimit = 0
	ta.Focus()
	return promptInput{ta: ta}
}

// navAction is what a ↑/↓ press should do to the transcript after the input has
// had its say (history recall is applied in-place first).
type navAction int

const (
	navConsumed   navAction = iota // handled here; the caller does nothing
	navTextarea                    // let the textarea handle the key (cursor move)
	navScrollUp                    // scroll the transcript up one line
	navScrollDown                  // scroll the transcript down one line
)

// onUp decides what ↑ does. Priority: continue an in-progress recall; then, with
// a non-empty input, let the textarea move the cursor; then, if the transcript
// is scrolled up, keep scrolling it; otherwise start a recall, or scroll when
// there is no history.
func (p *promptInput) onUp(transcriptAtBottom bool) navAction {
	switch {
	case p.histAt < len(p.history):
		p.histPrev()
		return navConsumed
	case p.ta.Value() != "":
		return navTextarea
	case !transcriptAtBottom:
		return navScrollUp
	case len(p.history) > 0:
		p.histPrev()
		return navConsumed
	default:
		return navScrollUp
	}
}

// onDown mirrors onUp toward the newest input; at the bottom with an empty
// input it defers to the textarea (matching ↑'s fall-through).
func (p *promptInput) onDown(transcriptAtBottom bool) navAction {
	switch {
	case p.histAt < len(p.history):
		p.histNext()
		return navConsumed
	case p.ta.Value() != "":
		return navTextarea
	case !transcriptAtBottom:
		return navScrollDown
	default:
		return navTextarea
	}
}

// histPrev recalls an older input into the textarea. Returns false when there is
// nothing older.
func (p *promptInput) histPrev() bool {
	if len(p.history) == 0 || p.histAt == 0 {
		return false
	}
	if p.histAt == len(p.history) {
		p.draft = p.ta.Value()
	}
	p.histAt--
	p.ta.SetValue(p.history[p.histAt])
	p.ta.CursorEnd()
	return true
}

// histNext moves toward the newest input, restoring the saved draft past the end.
func (p *promptInput) histNext() bool {
	if p.histAt >= len(p.history) {
		return false
	}
	p.histAt++
	if p.histAt == len(p.history) {
		p.ta.SetValue(p.draft)
	} else {
		p.ta.SetValue(p.history[p.histAt])
	}
	p.ta.CursorEnd()
	return true
}

// remember appends a submitted input to the recall history (skips duplicates of
// the immediately previous entry) and resets the cursor past the end.
func (p *promptInput) remember(text string) {
	if n := len(p.history); n == 0 || p.history[n-1] != text {
		p.history = append(p.history, text)
	}
	p.histAt = len(p.history)
	p.draft = ""
}

// detachHistory leaves recall mode (called when the user edits the input).
func (p *promptInput) detachHistory() { p.histAt = len(p.history) }
