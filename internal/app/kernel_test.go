package app

import (
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/checkpoint"
	"github.com/MauricioJC3/arnes_ng/internal/compact"
	"github.com/MauricioJC3/arnes_ng/internal/config"
	"github.com/MauricioJC3/arnes_ng/internal/hook"
	"github.com/MauricioJC3/arnes_ng/internal/memory"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/session"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
)

// TestNewMapsEveryDep guards the Deps -> App field mapping: if a field is added
// to Deps and forgotten in New, this fails.
func TestNewMapsEveryDep(t *testing.T) {
	prov := provider.NewMock()
	store, err := session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mem, err := memory.NewFileStore(t.TempDir()+"/m.json", "p")
	if err != nil {
		t.Fatal(err)
	}
	appr := approval.AllowAll{}
	cps := checkpoint.NewStore()
	subs := subagent.NewRegistry()
	deltas := make(chan string, 1)
	comp := compact.SlidingWindow{}
	hooks := hook.New(hook.Config{}, 0)

	a := New(Deps{
		ProviderName:  "mock",
		Provider:      prov,
		Cfg:           config.Config{Provider: "mock"},
		CfgPath:       "/tmp/cfg.json",
		Store:         store,
		BaseApprover:  appr,
		Mode:          ModePlan,
		AutoCompactor: comp,
		CompactAt:     42,
		Streaming:     true,
		Deltas:        deltas,
		Hooks:         hooks,
		Checkpoints:   cps,
		Mem:           mem,
		Rules:         "\n\nREGLAS",
		Subagents:     subs,
		Version:       "9.9.9",
		Repo:          "owner/repo",
	})

	checks := []struct {
		name string
		ok   bool
	}{
		{"providerName", a.providerName == "mock"},
		{"prov", a.prov == prov},
		{"cfg", a.cfg.Provider == "mock"},
		{"cfgPath", a.cfgPath == "/tmp/cfg.json"},
		{"store", a.store == store},
		{"baseApprover", a.baseApprover == approval.Approver(appr)},
		{"mode", a.mode == ModePlan},
		{"autoCompactor", a.autoCompactor == compact.Strategy(comp)},
		{"compactAt", a.compactAt == 42},
		{"streaming", a.streaming},
		{"deltas", a.deltas != nil},
		{"hooks", a.hooks != nil},
		{"checkpoints", a.checkpoints == cps},
		{"mem", a.mem != nil},
		{"rules", a.rules == "\n\nREGLAS"},
		{"subagents", a.subagents == subs},
		{"version", a.version == "9.9.9"},
		{"repo", a.repo == "owner/repo"},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("New no mapeó el campo %q", c.name)
		}
	}
}

// TestRebuildSwapsAgentAndPreservesUsage: any mutating use case that calls
// rebuild must give a fresh agent + persister while keeping the running cost.
func TestRebuildSwapsAgentAndPreservesUsage(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	a.usedIn, a.usedOut = 700, 90
	agBefore, convBefore := a.ag, a.conv

	if _, err := a.SetMode("auto"); err != nil { // a use case that calls rebuild
		t.Fatal(err)
	}

	if a.ag == agBefore {
		t.Error("rebuild no reemplazó el agente")
	}
	if a.conv == convBefore {
		t.Error("rebuild no reemplazó el persister")
	}
	if in, out := a.SessionUsage(); in != 700 || out != 90 {
		t.Errorf("rebuild reseteó el costo: %d/%d, esperaba 700/90", in, out)
	}
}

// TestAgentOptionsGating: the shared option set grows only with what is
// actually configured.
func TestAgentOptionsGating(t *testing.T) {
	bare := &App{} // no hooks, no checkpoints, no streaming, no maxSteps
	if n := len(bare.agentOptions()); n != 2 {
		t.Fatalf("sin nada configurado esperaba 2 opciones (system, warn), hubo %d", n)
	}

	full := &App{
		hooks:       hook.New(hook.Config{}, 0),
		checkpoints: checkpoint.NewStore(),
		streaming:   true,
		deltas:      make(chan string, 1),
		maxSteps:    50,
		maxTokens:   8192,
	}
	if n := len(full.agentOptions()); n != 8 {
		t.Fatalf("con hooks+checkpoints+streaming+deltas+maxSteps+maxTokens esperaba 8 opciones, hubo %d", n)
	}

	noDelta := &App{streaming: true} // streaming on but no delta channel
	if n := len(noDelta.agentOptions()); n != 3 {
		t.Fatalf("streaming sin canal de deltas esperaba 3 opciones, hubo %d", n)
	}
}

// TestBuildSystemComposition: base prompt + rules + mode addendum, in order,
// with no memory digest when there is no store.
func TestBuildSystemComposition(t *testing.T) {
	a := &App{rules: "\n\nREGLAS DEL PROYECTO", mode: ModePlan}
	got := a.buildSystem()

	if !strings.HasPrefix(got, systemPrompt) {
		t.Fatal("buildSystem no arranca con el prompt base")
	}
	want := systemPrompt + "\n\nREGLAS DEL PROYECTO" + modeAddendum(ModePlan)
	if got != want {
		t.Fatalf("composición inesperada:\n%q", got)
	}
	if !strings.Contains(got, "MODO PLAN ACTIVO") {
		t.Error("falta el addendum del modo plan")
	}
}

func TestBuildSystemNormalModeAddsNoAddendum(t *testing.T) {
	a := &App{mode: ModeNormal}
	if got := a.buildSystem(); got != systemPrompt {
		t.Fatalf("modo normal sin reglas ni memoria debería ser el prompt pelado, dio:\n%q", got)
	}
}

// TestFreshConversationIsIsolated: a fresh agent is a distinct instance and does
// not become the live conversation.
func TestFreshConversationIsIsolated(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	live := a.conv

	fresh := a.FreshConversation()
	if fresh == nil {
		t.Fatal("FreshConversation devolvió nil")
	}
	if a.conv != live {
		t.Error("FreshConversation no debería tocar la conversación viva")
	}
}
