package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/andresmjimenez/arnes/internal/approval"
	"github.com/andresmjimenez/arnes/internal/provider"
)

type fakeConv struct {
	reply string
	err   error
	seen  []string
}

func (f *fakeConv) Run(_ context.Context, in string) (string, error) {
	f.seen = append(f.seen, in)
	return f.reply, f.err
}

// plain is the raw text of every entry plus any in-flight streamed text.
func plain(m Model) string {
	parts := make([]string, 0, len(m.entries)+1)
	for _, e := range m.entries {
		parts = append(parts, e.text)
	}
	if m.live != "" {
		parts = append(parts, m.live)
	}
	return strings.Join(parts, "\n")
}

func newModel(t *testing.T, conv *fakeConv) (Model, chan approval.Request) {
	t.Helper()
	appr := make(chan approval.Request)
	m := New(Config{
		Conv:      conv,
		Provider:  provider.NewMock(),
		SessionID: func() string { return "sess-1" },
		Approvals: appr,
		Theme:     DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return tm.(Model), appr
}

func TestModelSubmitStartsAgentTurn(t *testing.T) {
	conv := &fakeConv{reply: "respuesta del modelo"}
	m, _ := newModel(t, conv)

	m.ta.SetValue("hola qué tal")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)

	if !m.busy {
		t.Fatal("no se puso busy tras Enter")
	}
	if !strings.Contains(plain(m), "hola qué tal") {
		t.Fatalf("el input no quedó en el transcript:\n%s", plain(m))
	}

	select {
	case res := <-m.results:
		tm, _ = m.Update(res)
		m = tm.(Model)
	case <-time.After(time.Second):
		t.Fatal("la goroutine del agente no respondió")
	}

	if m.busy {
		t.Fatal("sigue busy después del resultado")
	}
	if !strings.Contains(plain(m), "respuesta del modelo") {
		t.Fatalf("la respuesta no se renderizó:\n%s", plain(m))
	}
	if len(conv.seen) != 1 || conv.seen[0] != "hola qué tal" {
		t.Fatalf("conv.Run recibió %v", conv.seen)
	}
}

func TestModelSlashExitQuits(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	m.ta.SetValue("/exit")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("esperaba un comando (Quit)")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("no fue Quit: %T", cmd())
	}
}

func TestModelSlashHelpDoesNotHitAgent(t *testing.T) {
	conv := &fakeConv{}
	m, _ := newModel(t, conv)
	m.ta.SetValue("/help")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)

	if len(conv.seen) != 0 {
		t.Fatalf("/help llegó al agente: %v", conv.seen)
	}
	if !strings.Contains(plain(m), "/model") {
		t.Fatalf("/help no se renderizó:\n%s", plain(m))
	}
	if m.busy {
		t.Fatal("un slash command no debería poner busy")
	}
}

func TestModelApprovalFlow(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})

	req := approval.Request{
		Call:     provider.ToolCall{Name: "bash", Input: []byte(`{"command":"ls"}`)},
		Response: make(chan bool, 1),
	}
	tm, _ := m.Update(approvalMsg(req))
	m = tm.(Model)
	if m.pending == nil {
		t.Fatal("no quedó una aprobación pendiente")
	}
	if !strings.Contains(m.View(), "bash") {
		t.Fatalf("no se mostró el prompt de aprobación:\n%s", m.View())
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = tm.(Model)
	if m.pending != nil {
		t.Fatal("la aprobación sigue pendiente tras responder 'y'")
	}
	if !strings.Contains(plain(m), "bash permitido") {
		t.Fatalf("no quedó registro de la aprobación:\n%s", plain(m))
	}
	select {
	case ok := <-req.Response:
		if !ok {
			t.Fatal("se respondió 'y' pero llegó false")
		}
	default:
		t.Fatal("no se envió la respuesta al agente")
	}
}

func TestModelApprovalDeny(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	req := approval.Request{Call: provider.ToolCall{Name: "rm"}, Response: make(chan bool, 1)}
	tm, _ := m.Update(approvalMsg(req))
	m = tm.(Model)

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = tm.(Model)
	if m.pending != nil {
		t.Fatal("sigue pendiente")
	}
	if got := <-req.Response; got {
		t.Fatal("se respondió 'n' pero llegó true")
	}
}

func TestRedactConnect(t *testing.T) {
	if got := redactConnect("/connect anthropic claude-opus-5 sk-ant-supersecret"); strings.Contains(got, "supersecret") {
		t.Fatalf("no ocultó la key: %q", got)
	}
	if got := redactConnect("/connect anthropic claude-opus-5"); got != "/connect anthropic claude-opus-5" {
		t.Fatalf("cambió una línea sin key: %q", got)
	}
	if got := redactConnect("/help"); got != "/help" {
		t.Fatalf("tocó un comando que no es /connect: %q", got)
	}
}

