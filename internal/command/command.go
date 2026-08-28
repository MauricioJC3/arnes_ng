// Package command dispatches slash commands. Both front-ends (the plain REPL and
// the TUI) call Dispatch so the command set stays identical.
package command

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/session"
)

// Conversation is one user turn in, final text out.
type Conversation interface {
	Run(ctx context.Context, userInput string) (string, error)
}

// Sessions is implemented by a Conversation that manages session lifecycle.
type Sessions interface {
	ListSessions() ([]session.Meta, error)
	ResumeSession(id string) (string, error)
	NewSession() (string, error)
}

// Compaction is implemented by a Conversation that can compact its context.
type Compaction interface {
	SetStrategy(name string) (string, error)
	Compact() (string, error)
}

// Subagents is implemented by a Conversation that exposes delegation targets.
type Subagents interface {
	ListSubagents() []string
}

// Connector is implemented by a Conversation that can switch provider/model and
// persist the choice (the /connect command).
type Connector interface {
	Connect(providerName, model, apiKey string) (string, error)
}

// Modeler is implemented by a Conversation that can report and change the model
// on the active provider and persist it (the /model command and its picker).
type Modeler interface {
	ActiveProvider() string   // name of the provider currently in use
	Model() string            // its current model id
	KeyedProviders() []string // providers with a usable key, active one first
	SetModel(model string) (string, error)
}

// Modes is implemented by a Conversation with switchable permission modes
// (normal / auto / plan) -- the /mode command and the TUI's shift+tab.
type Modes interface {
	Mode() string
	SetMode(name string) (string, error)
}

// Coster is implemented by a Conversation that can report token spend, current
// and historical (the /cost command).
type Coster interface {
	CostReport() (string, error)
}

// Result is the outcome of a slash command: text to show, whether to quit, and
// an optional goal run for the front-end to kick off.
type Result struct {
	Output string
	Exit   bool
	Goal   *GoalRequest
}

// GoalRequest asks the front-end to start a Ralph-style goal loop.
type GoalRequest struct {
	Text    string
	MaxIter int  // 0 = the goal package's default
	Fresh   bool // --fresh: a new empty-context agent each iteration
}

// FreshFactory is implemented by a Conversation that can spawn a fresh,
// empty-context conversation for /goal --fresh.
type FreshFactory interface {
	FreshConversation() Conversation
}

// Updater is implemented by a Conversation that can replace the running arnes
// binary with a newer release (the /update-arnes command).
type Updater interface {
	SelfUpdate(ctx context.Context) (string, error)
}

// Rewinder is implemented by a Conversation that keeps per-turn restore points
// (the /rewind command).
type Rewinder interface {
	ListCheckpoints() string
	Rewind(n int) (string, error)
}

// Spec describes one slash command, for /help and for the TUI autocomplete.
type Spec struct {
	Name  string // "/connect"
	Args  string // "<prov> [modelo] [key]"
	Short string // one-line description
}

// Commands returns every slash command, in display order.
func Commands() []Spec {
	return []Spec{
		{"/help", "", "esta ayuda"},
		{"/connect", "[prov] [modelo] [key]", "cambia de proveedor y lo deja guardado"},
		{"/mode", "[normal|auto|plan]", "cambia el modo de permisos"},
		{"/goal", "[--fresh] [maxIter] <objetivo>", "itera autónomamente hasta cumplir el objetivo"},
		{"/cost", "", "gasto de tokens: sesión actual + historial"},
		{"/model", "[nombre]", "muestra o cambia el modelo"},
		{"/sessions", "", "lista las sesiones guardadas"},
		{"/resume", "<id>", "reanuda una sesión (acepta prefijo)"},
		{"/new", "", "empieza una sesión nueva"},
		{"/compact", "[estrategia]", "compacta el contexto (none|sliding|summarize)"},
		{"/rewind", "[n]", "lista los checkpoints o vuelve al checkpoint n (historial + archivos)"},
		{"/subagents", "", "lista los subagentes disponibles"},
		{"/update-arnes", "", "busca e instala una versión nueva de arnes"},
		{"/exit", "", "salir"},
	}
}

// Help is the text shown by /help, derived from Commands.
var Help = helpText()

