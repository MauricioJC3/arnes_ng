package repl

import (
	"bufio"
	"context"
	"strings"
	"testing"

	"github.com/andresmjimenez/arnes/internal/command"
	"github.com/andresmjimenez/arnes/internal/provider"
	"github.com/andresmjimenez/arnes/internal/session"
)

type fakeConv struct {
	inputs []string
	reply  string
	err    error
}

func (f *fakeConv) Run(_ context.Context, in string) (string, error) {
	f.inputs = append(f.inputs, in)
	return f.reply, f.err
}

// fakeApp is a Conversation that also implements Sessions and Compaction.
type fakeApp struct {
	fakeConv
	metas    []session.Meta
	resumed  string
	newed    int
	strategy string
	compacts int
}

func (f *fakeApp) ListSessions() ([]session.Meta, error) { return f.metas, nil }

func (f *fakeApp) ResumeSession(id string) (string, error) {
	f.resumed = id
	return "reanudada " + id, nil
}

func (f *fakeApp) NewSession() (string, error) {
	f.newed++
	return "sesión nueva", nil
}

func (f *fakeApp) SetStrategy(name string) (string, error) {
	f.strategy = name
	return "estrategia: " + name, nil
}

func (f *fakeApp) Compact() (string, error) {
	f.compacts++
	return "compactado", nil
}

func (f *fakeApp) ListSubagents() []string { return []string{"research: explora", "test-writer: testea"} }

func run(t *testing.T, script string, conv command.Conversation, p provider.Provider) string {
	t.Helper()
	var out strings.Builder
	r := New(conv, p, bufio.NewReader(strings.NewReader(script)), &out)
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run devolvió error: %v", err)
	}
	return out.String()
}

func TestREPL(t *testing.T) {
	t.Run("manda el texto a la conversación e imprime la respuesta", func(t *testing.T) {
		conv := &fakeConv{reply: "respuesta del agente"}
		out := run(t, "hola\n/exit\n", conv, provider.NewMock())
		if len(conv.inputs) != 1 || conv.inputs[0] != "hola" {
			t.Fatalf("inputs = %v", conv.inputs)
		}
		if !strings.Contains(out, "respuesta del agente") {
			t.Fatalf("no se imprimió la respuesta:\n%s", out)
		}
	})

	t.Run("los slash commands no llegan a la conversación", func(t *testing.T) {
		conv := &fakeConv{reply: "x"}
		out := run(t, "/help\n/exit\n", conv, provider.NewMock())
		if len(conv.inputs) != 0 {
			t.Fatalf("un comando llegó a la conversación: %v", conv.inputs)
		}
		if !strings.Contains(out, "/model") {
			t.Fatalf("/help no listó los comandos:\n%s", out)
		}
	})

	t.Run("/model cambia el modelo del provider", func(t *testing.T) {
		p := provider.NewMock()
		out := run(t, "/model claude-opus-5\n/exit\n", &fakeConv{}, p)
		if p.Model() != "claude-opus-5" {
			t.Fatalf("modelo = %q", p.Model())
		}
		if !strings.Contains(out, "claude-opus-5") {
			t.Fatalf("no confirmó el cambio:\n%s", out)
		}
	})

	t.Run("EOF sin /exit corta limpio y procesa la última línea", func(t *testing.T) {
		out := run(t, "hola\n", &fakeConv{reply: "ok"}, provider.NewMock())
		if !strings.Contains(out, "ok") {
			t.Fatalf("no procesó la última línea antes del EOF:\n%s", out)
		}
	})

	t.Run("comando desconocido avisa y sigue", func(t *testing.T) {
		out := run(t, "/nope\n/exit\n", &fakeConv{}, provider.NewMock())
		if !strings.Contains(out, "desconocido") {
			t.Fatalf("no avisó del comando inválido:\n%s", out)
		}
	})
}