func TestModelViewHasStatusBar(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	view := m.View()
	if !strings.Contains(view, "sess-1") {
		t.Fatalf("la status bar no muestra la sesión:\n%s", view)
	}
}

func TestModelStatusBarShowsContextTokens(t *testing.T) {
	appr := make(chan approval.Request)
	m := New(Config{
		Conv:      &fakeConv{},
		Provider:  provider.NewMock(),
		SessionID: func() string { return "s" },
		Stats:     func() int { return 12345 },
		Approvals: appr,
		Theme:     DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = tm.(Model)
	if !strings.Contains(m.View(), "ctx") {
		t.Fatalf("no muestra el contador de contexto:\n%s", m.View())
	}
}

func TestModelStatusBarShowsCost(t *testing.T) {
	appr := make(chan approval.Request)
	m := New(Config{
		Conv:      &fakeConv{},
		Provider:  provider.NewMock(),
		SessionID: func() string { return "s" },
		Cost:      func() string { return "$0.0421" },
		Approvals: appr,
		Theme:     DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = tm.(Model)
	if !strings.Contains(m.View(), "$0.0421") {
		t.Fatalf("la status bar no muestra el costo:\n%s", m.View())
	}
}

func TestModelStatusBarHidesEmptyCost(t *testing.T) {
	appr := make(chan approval.Request)
	m := New(Config{
		Conv: &fakeConv{}, Provider: provider.NewMock(),
		SessionID: func() string { return "s" },
		Cost:      func() string { return "" }, // modelo sin tarifa
		Approvals: appr, Theme: DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = tm.(Model)
	if strings.Contains(m.View(), "$") {
		t.Fatalf("no debería mostrar '$' con costo vacío:\n%s", m.View())
	}
}

func TestModelScrollKeys(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	for i := 0; i < 40; i++ {
		m.add(kindInfo, "línea de relleno número "+strings.Repeat("x", 5))
	}
	m.setContent(true) // al fondo
	if !m.vp.AtBottom() {
		t.Fatal("debería arrancar al fondo")
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = tm.(Model)
	if m.vp.AtBottom() {
		t.Fatal("PgUp no scrolleó hacia arriba")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = tm.(Model)
	if !m.vp.AtBottom() {
		t.Fatal("End no volvió al fondo")
	}
}

func newStreamingModel(t *testing.T, conv *fakeConv) Model {
	t.Helper()
	m := New(Config{
		Conv:      conv,
		Provider:  provider.NewMock(),
		SessionID: func() string { return "s" },
		Approvals: make(chan approval.Request),
		Deltas:    make(chan string, 16),
		Theme:     DefaultTheme(),
	})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return tm.(Model)
}

func TestModelStreamingRendersDeltasLive(t *testing.T) {
	m := newStreamingModel(t, &fakeConv{reply: "hola mundo"})

	m.ta.SetValue("decime algo")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)

	for _, part := range []string{"hola", " ", "mundo"} {
		tm, _ = m.Update(deltaMsg(part))
		m = tm.(Model)
	}
	if m.live != "hola mundo" {
		t.Fatalf("live = %q", m.live)
	}
	if !strings.Contains(m.renderTranscript(), "hola mundo") {
		t.Fatalf("el texto en vuelo no aparece en el viewport:\n%s", m.renderTranscript())
	}

	tm, _ = m.Update(runResult{text: "hola mundo"})
	m = tm.(Model)
	if m.live != "" {
		t.Fatalf("live debería estar limpio, quedó %q", m.live)
	}
	if n := strings.Count(plain(m), "hola mundo"); n != 1 {
		t.Fatalf("el texto aparece %d veces (se duplicó):\n%s", n, plain(m))
	}
}

func TestModelLateDeltaAfterResultIsIgnored(t *testing.T) {
	m := newStreamingModel(t, &fakeConv{reply: "hola mundo"})
	m.ta.SetValue("hola")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)

	// llega parte del stream, después el resultado, y DESPUÉS un chunk tardío
	tm, _ = m.Update(deltaMsg("hola mu"))
	m = tm.(Model)
	tm, _ = m.Update(runResult{text: "hola mundo"})
	m = tm.(Model)
	tm, _ = m.Update(deltaMsg("ndo"))
	m = tm.(Model)

	if m.live != "" {
		t.Fatalf("un delta tardío se coló en live: %q", m.live)
	}
	if n := strings.Count(plain(m), "hola mundo"); n != 1 {
		t.Fatalf("el mensaje aparece %d veces o está partido:\n%s", n, plain(m))
	}
	if strings.Contains(plain(m), "hola mu\n") {
		t.Fatalf("texto partido:\n%s", plain(m))
	}
}

// fakeModeConv implements Conversation + command.Modes.
type fakeModeConv struct {
	fakeConv
	mode string
}

func (f *fakeModeConv) Mode() string { return f.mode }
func (f *fakeModeConv) SetMode(name string) (string, error) {
	f.mode = name
	return "modo: " + name, nil
}

func TestModelShiftTabCyclesMode(t *testing.T) {
	c := &fakeModeConv{mode: "normal"}
	appr := make(chan approval.Request)
	m := New(Config{Conv: c, Provider: provider.NewMock(), SessionID: func() string { return "s" },
		Approvals: appr, Theme: DefaultTheme()})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	m = tm.(Model)

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = tm.(Model)
	if c.mode != "auto" {
		t.Fatalf("normal → %q, quería auto", c.mode)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = tm.(Model)
	if c.mode != "plan" {
		t.Fatalf("auto → %q, quería plan", c.mode)
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = tm.(Model)
	if c.mode != "normal" {
		t.Fatalf("plan → %q, quería normal", c.mode)
	}
	if !strings.Contains(m.View(), "plan") && !strings.Contains(plain(m), "modo:") {
		t.Fatalf("no se registró el cambio de modo:\n%s", plain(m))
	}
}

func TestModelAssistantEntryIsMarkdownRendered(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	raw := "# Título\n\ntexto de una lista:\n\n- uno\n- dos\n"
	m.add(kindAssistant, raw)

	last := m.entries[len(m.entries)-1]
	if last.rendered == "" {
		t.Fatal("la entry assistant no quedó pre-renderizada")
	}
	if last.rendered == raw {
		t.Fatalf("glamour no transformó el markdown:\n%s", last.rendered)
	}
	// las entries que no son del asistente NO pasan por glamour
	m.add(kindInfo, "# esto no es markdown")
	if info := m.entries[len(m.entries)-1]; info.rendered != "" {
		t.Fatalf("una entry info no debería pre-renderizarse: %q", info.rendered)
	}
}

func TestModelUpDownScrollsWhenInputEmpty(t *testing.T) {
	m, _ := newModel(t, &fakeConv{})
	for i := 0; i < 60; i++ {
		m.add(kindInfo, "relleno")
	}
	m.setContent(true)
	if !m.vp.AtBottom() {
		t.Fatal("debería arrancar al fondo")
	}

	// input vacío → ↑ scrollea
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = tm.(Model)
	if m.vp.AtBottom() {
		t.Fatal("↑ con input vacío no scrolleó")
	}

	// con texto en el input, ↑ NO scrollea (mueve el cursor)
	m.vp.GotoBottom()
	m.ta.SetValue("hola")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = tm.(Model)
	if !m.vp.AtBottom() {
		t.Fatal("↑ con texto en el input no debería scrollear el viewport")
	}
}

// blockingConv blocks in Run until its context is cancelled.
type blockingConv struct{ started chan struct{} }

func (c *blockingConv) Run(ctx context.Context, _ string) (string, error) {
	close(c.started)
	<-ctx.Done()
	return "", ctx.Err()
}

func TestModelEscInterruptsTurn(t *testing.T) {
	c := &blockingConv{started: make(chan struct{})}
	appr := make(chan approval.Request)
	m := New(Config{Conv: c, Provider: provider.NewMock(),
		SessionID: func() string { return "s" }, Approvals: appr, Theme: DefaultTheme()})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = tm.(Model)

	m.ta.SetValue("algo largo")
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if !m.busy || m.cancel == nil {
		t.Fatal("no arrancó el turno cancelable")
	}

	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("la goroutine del agente no arrancó")
	}

	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)

	select {
	case res := <-m.results:
		if !errors.Is(res.err, context.Canceled) {
			t.Fatalf("err = %v, quería context.Canceled", res.err)
		}
		tm, _ = m.Update(res)
		m = tm.(Model)
	case <-time.After(time.Second):
		t.Fatal("Esc no canceló el turno")
	}

	if m.busy || m.cancel != nil {
		t.Fatal("quedó en estado busy tras cancelar")
	}
	if !strings.Contains(plain(m), "interrumpido") {
		t.Fatalf("no se mostró el aviso de interrupción:\n%s", plain(m))
	}
}

func TestModelRunResultAppendsWhenNoStreaming(t *testing.T) {
	m, _ := newModel(t, &fakeConv{reply: "respuesta completa"})
	m.ta.SetValue("hola")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)

	tm, _ = m.Update(runResult{text: "respuesta completa"})
	m = tm.(Model)
	if !strings.Contains(plain(m), "respuesta completa") {
		t.Fatalf("no agregó la respuesta:\n%s", plain(m))
	}
}
