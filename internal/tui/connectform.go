package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

var connectProviders = []string{"anthropic", "deepseek", "kimi", "openai"}

// connectModels is the offline fallback used when the live /models lookup fails
// (no network, bad key, provider without the capability). Kept short on purpose.
var connectModels = map[string][]string{
	"anthropic": {"claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5", "claude-opus-4-8", "claude-fable-5"},
	"deepseek":  {"deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"},
	"kimi":      {"kimi-k2", "moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k"},
	"openai":    {"gpt-4o", "gpt-4o-mini", "gpt-4.1", "gpt-4.1-mini"},
}

const manualModelOption = "✎ escribir a mano…"

// ListModelsFunc fetches the model ids a provider offers for the given key. It
// is wired from the command layer; nil disables the live lookup.
type ListModelsFunc func(ctx context.Context, provider, apiKey string) ([]string, error)

// connectModelsMsg carries the result of the async /models lookup back to the
// form.
type connectModelsMsg struct {
	models []string
	err    error
}

// fetchConnectModels runs the lookup off the UI goroutine with a short timeout.
func fetchConnectModels(fn ListModelsFunc, provider, key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		models, err := fn(ctx, provider, key)
		return connectModelsMsg{models: models, err: err}
	}
}

type connectStep int

const (
	stepProvider connectStep = iota
	stepKey
	stepModel
)

// connectForm is the interactive picker for a bare /connect. The steps run
// provider -> API key -> model, because the model list is fetched live and that
// needs the key.
type connectForm struct {
	step     connectStep
	provIdx  int
	provider string

	key string // captured at stepKey, also fed to the model lookup

	models   []string // picker rows (manualModelOption last); nil while loading
	modelIdx int
	loading  bool
	note     string // shown when the live lookup fell back to the offline list
	manual   bool   // the user chose "escribir a mano…": stepModel is a text input
	model    string

	input textinput.Model
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

// update handles one key. At most one of (done, cancelled) is set; fetch is true
// on the single transition into the model step, asking the caller to kick off
// the /models lookup.
func (f *connectForm) update(msg tea.KeyMsg) (done *connectResult, cancelled, fetch bool) {
	key := msg.String()
	if key == "esc" {
		return nil, true, false
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
			f.step = stepKey
			f.input.Reset()
			f.input.EchoMode = textinput.EchoPassword
			f.input.Focus()
		}

	case stepKey:
		if key == "enter" {
			f.key = strings.TrimSpace(f.input.Value())
			f.step = stepModel
			f.loading = true
			return nil, false, true
		}
		f.input, _ = f.input.Update(msg)

	case stepModel:
		return f.updateModelStep(msg, key)
	}
	return nil, false, false
}

func (f *connectForm) updateModelStep(msg tea.KeyMsg, key string) (done *connectResult, cancelled, fetch bool) {
	if f.loading {
		return nil, false, false // ignore input until the list arrives
	}

	if f.manual {
		if key == "enter" {
			if m := strings.TrimSpace(f.input.Value()); m != "" {
				return &connectResult{f.provider, m, f.key}, false, false
			}
			return nil, false, false
		}
		f.input, _ = f.input.Update(msg)
		return nil, false, false
	}

	switch key {
	case "up", "ctrl+p":
		f.modelIdx = (f.modelIdx - 1 + len(f.models)) % len(f.models)
	case "down", "ctrl+n":
		f.modelIdx = (f.modelIdx + 1) % len(f.models)
	case "enter":
		if f.models[f.modelIdx] == manualModelOption {
			f.manual = true
			f.input.Reset()
			f.input.EchoMode = textinput.EchoNormal
			f.input.Focus()
			return nil, false, false
		}
		return &connectResult{f.provider, f.models[f.modelIdx], f.key}, false, false
	}
	return nil, false, false
}

// setModels installs the picker rows once the lookup returns. On error or an
// empty result it falls back to the offline list for the provider.
func (f *connectForm) setModels(models []string, err error) {
	f.loading = false
	if err != nil || len(models) == 0 {
		models = connectModels[f.provider]
		if err != nil {
			f.note = "no se pudo consultar la lista; modelos locales"
		}
	}
	f.models = append(append([]string{}, models...), manualModelOption)
	f.modelIdx = 0
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

	case stepKey:
		return s.Accent.Render("API key para "+f.provider) + "\n" +
			s.Muted.Render("(enter vacío = mantener la key guardada)") + "\n\n" +
			f.input.View() + "\n\n" +
			s.Muted.Render("enter confirma · esc cancela")

	case stepModel:
		if f.loading {
			return s.Accent.Render("modelo para "+f.provider) + "\n\n" +
				s.Muted.Render("buscando modelos…")
		}
		if f.manual {
			return s.Accent.Render("modelo para "+f.provider) + "\n\n" +
				f.input.View() + "\n\n" +
				s.Muted.Render("enter confirma · esc cancela")
		}
		var b strings.Builder
		b.WriteString(s.Accent.Render("modelo para "+f.provider) + "\n")
		if f.note != "" {
			b.WriteString(s.Muted.Render("("+f.note+")") + "\n")
		}
		b.WriteString("\n")
		for i, name := range f.models {
			if i == f.modelIdx {
				b.WriteString(s.Accent.Render("❯ ") + s.User.Render(name) + "\n")
			} else {
				b.WriteString("  " + name + "\n")
			}
		}
		b.WriteString("\n" + s.Muted.Render("↑↓ elegir · enter · esc cancela"))
		return b.String()
	}
	return ""
}