func helpText() string {
	var b strings.Builder
	b.WriteString("comandos:\n")
	for _, c := range Commands() {
		left := c.Name
		if c.Args != "" {
			left += " " + c.Args
		}
		fmt.Fprintf(&b, "  %-30s %s\n", left, c.Short)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Dispatch handles one slash-command line. conv is the active conversation (it
// may also implement Sessions / Compaction / Subagents); prov backs /model.
func Dispatch(line string, conv Conversation, prov provider.Provider) (Result, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Result{}, nil
	}

	switch fields[0] {
	case "/help":
		return Result{Output: Help}, nil

	case "/connect":
		conn, ok := conv.(Connector)
		if !ok {
			return Result{}, errors.New("este arnés no soporta /connect")
		}
		if len(fields) < 2 {
			return Result{}, errors.New("uso: /connect <provider> [modelo] [api-key]  (provider: anthropic|deepseek|kimi|openai)")
		}
		model, key := "", ""
		if len(fields) >= 3 {
			model = fields[2]
		}
		if len(fields) >= 4 {
			key = fields[3]
		}
		msg, err := conn.Connect(fields[1], model, key)
		if err != nil {
			return Result{}, err
		}
		return Result{Output: msg}, nil

	case "/mode":
		md, ok := conv.(Modes)
		if !ok {
			return Result{}, errors.New("este arnés no soporta modos")
		}
		if len(fields) < 2 {
			return Result{Output: "modo actual: " + md.Mode() + "  (normal | auto | plan)"}, nil
		}
		msg, err := md.SetMode(fields[1])
		if err != nil {
			return Result{}, err
		}
		return Result{Output: msg}, nil

	case "/goal":
		if len(fields) < 2 {
			return Result{}, errors.New("uso: /goal [--fresh] [maxIter] <objetivo>")
		}
		args := fields[1:]
		fresh := false
		kept := args[:0]
		for _, a := range args {
			if a == "--fresh" {
				fresh = true
				continue
			}
			kept = append(kept, a)
		}
		args = kept

		maxIter := 0
		if len(args) > 0 {
			if n, convErr := strconv.Atoi(args[0]); convErr == nil && n > 0 && len(args) > 1 {
				maxIter = n
				args = args[1:]
			}
		}
		text := strings.TrimSpace(strings.Join(args, " "))
		if text == "" {
			return Result{}, errors.New("uso: /goal [--fresh] [maxIter] <objetivo>")
		}
		return Result{Goal: &GoalRequest{Text: text, MaxIter: maxIter, Fresh: fresh}}, nil

	case "/cost":
		c, ok := conv.(Coster)
		if !ok {
			return Result{}, errors.New("este arnés no lleva registro de costo")
		}
		out, err := c.CostReport()
		if err != nil {
			return Result{}, err
		}
		return Result{Output: out}, nil

	case "/model":
		md, hasModeler := conv.(Modeler)
		if len(fields) < 2 {
			if hasModeler {
				return Result{Output: "modelo actual: " + md.Model() + "  ·  /model <nombre> para cambiar"}, nil
			}
			return Result{Output: "modelo actual: " + prov.Model()}, nil
		}
		if hasModeler {
			out, err := md.SetModel(fields[1])
			if err != nil {
				return Result{}, err
			}
			return Result{Output: out}, nil
		}
		prov.SetModel(fields[1])
		return Result{Output: "modelo: " + prov.Model()}, nil

	case "/sessions", "/ls":
		return listSessions(conv)

	case "/resume":
		if len(fields) < 2 {
			return Result{}, errors.New("uso: /resume <id>")
		}
		return sessionCmd(conv, func(s Sessions) (string, error) { return s.ResumeSession(fields[1]) })

	case "/new":
		return sessionCmd(conv, func(s Sessions) (string, error) { return s.NewSession() })

	case "/compact":
		return compactCmd(conv, fields)

	case "/rewind":
		rw, ok := conv.(Rewinder)
		if !ok {
			return Result{}, errors.New("este arnés no tiene checkpoints")
		}
		if len(fields) < 2 {
			return Result{Output: rw.ListCheckpoints()}, nil
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil || n < 1 {
			return Result{}, fmt.Errorf("uso: /rewind [n]  (n es un número de checkpoint; sin n lista)")
		}
		out, err := rw.Rewind(n)
		if err != nil {
			if out != "" {
				return Result{Output: out}, err // partial success
			}
			return Result{}, err
		}
		return Result{Output: out}, nil

	case "/subagents":
		sa, ok := conv.(Subagents)
		if !ok {
			return Result{}, errors.New("este arnés no tiene subagentes")
		}
		lines := sa.ListSubagents()
		if len(lines) == 0 {
			return Result{Output: "no hay subagentes configurados"}, nil
		}
		return Result{Output: "  " + strings.Join(lines, "\n  ")}, nil

	case "/update-arnes", "/update":
		up, ok := conv.(Updater)
		if !ok {
			return Result{}, errors.New("este arnés no se puede autoactualizar")
		}
		out, err := up.SelfUpdate(context.Background())
		if err != nil {
			return Result{}, err
		}
		return Result{Output: out}, nil

	case "/exit", "/quit":
		return Result{Exit: true}, nil

	default:
		return Result{}, fmt.Errorf("comando desconocido: %s (probá /help)", fields[0])
	}
}

func sessionCmd(conv Conversation, fn func(Sessions) (string, error)) (Result, error) {
	sc, ok := conv.(Sessions)
	if !ok {
		return Result{}, errors.New("este arnés no tiene gestión de sesiones")
	}
	msg, err := fn(sc)
	if err != nil {
		return Result{}, err
	}
	return Result{Output: msg}, nil
}

func compactCmd(conv Conversation, fields []string) (Result, error) {
	c, ok := conv.(Compaction)
	if !ok {
		return Result{}, errors.New("este arnés no tiene compactación de contexto")
	}
	var out []string
	if len(fields) >= 2 {
		msg, err := c.SetStrategy(fields[1])
		if err != nil {
			return Result{}, err
		}
		out = append(out, msg)
	}
	msg, err := c.Compact()
	if err != nil {
		return Result{}, err
	}
	out = append(out, msg)
	return Result{Output: strings.Join(out, "\n")}, nil
}

func listSessions(conv Conversation) (Result, error) {
	sc, ok := conv.(Sessions)
	if !ok {
		return Result{}, errors.New("este arnés no tiene gestión de sesiones")
	}
	metas, err := sc.ListSessions()
	if err != nil {
		return Result{}, err
	}
	if len(metas) == 0 {
		return Result{Output: "no hay sesiones guardadas"}, nil
	}
	var b strings.Builder
	for i, m := range metas {
		title := m.Title
		if title == "" {
			title = "(sin título)"
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "  %s  %2d msg  %s", m.ID, m.Messages, title)
	}
	return Result{Output: b.String()}, nil
}
