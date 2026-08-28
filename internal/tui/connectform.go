package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

var connectProviders = []string{"anthropic", "deepseek", "kimi", "openai"}

var connectDefaultModel = map[string]string{
	"anthropic": "claude-opus-5",
	"deepseek":  "deepseek-chat",
	"kimi":      "moonshot-v1-8k",
	"openai":    "gpt-4o",
}

type connectStep int

const (
	stepProvider connectStep = iota
	stepModel
	stepKey
)

// connectForm is the interactive picker for a bare /connect.
type connectForm struct {
	step     connectStep
	provIdx  int
	provider string
	model    string
	input    textinput.Model
}

func newConnectForm() *connectForm {
	ti := textinput.New()
	ti.Prompt = "  "
	return &connectForm{input: ti}
}

// connectResult is returned when the form is done.
type connectResult struct {
	provider, model, key string
}

// update handles one key. Exactly one of (done, cancelled) can be non-nil/true.
func (f *connectForm) update(msg tea.KeyMsg) (done *connectResult, cancelled bool) {
	key := msg.String()
	if key == "esc" {
		return nil, true
	}

	switch f.step {
	case stepProvider:
		switch key {
		case "up", "ctrl+p":
			f.provIdx = (f.provIdx - 1 + len(connectProviders)) % len(connectProviders)
		case "down", "ctrl+n":
			f.provIdx = (f.provIdx + 1) % len(connectProviders)
		case "enter":
			f.provider = connectProviders[f.provIdx]
			f.step = stepModel
			f.input.EchoMode = textinput.EchoNormal
			f.input.SetValue(connectDefaultModel[f.provider])
			f.input.CursorEnd()
			f.input.Focus()
		}

	case stepModel:
		if key == "enter" {
			f.model = strings.TrimSpace(f.input.Value())
			f.step = stepKey
			f.input.Reset()
			f.input.EchoMode = textinput.EchoPassword
			f.input.Focus()
			return nil, false
		}
		f.input, _ = f.input.Update(msg)

	case stepKey:
		if key == "enter" {
			return &connectResult{
				provider: f.provider,
				model:    f.model,
				key:      strings.TrimSpace(f.input.Value()),
			}, false
		}
		f.input, _ = f.input.Update(msg)
	}
	return nil, false
}

func (f *connectForm) view(s Styles) string {
	switch f.step {
	case stepProvider:
		var b strings.Builder
		b.WriteString(s.Accent.Render("elegí un proveedor") + "\n\n")
		for i, p := range connectProviders {
			if i == f.provIdx {
				b.WriteString(s.Accent.Render("❯ ") + s.User.Render(p) + "\n")
			} else {
				b.WriteString("  " + p + "\n")
			}
		}
		b.WriteString("\n" + s.Muted.Render("↑↓ elegir · enter · esc cancela"))
		return b.String()

	case stepModel:
		return s.Accent.Render("modelo para "+f.provider) + "\n\n" +
			f.input.View() + "\n\n" +
			s.Muted.Render("enter confirma · esc cancela")

	case stepKey:
		return s.Accent.Render("API key para "+f.provider) + "\n" +
			s.Muted.Render("(enter vacío = mantener la key guardada)") + "\n\n" +
			f.input.View() + "\n\n" +
			s.Muted.Render("enter confirma · esc cancela")
	}
	return ""
}
