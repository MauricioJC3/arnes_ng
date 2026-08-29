package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/checkpoint"
	"github.com/MauricioJC3/arnes_ng/internal/lsp"
	"github.com/MauricioJC3/arnes_ng/internal/memory"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/session"
	"github.com/MauricioJC3/arnes_ng/internal/skill"
	"github.com/MauricioJC3/arnes_ng/internal/subagent"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

// newIntegrationApp wires an App the way the composition root does: the real
// base tool pool, a real on-disk session store and project memory, the
// checkpoint store, and an auto-approving gateway. Only the provider is a mock.
func newIntegrationApp(t *testing.T, prov provider.Provider) *App {
	t.Helper()
	dir := t.TempDir()

	store, err := session.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mem, err := memory.NewFileStore(dir+"/mem.json", "test/proj")
	if err != nil {
		t.Fatal(err)
	}
	tools := BuildBaseTools(BaseToolDeps{
		Todos:  todo.NewStore(),
		LSPMgr: lsp.NewManager(lsp.Config{}, dir),
		Skills: skill.NewRegistry(),
		Mem:    mem,
	})

	return &App{
		providerName: "mock",
		prov:         prov,
		cfgPath:      dir + "/config.json",
		store:        store,
		tools:        tools,
		baseApprover: approval.AllowAll{},
		mode:         ModeNormal,
		subagents:    subagent.NewRegistry(),
		checkpoints:  checkpoint.NewStore(),
		mem:          mem,
	}
}

func mock(p provider.Provider) *provider.MockProvider { return p.(*provider.MockProvider) }

// TestIntegrationTurnRunsToolThroughTheWholeSpine drives one full turn:
// user input -> model asks for bash -> tool executes -> result goes back to the
// model -> final text. It asserts the tool ran for real, the result reached the
// model, the session was persisted, token usage synced, and a checkpoint opened.
func TestIntegrationTurnRunsToolThroughTheWholeSpine(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short: ejecuta bash de verdad")
	}

	prov := provider.NewMock(
		provider.Response{
			ToolCalls: []provider.ToolCall{
				{ID: "c1", Name: "bash", Input: json.RawMessage(`{"command":"echo spine-ok"}`)},
			},
			StopReason: provider.StopToolUse,
			Usage:      provider.Usage{InputTokens: 10, OutputTokens: 5},
		},
		provider.Response{
			Text:       "el comando corrió",
			StopReason: provider.StopEndTurn,
			Usage:      provider.Usage{InputTokens: 12, OutputTokens: 4},
		},
	)

	a := newIntegrationApp(t, prov)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	sessID := a.sess.ID

	out, err := a.Run(context.Background(), "corré un echo")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "el comando corrió" {
		t.Fatalf("texto final = %q", out)
	}

	// 1. la herramienta se ejecutó de verdad: el segundo request trae su resultado
	calls := mock(prov).Calls
	if len(calls) != 2 {
		t.Fatalf("esperaba 2 llamadas al provider (turno + resultado), hubo %d", len(calls))
	}
	var sawResult bool
	for _, m := range calls[1].Messages {
		for _, tr := range m.ToolResults {
			if strings.Contains(tr.Content, "spine-ok") {
				sawResult = true
			}
		}
	}
	if !sawResult {
		t.Fatalf("el stdout del bash no volvió al modelo:\n%+v", calls[1].Messages)
	}

	// 2. la sesión quedó persistida en disco con el historial completo del turno
	saved, err := a.store.Load(sessID)
	if err != nil {
		t.Fatalf("la sesión no se guardó: %v", err)
	}
	if len(saved.Messages) < 4 { // user, assistant+toolcall, tool result, assistant final
		t.Fatalf("historial persistido incompleto: %d mensajes\n%+v", len(saved.Messages), saved.Messages)
	}

	// 3. el uso de tokens se sincronizó desde el agente (10+12 / 5+4)
	if in, out := a.SessionUsage(); in != 22 || out != 9 {
		t.Fatalf("SessionUsage = %d/%d, esperaba 22/9", in, out)
	}

	// 4. se abrió un checkpoint al comenzar el turno
	cps := a.checkpoints.List()
	if len(cps) != 1 {
		t.Fatalf("esperaba 1 checkpoint, hay %d", len(cps))
	}
	if !strings.Contains(cps[0].Label, "corré un echo") {
		t.Fatalf("label del checkpoint = %q", cps[0].Label)
	}
}

