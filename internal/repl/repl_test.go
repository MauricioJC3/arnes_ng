package repl

import (
	"bufio"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/command"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/session"
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

func (f *fakeApp) ListSubagents() []string {
	return []string{"research: explora", "test-writer: testea"}
}

// fakeFreshApp is a Conversation that also implements FreshFactory, so /goal
// --fresh gets a new conversation per iteration.
type fakeFreshApp struct {
	fakeConv
	fresh *fakeConv
}

func (f *fakeFreshApp) FreshConversation() command.Conversation {
	if f.fresh == nil {
		f.fresh = &fakeConv{reply: "listo\n" + "ARNES_GOAL_DONE"}
	}
	return f.fresh
}

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

	t.Run("un error de la conversación se reporta y el REPL sigue", func(t *testing.T) {
		conv := &fakeConv{err: errors.New("se cayó el provider")}
		out := run(t, "hola\n/exit\n", conv, provider.NewMock())
		if !strings.Contains(out, "error: se cayó el provider") {
			t.Fatalf("no reportó el error de la conversación:\n%s", out)
		}
	})

	t.Run("un error de lectura (no EOF) corta el loop", func(t *testing.T) {
		var out strings.Builder
		r := New(&fakeConv{}, provider.NewMock(), bufio.NewReader(errReader{}), &out)
		if err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "disco") {
			t.Fatalf("Run debería devolver el error de lectura, dio %v", err)
		}
	})
}

// errReader fails every Read with a non-EOF error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("falló el disco") }

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

func TestREPLGoal(t *testing.T) {
	t.Run("corre hasta el sentinel e imprime progreso y resumen", func(t *testing.T) {
		conv := &fakeConv{reply: "trabajo hecho\n" + "ARNES_GOAL_DONE"}
		out := run(t, "/goal 3 arreglá el parser\n/exit\n", conv, provider.NewMock())
		if len(conv.inputs) != 1 {
			t.Fatalf("el sentinel debería cortar en la 1ª iteración, hubo %d", len(conv.inputs))
		}
		if !strings.Contains(out, "— iteración 1/3 —") {
			t.Fatalf("no imprimió el progreso:\n%s", out)
		}
		if !strings.Contains(out, "objetivo: completado") {
			t.Fatalf("no imprimió el resumen del goal:\n%s", out)
		}
	})

	t.Run("--fresh usa una conversación nueva, no la viva", func(t *testing.T) {
		app := &fakeFreshApp{}
		out := run(t, "/goal --fresh dale\n/exit\n", app, provider.NewMock())
		if len(app.fakeConv.inputs) != 0 {
			t.Fatalf("la conversación viva no debería usarse con --fresh: %v", app.fakeConv.inputs)
		}
		if app.fresh == nil || len(app.fresh.inputs) != 1 {
			t.Fatalf("la conversación fresca no recibió el turno")
		}
		if !strings.Contains(out, "objetivo: completado") {
			t.Fatalf("out:\n%s", out)
		}
	})

	t.Run("--fresh sin FreshFactory cae a la conversación viva", func(t *testing.T) {
		conv := &fakeConv{reply: "x\n" + "ARNES_GOAL_DONE"}
		run(t, "/goal --fresh probando\n/exit\n", conv, provider.NewMock())
		if len(conv.inputs) != 1 {
			t.Fatalf("sin FreshFactory debería usar la conv viva: %v", conv.inputs)
		}
	})

	t.Run("un error del loop se reporta y el REPL sigue", func(t *testing.T) {
		conv := &fakeConv{reply: "boom", err: errors.New("falló el turno")}
		out := run(t, "/goal 2 hacé algo\n/exit\n", conv, provider.NewMock())
		if !strings.Contains(out, "error: falló el turno") {
			t.Fatalf("no reportó el error del goal:\n%s", out)
		}
		if !strings.Contains(out, "objetivo: error") {
			t.Fatalf("no imprimió el resumen tras el error:\n%s", out)
		}
	})
}
