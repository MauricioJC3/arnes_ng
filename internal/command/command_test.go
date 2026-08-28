package command

import (
	"cmp"
	"context"
	"strings"
	"testing"

	"github.com/andresmjimenez/arnes/internal/provider"
	"github.com/andresmjimenez/arnes/internal/session"
)

// fakeApp implements Conversation + Sessions + Compaction + Subagents.
type fakeApp struct {
	metas     []session.Meta
	resumed   string
	newed     int
	strategy  string
	compacts  int
	connected [3]string
	mode      string
}

func (*fakeApp) Run(context.Context, string) (string, error) { return "", nil }
func (f *fakeApp) ListSessions() ([]session.Meta, error)     { return f.metas, nil }
func (f *fakeApp) ResumeSession(id string) (string, error)   { f.resumed = id; return "reanudada " + id, nil }
func (f *fakeApp) NewSession() (string, error)               { f.newed++; return "sesión nueva", nil }
func (f *fakeApp) SetStrategy(n string) (string, error)      { f.strategy = n; return "estrategia: " + n, nil }
func (f *fakeApp) Compact() (string, error)                  { f.compacts++; return "compactado", nil }
func (f *fakeApp) ListSubagents() []string                   { return []string{"research: explora"} }

func (f *fakeApp) Connect(p, model, key string) (string, error) {
	f.connected = [3]string{p, model, key}
	return "conectado: " + p, nil
}

func (f *fakeApp) CostReport() (string, error) { return "sesión actual: $0.0000\nhistorial: (vacío)", nil }

func (f *fakeApp) Mode() string { return cmp.Or(f.mode, "normal") }
func (f *fakeApp) SetMode(name string) (string, error) {
	if name != "normal" && name != "auto" && name != "plan" {
		return "", errStr("modo desconocido")
	}
	f.mode = name
	return "modo: " + name, nil
}

type errStr string

func (e errStr) Error() string { return string(e) }

// bareConv only implements Conversation.
type bareConv struct{}

func (bareConv) Run(context.Context, string) (string, error) { return "", nil }