// TestIntegrationDeniedToolIsNotExecuted checks the approval gateway is really in
// the path: in plan mode a write is refused and the model is told so.
func TestIntegrationDeniedToolIsNotExecuted(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short")
	}

	prov := provider.NewMock(
		provider.Response{
			ToolCalls: []provider.ToolCall{
				{ID: "c1", Name: "write_file", Input: json.RawMessage(`{"path":"x.txt","content":"nope"}`)},
			},
			StopReason: provider.StopToolUse,
		},
		provider.Response{Text: "ok, no escribo nada", StopReason: provider.StopEndTurn},
	)

	a := newIntegrationApp(t, prov)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetMode("plan"); err != nil {
		t.Fatal(err)
	}

	out, err := a.Run(context.Background(), "escribí un archivo")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ok, no escribo nada" {
		t.Fatalf("texto final = %q", out)
	}

	// el segundo request debe llevar un tool result marcado como error/denegado
	calls := mock(prov).Calls
	if len(calls) != 2 {
		t.Fatalf("esperaba 2 llamadas, hubo %d", len(calls))
	}
	var denied bool
	for _, m := range calls[1].Messages {
		for _, tr := range m.ToolResults {
			if tr.IsError || strings.Contains(strings.ToLower(tr.Content), "deneg") {
				denied = true
			}
		}
	}
	if !denied {
		t.Fatalf("el modo plan no denegó el write:\n%+v", calls[1].Messages)
	}
}

// TestIntegrationRewindUndoesAFileWrite runs a turn that creates a file, then
// /rewind: the file is deleted (it did not exist at the checkpoint) and the
// history is rolled back.
func TestIntegrationRewindUndoesAFileWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short")
	}

	target := filepath.Join(t.TempDir(), "nuevo.txt")
	input, _ := json.Marshal(map[string]string{"path": target, "content": "contenido v1"})

	prov := provider.NewMock(
		provider.Response{
			ToolCalls:  []provider.ToolCall{{ID: "c1", Name: "write_file", Input: input}},
			StopReason: provider.StopToolUse,
		},
		provider.Response{Text: "archivo creado", StopReason: provider.StopEndTurn},
	)

	a := newIntegrationApp(t, prov)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Run(context.Background(), "creá nuevo.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("el archivo debería existir tras el turno: %v", err)
	}
	histAfterTurn := len(a.sess.Messages)
	if histAfterTurn == 0 {
		t.Fatal("el turno no dejó historial")
	}

	msg, err := a.Rewind(1)
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("Rewind debería haber borrado el archivo, stat err = %v", err)
	}
	if len(a.sess.Messages) >= histAfterTurn {
		t.Fatalf("Rewind no truncó el historial: antes %d, ahora %d", histAfterTurn, len(a.sess.Messages))
	}
	if !strings.Contains(msg, "checkpoint 1") {
		t.Fatalf("mensaje de Rewind: %q", msg)
	}
}

// TestIntegrationFreshConversationDoesNotPersist checks /goal --fresh gets a bare
// agent whose turns never touch the live session.
func TestIntegrationFreshConversationDoesNotPersist(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short")
	}

	prov := provider.NewMock(provider.Response{Text: "listo fresh", StopReason: provider.StopEndTurn})
	a := newIntegrationApp(t, prov)
	if _, err := a.NewSession(); err != nil {
		t.Fatal(err)
	}

	fresh := a.FreshConversation()
	out, err := fresh.Run(context.Background(), "hola")
	if err != nil {
		t.Fatal(err)
	}
	if out != "listo fresh" {
		t.Fatalf("out = %q", out)
	}
	if len(a.sess.Messages) != 0 {
		t.Fatalf("FreshConversation no debería tocar la sesión viva: %d mensajes", len(a.sess.Messages))
	}
	if in, out := a.SessionUsage(); in != 0 || out != 0 {
		t.Fatalf("FreshConversation no debería sumar al uso de la sesión: %d/%d", in, out)
	}
}
