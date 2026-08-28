package main

import (
	"os"
	"strings"
	"testing"

	"github.com/andresmjimenez/arnes/internal/approval"
	"github.com/andresmjimenez/arnes/internal/config"
	"github.com/andresmjimenez/arnes/internal/provider"
	"github.com/andresmjimenez/arnes/internal/session"
	"github.com/andresmjimenez/arnes/internal/subagent"
	"github.com/andresmjimenez/arnes/internal/tool"
)

func TestProviderFromConfig(t *testing.T) {
	t.Run("provider vacío = anthropic con su modelo default", func(t *testing.T) {
		p, name, err := providerFromConfig(config.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if name != "anthropic" || p.Model() != provider.DefaultAnthropicModel {
			t.Fatalf("name=%q model=%q", name, p.Model())
		}
	})

	t.Run("deepseek respeta el modelo del config", func(t *testing.T) {
		p, name, err := providerFromConfig(config.Config{Provider: "deepseek", Model: "deepseek-reasoner"})
		if err != nil || name != "deepseek" || p.Model() != "deepseek-reasoner" {
			t.Fatalf("name=%q model=%q err=%v", name, p.Model(), err)
		}
	})

	t.Run("kimi sin modelo usa un default no vacío", func(t *testing.T) {
		p, _, err := providerFromConfig(config.Config{Provider: "kimi"})
		if err != nil || p.Model() == "" {
			t.Fatalf("model=%q err=%v", p.Model(), err)
		}
	})

	t.Run("provider desconocido es error", func(t *testing.T) {
		if _, _, err := providerFromConfig(config.Config{Provider: "gemini"}); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestMergeEnvKeys(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-from-env")
	base := config.Config{Provider: "deepseek"}
	got := mergeEnvKeys(base)

	if got.Keys["deepseek"] != "sk-from-env" {
		t.Fatalf("no tomó la key del env: %+v", got.Keys)
	}
	if base.Keys != nil {
		t.Fatal("mergeEnvKeys mutó el config original")
	}
}

func TestAppConnect(t *testing.T) {
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

func newTestApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	store, err := session.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &app{
		providerName: "mock",
		prov:         provider.NewMock(),
		cfgPath:      dir + "/config.json",
		store:        store,
		tools:        tool.NewRegistry(),
		baseApprover: approval.AllowAll{},
		mode:         modeNormal,
		subagents:    subagent.NewRegistry(subagent.Defaults()...),
	}
}

func TestAppListSubagents(t *testing.T) {
	a := newTestApp(t)
	lines := a.ListSubagents()
	if len(lines) != len(subagent.Defaults()) {
		t.Fatalf("líneas = %d, quiero %d", len(lines), len(subagent.Defaults()))
	}
	if !strings.HasPrefix(lines[0], "research: ") {
		t.Errorf("primera línea = %q", lines[0])
	}
}

func TestLoadSubagentsFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sa.json"
	if err := os.WriteFile(path, []byte(`[{"name":"x","system":"s","description":"d"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARNES_SUBAGENTS", path)

	defs, err := loadSubagents()
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "x" {
		t.Fatalf("defs = %+v", defs)
	}
}

func TestAppNewAndResumeByPrefix(t *testing.T) {
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

func TestAppResumeMissing(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.ResumeSession("20990101-000000-ffff"); err == nil {
		t.Fatal("esperaba error al reanudar una sesión inexistente")
	}
}

func TestAppModes(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	if a.Mode() != modeNormal {
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
	if a.Mode() != modeAuto {
		t.Fatalf("yolo no mapeó a auto: %q", a.Mode())
	}
	if _, ok := a.effectiveApprover().(approval.AllowAll); !ok {
		t.Fatalf("modo auto no usa AllowAll: %T", a.effectiveApprover())
	}

	if _, err := a.SetMode("marciano"); err == nil {
		t.Fatal("esperaba error con un modo inválido")
	}
}

func TestAppSetStrategy(t *testing.T) {
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

func TestCompactionFromEnv(t *testing.T) {
	t.Run("off por defecto", func(t *testing.T) {
		t.Setenv("ARNES_COMPACT", "")
		s, at, err := compactionFromEnv(provider.NewMock())
		if err != nil || s != nil || at != 0 {
			t.Fatalf("s=%v at=%d err=%v", s, at, err)
		}
	})

	t.Run("sliding con umbral custom", func(t *testing.T) {
		t.Setenv("ARNES_COMPACT", "sliding")
		t.Setenv("ARNES_COMPACT_AT", "50000")
		s, at, err := compactionFromEnv(provider.NewMock())
		if err != nil {
			t.Fatal(err)
		}
		if s == nil || s.Name() != "sliding-window" || at != 50000 {
			t.Fatalf("s=%v at=%d", s, at)
		}
	})

	t.Run("umbral inválido es error", func(t *testing.T) {
		t.Setenv("ARNES_COMPACT", "summarize")
		t.Setenv("ARNES_COMPACT_AT", "muchos")
		if _, _, err := compactionFromEnv(provider.NewMock()); err == nil {
			t.Fatal("esperaba error")
		}
	})
}
