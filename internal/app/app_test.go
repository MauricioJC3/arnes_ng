package app

import (
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/config"
	"github.com/MauricioJC3/arnes_ng/internal/lsp"
	"github.com/MauricioJC3/arnes_ng/internal/memory"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/session"
	"github.com/MauricioJC3/arnes_ng/internal/skill"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
)

// newTestApp builds an App with a mock provider, an on-disk session store and an
// auto-approving gateway -- enough to exercise the use-case methods.
func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	store, err := session.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &App{
		providerName: "mock",
		prov:         provider.NewMock(),
		cfgPath:      dir + "/config.json",
		store:        store,
		tools:        tool.NewRegistry(),
		baseApprover: approval.AllowAll{},
		mode:         ModeNormal,
		subagents:    subagent.NewRegistry(subagent.Defaults()...),
	}
}

func TestConnect(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	msg, err := a.Connect("deepseek", "deepseek-chat", "sk-secret")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if a.providerName != "deepseek" || a.prov.Model() != "deepseek-chat" {
		t.Fatalf("estado tras Connect: name=%q model=%q", a.providerName, a.prov.Model())
	}
	if strings.Contains(msg, "sk-secret") {
		t.Fatalf("el mensaje filtró la key: %q", msg)
	}

	// quedó persistido
	saved, err := config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Provider != "deepseek" || saved.Model != "deepseek-chat" || saved.Keys["deepseek"] != "sk-secret" {
		t.Fatalf("config guardado: %+v", saved)
	}

	if _, err := a.Connect("gemini", "", ""); err == nil {
		t.Fatal("esperaba error con un provider desconocido")
	}
}

func TestListSubagents(t *testing.T) {
	a := newTestApp(t)
	lines := a.ListSubagents()
	if len(lines) != len(subagent.Defaults()) {
		t.Fatalf("líneas = %d, quiero %d", len(lines), len(subagent.Defaults()))
	}
	if !strings.HasPrefix(lines[0], "research: ") {
		t.Errorf("primera línea = %q", lines[0])
	}
}

func TestNewAndResumeByPrefix(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	first := a.sess.ID
	a.sess.Messages = []provider.Message{{Role: provider.RoleUser, Text: "hola"}}
	if err := a.store.Save(a.sess); err != nil {
		t.Fatal(err)
	}

	// otra sesión guardada, con un id claramente distinto
	other := session.New("mock", "m", "")
	other.ID = "20200101-000000-0000"
	if err := a.store.Save(other); err != nil {
		t.Fatal(err)
	}

	msg, err := a.ResumeSession(first[:15]) // prefijo 'YYYYMMDD-HHMMSS', no colisiona con 20200101
	if err != nil {
		t.Fatalf("ResumeSession por prefijo: %v", err)
	}
	if a.sess.ID != first {
		t.Fatalf("reanudó %q, quería %q", a.sess.ID, first)
	}
	if !strings.Contains(msg, "1 mensajes") {
		t.Errorf("mensaje de confirmación inesperado: %q", msg)
	}
}

func TestSessionUsageResetsOnNew(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	a.usedIn, a.usedOut = 500, 40
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	if in, out := a.SessionUsage(); in != 0 || out != 0 {
		t.Fatalf("SessionUsage tras /new = %d/%d, quiero 0/0", in, out)
	}
}

func TestResumeRestoresUsage(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	first := a.sess.ID
	a.sess.UsageIn, a.sess.UsageOut = 1200, 300
	if err := a.store.Save(a.sess); err != nil {
		t.Fatal(err)
	}

	if _, err := a.NewSession(); err != nil { // otra sesión, uso a cero
		t.Fatal(err)
	}
	if _, err := a.ResumeSession(first); err != nil {
		t.Fatal(err)
	}
	if in, out := a.SessionUsage(); in != 1200 || out != 300 {
		t.Fatalf("SessionUsage tras /resume = %d/%d, quiero 1200/300", in, out)
	}
}