func TestDispatch(t *testing.T) {
	app := &fakeApp{metas: []session.Meta{{ID: "s1", Title: "primera", Messages: 3}}}
	p := provider.NewMock()

	t.Run("/help", func(t *testing.T) {
		r, err := Dispatch("/help", app, p)
		if err != nil || !strings.Contains(r.Output, "/model") || r.Exit {
			t.Fatalf("r=%+v err=%v", r, err)
		}
	})

	t.Run("/exit marca Exit", func(t *testing.T) {
		r, _ := Dispatch("/exit", app, p)
		if !r.Exit {
			t.Fatal("Exit debería ser true")
		}
	})

	t.Run("/model cambia el modelo", func(t *testing.T) {
		r, err := Dispatch("/model claude-opus-5", app, p)
		if err != nil || p.Model() != "claude-opus-5" || !strings.Contains(r.Output, "claude-opus-5") {
			t.Fatalf("r=%+v model=%q err=%v", r, p.Model(), err)
		}
	})

	t.Run("/sessions lista", func(t *testing.T) {
		r, _ := Dispatch("/sessions", app, p)
		if !strings.Contains(r.Output, "s1") || !strings.Contains(r.Output, "primera") {
			t.Fatalf("output: %q", r.Output)
		}
	})

	t.Run("/resume sin id es error", func(t *testing.T) {
		if _, err := Dispatch("/resume", app, p); err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("/resume delega", func(t *testing.T) {
		Dispatch("/resume abc", app, p)
		if app.resumed != "abc" {
			t.Fatalf("resumed = %q", app.resumed)
		}
	})

	t.Run("/compact summarize", func(t *testing.T) {
		Dispatch("/compact summarize", app, p)
		if app.strategy != "summarize" || app.compacts != 1 {
			t.Fatalf("strategy=%q compacts=%d", app.strategy, app.compacts)
		}
	})

	t.Run("comando desconocido es error", func(t *testing.T) {
		if _, err := Dispatch("/qwe", app, p); err == nil {
			t.Fatal("esperaba error")
		}
	})

	t.Run("Commands y Help están sincronizados", func(t *testing.T) {
		cmds := Commands()
		if len(cmds) < 5 {
			t.Fatalf("Commands() devolvió %d", len(cmds))
		}
		for _, c := range cmds {
			if !strings.Contains(Help, c.Name) {
				t.Errorf("Help no menciona %q", c.Name)
			}
		}
	})

	t.Run("/connect pasa provider, modelo y key", func(t *testing.T) {
		r, err := Dispatch("/connect deepseek deepseek-chat sk-123", app, p)
		if err != nil {
			t.Fatal(err)
		}
		if app.connected != [3]string{"deepseek", "deepseek-chat", "sk-123"} {
			t.Fatalf("connected = %v", app.connected)
		}
		if r.Output != "conectado: deepseek" {
			t.Fatalf("output = %q", r.Output)
		}
	})

	t.Run("/connect sin provider muestra el uso", func(t *testing.T) {
		if _, err := Dispatch("/connect", app, p); err == nil {
			t.Fatal("esperaba error de uso")
		}
	})

	t.Run("/connect sin soporte avisa", func(t *testing.T) {
		if _, err := Dispatch("/connect anthropic", bareConv{}, p); err == nil {
			t.Fatal("esperaba error 'no soporta /connect'")
		}
	})

	t.Run("/mode cambia el modo", func(t *testing.T) {
		if _, err := Dispatch("/mode plan", app, p); err != nil {
			t.Fatal(err)
		}
		if app.mode != "plan" {
			t.Fatalf("mode = %q", app.mode)
		}
		if _, err := Dispatch("/mode gaseoso", app, p); err == nil {
			t.Fatal("esperaba error con un modo inválido")
		}
	})

	t.Run("/mode sin arg muestra el actual", func(t *testing.T) {
		r, err := Dispatch("/mode", app, p)
		if err != nil || !strings.Contains(r.Output, "modo actual") {
			t.Fatalf("r=%+v err=%v", r, err)
		}
	})

	t.Run("/goal parsea objetivo y maxIter opcional", func(t *testing.T) {
		r, err := Dispatch("/goal arreglá el bug del login", app, p)
		if err != nil || r.Goal == nil {
			t.Fatalf("r=%+v err=%v", r, err)
		}
		if r.Goal.Text != "arreglá el bug del login" || r.Goal.MaxIter != 0 {
			t.Fatalf("goal = %+v", *r.Goal)
		}

		r, _ = Dispatch("/goal 5 escribí los tests", app, p)
		if r.Goal == nil || r.Goal.MaxIter != 5 || r.Goal.Text != "escribí los tests" {
			t.Fatalf("goal con maxIter = %+v", r.Goal)
		}

		// "/goal 5" solo (número sin objetivo) → el 5 es el objetivo, no maxIter
		r, _ = Dispatch("/goal 5", app, p)
		if r.Goal == nil || r.Goal.Text != "5" || r.Goal.MaxIter != 0 {
			t.Fatalf("goal ='/goal 5' = %+v", r.Goal)
		}

		// --fresh en cualquier posición
		r, _ = Dispatch("/goal --fresh 8 migrá la config", app, p)
		if r.Goal == nil || !r.Goal.Fresh || r.Goal.MaxIter != 8 || r.Goal.Text != "migrá la config" {
			t.Fatalf("goal --fresh = %+v", r.Goal)
		}

		if _, err := Dispatch("/goal", app, p); err == nil {
			t.Fatal("esperaba error sin objetivo")
		}
		if _, err := Dispatch("/goal --fresh", app, p); err == nil {
			t.Fatal("esperaba error: --fresh sin objetivo")
		}
	})

	t.Run("/cost delega en el reporte", func(t *testing.T) {
		r, err := Dispatch("/cost", app, p)
		if err != nil || !strings.Contains(r.Output, "sesión actual") {
			t.Fatalf("r=%+v err=%v", r, err)
		}
		if _, err := Dispatch("/cost", bareConv{}, p); err == nil {
			t.Fatal("esperaba error sin soporte de costo")
		}
	})

	t.Run("comandos de sesión sin soporte avisan", func(t *testing.T) {
		if _, err := Dispatch("/sessions", bareConv{}, p); err == nil {
			t.Fatal("esperaba error 'sin gestión de sesiones'")
		}
		if _, err := Dispatch("/compact", bareConv{}, p); err == nil {
			t.Fatal("esperaba error 'sin compactación'")
		}
		if _, err := Dispatch("/subagents", bareConv{}, p); err == nil {
			t.Fatal("esperaba error 'sin subagentes'")
		}
	})
}