func TestREPLSessionCommands(t *testing.T) {
	t.Run("/sessions lista cuando la conversación lo soporta", func(t *testing.T) {
		app := &fakeApp{metas: []session.Meta{
			{ID: "20260827-100000-aa", Title: "primera", Messages: 4},
			{ID: "20260827-110000-bb", Title: "", Messages: 2},
		}}
		out := run(t, "/sessions\n/exit\n", app, provider.NewMock())
		if !strings.Contains(out, "20260827-100000-aa") || !strings.Contains(out, "primera") {
			t.Fatalf("listado incompleto:\n%s", out)
		}
		if !strings.Contains(out, "(sin título)") {
			t.Fatalf("no marcó la sesión sin título:\n%s", out)
		}
	})

	t.Run("/resume delega el id y confirma", func(t *testing.T) {
		app := &fakeApp{}
		out := run(t, "/resume abc123\n/exit\n", app, provider.NewMock())
		if app.resumed != "abc123" {
			t.Fatalf("resumed = %q", app.resumed)
		}
		if !strings.Contains(out, "reanudada abc123") {
			t.Fatalf("no confirmó:\n%s", out)
		}
	})

	t.Run("/resume sin id muestra el uso", func(t *testing.T) {
		out := run(t, "/resume\n/exit\n", &fakeApp{}, provider.NewMock())
		if !strings.Contains(out, "uso: /resume") {
			t.Fatalf("no mostró el uso:\n%s", out)
		}
	})

	t.Run("/new arranca una sesión nueva", func(t *testing.T) {
		app := &fakeApp{}
		run(t, "/new\n/exit\n", app, provider.NewMock())
		if app.newed != 1 {
			t.Fatalf("newed = %d", app.newed)
		}
	})

	t.Run("sin gestión de sesiones, avisa en vez de crashear", func(t *testing.T) {
		out := run(t, "/sessions\n/resume x\n/new\n/exit\n", &fakeConv{}, provider.NewMock())
		if strings.Count(out, "no tiene gestión de sesiones") != 3 {
			t.Fatalf("esperaba 3 avisos:\n%s", out)
		}
	})
}

func TestREPLCompact(t *testing.T) {
	t.Run("/compact sin args compacta con la estrategia actual", func(t *testing.T) {
		app := &fakeApp{}
		out := run(t, "/compact\n/exit\n", app, provider.NewMock())
		if app.compacts != 1 || app.strategy != "" {
			t.Fatalf("compacts=%d strategy=%q", app.compacts, app.strategy)
		}
		if !strings.Contains(out, "compactado") {
			t.Fatalf("no confirmó:\n%s", out)
		}
	})

	t.Run("/compact <estrategia> cambia y compacta", func(t *testing.T) {
		app := &fakeApp{}
		run(t, "/compact summarize\n/exit\n", app, provider.NewMock())
		if app.strategy != "summarize" || app.compacts != 1 {
			t.Fatalf("strategy=%q compacts=%d", app.strategy, app.compacts)
		}
	})

	t.Run("sin compactación, avisa", func(t *testing.T) {
		out := run(t, "/compact\n/exit\n", &fakeConv{}, provider.NewMock())
		if !strings.Contains(out, "no tiene compactación") {
			t.Fatalf("out:\n%s", out)
		}
	})
}

func TestREPLSubagents(t *testing.T) {
	t.Run("/subagents lista cuando la conversación lo soporta", func(t *testing.T) {
		out := run(t, "/subagents\n/exit\n", &fakeApp{}, provider.NewMock())
		if !strings.Contains(out, "research: explora") || !strings.Contains(out, "test-writer: testea") {
			t.Fatalf("no listó los subagentes:\n%s", out)
		}
	})

	t.Run("sin subagentes, avisa", func(t *testing.T) {
		out := run(t, "/subagents\n/exit\n", &fakeConv{}, provider.NewMock())
		if !strings.Contains(out, "no tiene subagentes") {
			t.Fatalf("out:\n%s", out)
		}
	})
}