func TestCostReport(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	a.sess.Model = "claude-opus-5"
	a.sess.UsageIn, a.sess.UsageOut = 1_000_000, 0
	if err := a.store.Save(a.sess); err != nil {
		t.Fatal(err)
	}
	a.prov.SetModel("claude-opus-5")
	a.usedIn = 1_000_000

	rep, err := a.CostReport()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep, "sesión actual") || !strings.Contains(rep, "$5.0000") {
		t.Fatalf("reporte inesperado:\n%s", rep)
	}
	if !strings.Contains(rep, "← actual") {
		t.Fatalf("no marcó la sesión actual:\n%s", rep)
	}
}

func TestResumeMissing(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.ResumeSession("20990101-000000-ffff"); err == nil {
		t.Fatal("esperaba error al reanudar una sesión inexistente")
	}
}

func TestModes(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	if a.Mode() != ModeNormal {
		t.Fatalf("modo inicial = %q", a.Mode())
	}

	if _, err := a.SetMode("plan"); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.effectiveApprover().(approval.ReadOnly); !ok {
		t.Fatalf("modo plan no usa ReadOnly: %T", a.effectiveApprover())
	}

	if _, err := a.SetMode("yolo"); err != nil { // alias de auto
		t.Fatal(err)
	}
	if a.Mode() != ModeAuto {
		t.Fatalf("yolo no mapeó a auto: %q", a.Mode())
	}
	if _, ok := a.effectiveApprover().(approval.AllowAll); !ok {
		t.Fatalf("modo auto no usa AllowAll: %T", a.effectiveApprover())
	}

	if _, err := a.SetMode("marciano"); err == nil {
		t.Fatal("esperaba error con un modo inválido")
	}
}

func TestSetModePersists(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetMode("auto"); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(a.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "auto" {
		t.Fatalf("el modo no se guardó en la config: %q", got.Mode)
	}
}

func TestSetStrategy(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	if _, err := a.SetStrategy("summarize"); err != nil {
		t.Fatalf("SetStrategy summarize: %v", err)
	}
	if a.ag.CompactorName() != "summarize" {
		t.Fatalf("estrategia activa = %q", a.ag.CompactorName())
	}
	if _, err := a.SetStrategy("gzip"); err == nil {
		t.Fatal("esperaba error con una estrategia desconocida")
	}
}

func TestModelerMethods(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	if a.ActiveProvider() != "mock" {
		t.Fatalf("ActiveProvider = %q", a.ActiveProvider())
	}
	if a.Model() == "" {
		t.Fatal("Model() vacío")
	}
	if kp := a.KeyedProviders(); len(kp) != 1 || kp[0] != "mock" {
		t.Fatalf("KeyedProviders = %v (sin keys, sólo el activo)", kp)
	}

	if _, err := a.SetModel("   "); err == nil {
		t.Fatal("SetModel con string vacío debería fallar")
	}
	if _, err := a.SetModel("mock-2"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if a.Model() != "mock-2" {
		t.Fatalf("SetModel no cambió el modelo: %q", a.Model())
	}
}

func TestStartupSummary(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	// projID absoluto (sin remote de git) -> se muestra sólo el nombre de carpeta
	abs := a.StartupSummary(StartupInfo{
		RulesLabel: "sin reglas", Skills: 3, MCPTools: 0, Hooks: 2, LSPServers: 1,
		ProjID: "/home/user/proyecto-x",
	})
	for _, want := range []string{"proveedor mock", "modo normal", "skills 3", "hooks 2", "lsp 1", "memoria 0", "[proyecto-x]"} {
		if !strings.Contains(abs, want) {
			t.Errorf("summary sin %q:\n%s", want, abs)
		}
	}

	// projID con remote -> va tal cual
	rem := a.StartupSummary(StartupInfo{ProjID: "owner/repo"})
	if !strings.Contains(rem, "[owner/repo]") {
		t.Errorf("un projID con remote debería mostrarse verbatim:\n%s", rem)
	}
}

func TestModeAddendum(t *testing.T) {
	if s := modeAddendum(ModeNormal); s != "" {
		t.Fatalf("modo normal no debería agregar nada, dio %q", s)
	}
	if s := modeAddendum(ModePlan); !strings.Contains(s, "PLAN") {
		t.Fatalf("modo plan sin addendum: %q", s)
	}
	if s := modeAddendum(ModeAuto); !strings.Contains(s, "AUTO") {
		t.Fatalf("modo auto sin addendum: %q", s)
	}
}

func TestParseMode(t *testing.T) {
	cases := map[string]string{
		"": ModeNormal, "normal": ModeNormal, "NORMAL": ModeNormal,
		"auto": ModeAuto, "bypass": ModeAuto, "yolo": ModeAuto, " Auto ": ModeAuto,
		"plan": ModePlan,
	}
	for in, want := range cases {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v; quiero %q", in, got, err, want)
		}
	}
	if _, err := ParseMode("marciano"); err == nil {
		t.Fatal("esperaba error con un modo inválido")
	}
}

func TestHumanCount(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1500, "1.5k"},
		{999_999, "1000.0k"},
		{2_000_000, "2.0M"},
	}
	for _, c := range cases {
		if got := humanCount(c.n); got != c.want {
			t.Errorf("humanCount(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestProviderFromConfig(t *testing.T) {
	t.Run("provider vacío = anthropic con su modelo default", func(t *testing.T) {
		p, name, err := ProviderFromConfig(config.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if name != "anthropic" || p.Model() != provider.DefaultAnthropicModel {
			t.Fatalf("name=%q model=%q", name, p.Model())
		}
	})

	t.Run("deepseek respeta el modelo del config", func(t *testing.T) {
		p, name, err := ProviderFromConfig(config.Config{Provider: "deepseek", Model: "deepseek-reasoner"})
		if err != nil || name != "deepseek" || p.Model() != "deepseek-reasoner" {
			t.Fatalf("name=%q model=%q err=%v", name, p.Model(), err)
		}
	})

	t.Run("kimi sin modelo usa un default no vacío", func(t *testing.T) {
		p, _, err := ProviderFromConfig(config.Config{Provider: "kimi"})
		if err != nil || p.Model() == "" {
			t.Fatalf("model=%q err=%v", p.Model(), err)
		}
	})

	t.Run("nvidia sin modelo usa un default no vacío", func(t *testing.T) {
		p, name, err := ProviderFromConfig(config.Config{Provider: "nvidia"})
		if err != nil || name != "nvidia" || p.Model() == "" {
			t.Fatalf("name=%q model=%q err=%v", name, p.Model(), err)
		}
	})

	t.Run("opencode sin modelo usa un default no vacío", func(t *testing.T) {
		p, name, err := ProviderFromConfig(config.Config{Provider: "opencode"})
		if err != nil || name != "opencode" || p.Model() == "" {
			t.Fatalf("name=%q model=%q err=%v", name, p.Model(), err)
		}
	})

	t.Run("provider desconocido es error", func(t *testing.T) {
		if _, _, err := ProviderFromConfig(config.Config{Provider: "gemini"}); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestMergeEnvKeys(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-from-env")
	base := config.Config{Provider: "deepseek"}
	got := MergeEnvKeys(base)

	if got.Keys["deepseek"] != "sk-from-env" {
		t.Fatalf("no tomó la key del env: %+v", got.Keys)
	}
	if base.Keys != nil {
		t.Fatal("MergeEnvKeys mutó el config original")
	}
}

func TestBuildBaseToolsHasEveryCapability(t *testing.T) {
	mem, err := memory.NewFileStore(t.TempDir()+"/mem.json", "test/proj")
	if err != nil {
		t.Fatal(err)
	}
	reg := BuildBaseTools(BaseToolDeps{
		Todos:  todo.NewStore(),
		LSPMgr: lsp.NewManager(lsp.Config{}, t.TempDir()),
		Skills: skill.NewRegistry(),
		Mem:    mem,
	})

	want := []string{
		"bash", "grep", "glob", "read_file", "write_file", "edit_file",
		"todo_write", "lsp", "skill", "remember", "recall",
	}
	got := map[string]bool{}
	for _, tl := range reg.All() {
		got[tl.Name()] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("falta la tool %q en el pool base", w)
		}
	}
	if n := len(reg.All()); n != len(want) {
		t.Errorf("el pool base tiene %d tools, esperaba %d (%v)", n, len(want), want)
	}
	if _, ok := reg.Get("delegate"); ok {
		t.Error("delegate no debería estar en el pool base (los subagentes no recursan)")
	}
}
