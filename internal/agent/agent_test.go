package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
)

// fakeTool is a Tool double that records how it was called.
type fakeTool struct {
	name   string
	calls  int
	lastIn json.RawMessage
	out    string
	err    error
}

func (f *fakeTool) Name() string                { return f.name }
func (f *fakeTool) Description() string         { return "herramienta falsa para tests" }
func (f *fakeTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (f *fakeTool) Execute(_ context.Context, in json.RawMessage) (string, error) {
	f.calls++
	f.lastIn = in
	return f.out, f.err
}

func newAgent(p provider.Provider, ap approval.Approver, tools ...tool.Tool) *Agent {
	return New(p, tool.NewRegistry(tools...), ap, WithMaxSteps(5))
}

func TestAgentRun(t *testing.T) {
	ctx := context.Background()

	t.Run("respuesta directa sin herramientas", func(t *testing.T) {
		p := provider.NewMock(provider.Response{Text: "hola", StopReason: provider.StopEndTurn})
		a := newAgent(p, approval.AllowAll{})

		got, err := a.Run(ctx, "buenas")
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if got != "hola" {
			t.Fatalf("texto final = %q, quiero %q", got, "hola")
		}
		if len(p.Calls) != 1 {
			t.Fatalf("llamadas al provider = %d, quiero 1", len(p.Calls))
		}
	})

	t.Run("ejecuta la herramienta aprobada y sigue hasta el texto final", func(t *testing.T) {
		ft := &fakeTool{name: "echo", out: "eco: x"}
		p := provider.NewMock(
			provider.Response{
				ToolCalls:  []provider.ToolCall{{ID: "c1", Name: "echo", Input: json.RawMessage(`{"v":"x"}`)}},
				StopReason: provider.StopToolUse,
			},
			provider.Response{Text: "listo", StopReason: provider.StopEndTurn},
		)
		a := newAgent(p, approval.AllowAll{}, ft)

		got, err := a.Run(ctx, "corré echo")
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if got != "listo" {
			t.Fatalf("texto final = %q, quiero %q", got, "listo")
		}
		if ft.calls != 1 {
			t.Fatalf("ejecuciones de la tool = %d, quiero 1", ft.calls)
		}
		if string(ft.lastIn) != `{"v":"x"}` {
			t.Fatalf("input recibido por la tool = %s", ft.lastIn)
		}
		fedBack := a.History()[len(a.History())-2] // user con los tool results, antes del assistant final
		if len(fedBack.ToolResults) != 1 || fedBack.ToolResults[0].Content != "eco: x" {
			t.Fatalf("no se realimentó el resultado de la tool: %+v", fedBack)
		}
	})

	t.Run("una herramienta denegada no se ejecuta", func(t *testing.T) {
		ft := &fakeTool{name: "rm", out: "no deberia correr"}
		p := provider.NewMock(
			provider.Response{
				ToolCalls:  []provider.ToolCall{{ID: "c1", Name: "rm", Input: json.RawMessage(`{}`)}},
				StopReason: provider.StopToolUse,
			},
			provider.Response{Text: "ok, no lo hago", StopReason: provider.StopEndTurn},
		)
		a := newAgent(p, approval.DenyAll{}, ft)

		got, err := a.Run(ctx, "borra todo")
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if ft.calls != 0 {
			t.Fatalf("la tool se ejecutó %d veces, quiero 0", ft.calls)
		}
		if got != "ok, no lo hago" {
			t.Fatalf("texto final = %q", got)
		}
		fedBack := a.History()[2]
		if len(fedBack.ToolResults) != 1 || !fedBack.ToolResults[0].IsError {
			t.Fatalf("no se realimentó la denegación como error: %+v", fedBack)
		}
		if !strings.Contains(fedBack.ToolResults[0].Content, "denegó") {
			t.Fatalf("mensaje de denegación inesperado: %q", fedBack.ToolResults[0].Content)
		}
	})

	t.Run("herramienta inexistente vuelve como error y el loop sigue", func(t *testing.T) {
		p := provider.NewMock(
			provider.Response{
				ToolCalls:  []provider.ToolCall{{ID: "c1", Name: "fantasma", Input: json.RawMessage(`{}`)}},
				StopReason: provider.StopToolUse,
			},
			provider.Response{Text: "ah, no existe", StopReason: provider.StopEndTurn},
		)
		a := newAgent(p, approval.AllowAll{})

		got, err := a.Run(ctx, "usá fantasma")
		if err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		if got != "ah, no existe" {
			t.Fatalf("texto final = %q", got)
		}
		res := a.History()[2].ToolResults
		if len(res) != 1 || !res[0].IsError {
			t.Fatalf("esperaba un tool result de error: %+v", res)
		}
	})

	t.Run("un tool call con Input vacío se normaliza a {} en el historial", func(t *testing.T) {
		p := provider.NewMock(
			provider.Response{
				ToolCalls:  []provider.ToolCall{{ID: "c1", Name: "echo", Input: json.RawMessage("")}},
				StopReason: provider.StopToolUse,
			},
			provider.Response{Text: "ok", StopReason: provider.StopEndTurn},
		)
		ft := &fakeTool{name: "echo", out: "eco"}
		a := newAgent(p, approval.AllowAll{}, ft)

		if _, err := a.Run(ctx, "andá"); err != nil {
			t.Fatal(err)
		}
		got := a.History()[1].ToolCalls
		if len(got) != 1 || string(got[0].Input) != "{}" {
			t.Fatalf("Input no se normalizó: %q", got[0].Input)
		}
	})

	t.Run("corta al llegar al límite de pasos y devuelve el texto parcial", func(t *testing.T) {
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			return provider.Response{
				Text:       "voy por acá...",
				ToolCalls:  []provider.ToolCall{{ID: "c", Name: "echo", Input: json.RawMessage(`{}`)}},
				StopReason: provider.StopToolUse,
			}, nil
		}
		ft := &fakeTool{name: "echo", out: "eco"}
		a := New(p, tool.NewRegistry(ft), approval.AllowAll{}, WithMaxSteps(3))

		out, err := a.Run(ctx, "loop infinito")
		if err == nil {
			t.Fatal("esperaba error por límite de pasos, no hubo")
		}
		if out != "voy por acá..." {
			t.Fatalf("texto parcial = %q, quiero el último texto del modelo", out)
		}
		if ft.calls != 3 {
			t.Fatalf("ejecuciones = %d, quiero 3 (una por paso)", ft.calls)
		}
	})

	t.Run("propaga el error del provider", func(t *testing.T) {
		p := &provider.MockProvider{Err: errors.New("503 del proveedor")}
		a := newAgent(p, approval.AllowAll{})

		_, err := a.Run(ctx, "hola")
		if err == nil || !strings.Contains(err.Error(), "503") {
			t.Fatalf("esperaba el error del provider propagado, tengo: %v", err)
		}
	})

	t.Run("reanuda desde un historial previo", func(t *testing.T) {
		prior := []provider.Message{
			{Role: provider.RoleUser, Text: "te dije que me llamo Andrés"},
			{Role: provider.RoleAssistant, Text: "anotado"},
		}
		p := provider.NewMock(provider.Response{Text: "seguís siendo Andrés", StopReason: provider.StopEndTurn})
		a := New(p, tool.NewRegistry(), approval.AllowAll{}, WithHistory(prior))

		if _, err := a.Run(ctx, "cómo me llamo?"); err != nil {
			t.Fatalf("error inesperado: %v", err)
		}
		sent := p.Calls[0].Messages
		if len(sent) != 3 {
			t.Fatalf("mensajes enviados = %d, quiero 3 (2 previos + 1 nuevo)", len(sent))
		}
		if sent[0].Text != "te dije que me llamo Andrés" || sent[2].Text != "cómo me llamo?" {
			t.Fatalf("historial mal reconstruido: %+v", sent)
		}
	})
}

// spyCompactor records calls and drops the history to a single marker message.
type spyCompactor struct {
	calls int
	err   error
}

func (s *spyCompactor) Name() string { return "spy" }

func (s *spyCompactor) Compact(_ context.Context, h []provider.Message) ([]provider.Message, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return []provider.Message{{Role: provider.RoleUser, Text: "[compactado]"}}, nil
}

func TestAgentAutoCompaction(t *testing.T) {
	ctx := context.Background()
	big := strings.Repeat("x", 4000) // ~1000 tokens estimados

	t.Run("compacta antes del turno cuando supera el umbral", func(t *testing.T) {
		spy := &spyCompactor{}
		p := provider.NewMock(provider.Response{Text: "ok", StopReason: provider.StopEndTurn})
		a := New(p, tool.NewRegistry(), approval.AllowAll{},
			WithHistory([]provider.Message{{Role: provider.RoleUser, Text: big}}),
			WithCompactor(spy), WithCompactThreshold(500))

		if _, err := a.Run(ctx, "seguimos"); err != nil {
			t.Fatal(err)
		}
		if spy.calls != 1 {
			t.Fatalf("compactor llamado %d veces, quiero 1", spy.calls)
		}
		// el provider recibió el historial compactado + el mensaje nuevo
		sent := p.Calls[0].Messages
		if len(sent) != 2 || sent[0].Text != "[compactado]" {
			t.Fatalf("no usó el historial compactado: %+v", sent)
		}
	})

	t.Run("no compacta si está por debajo del umbral", func(t *testing.T) {
		spy := &spyCompactor{}
		p := provider.NewMock(provider.Response{Text: "ok", StopReason: provider.StopEndTurn})
		a := New(p, tool.NewRegistry(), approval.AllowAll{},
			WithCompactor(spy), WithCompactThreshold(500))

		if _, err := a.Run(ctx, "hola"); err != nil {
			t.Fatal(err)
		}
		if spy.calls != 0 {
			t.Fatalf("compactó de más: %d llamadas", spy.calls)
		}
	})

	t.Run("un fallo de compactación es no-fatal y avisa", func(t *testing.T) {
		spy := &spyCompactor{err: errors.New("modelo caído")}
		var warned error
		p := provider.NewMock(provider.Response{Text: "ok", StopReason: provider.StopEndTurn})
		a := New(p, tool.NewRegistry(), approval.AllowAll{},
			WithHistory([]provider.Message{{Role: provider.RoleUser, Text: big}}),
			WithCompactor(spy), WithCompactThreshold(500),
			WithWarnFn(func(err error) { warned = err }))

		out, err := a.Run(ctx, "seguimos")
		if err != nil || out != "ok" {
			t.Fatalf("el turno debería completarse igual: out=%q err=%v", out, err)
		}
		if warned == nil || !strings.Contains(warned.Error(), "modelo caído") {
			t.Fatalf("no avisó del fallo: %v", warned)
		}
		// siguió con el historial completo (2 previos + nuevo = pero era 1 previo) -> 2
		if len(p.Calls[0].Messages) != 2 {
			t.Fatalf("no siguió con el historial completo: %+v", p.Calls[0].Messages)
		}
	})

	_ = big
}

// noStreamProvider implements Provider but NOT Streamer.
type noStreamProvider struct{ text string }

func (noStreamProvider) Model() string   { return "no-stream" }
func (noStreamProvider) SetModel(string) {}
func (p noStreamProvider) SendMessage(context.Context, provider.Request) (provider.Response, error) {
	return provider.Response{Text: p.text, StopReason: provider.StopEndTurn}, nil
}

func TestAgentStreaming(t *testing.T) {
	ctx := context.Background()

	t.Run("con streaming, emite deltas que concatenan la respuesta", func(t *testing.T) {
		p := provider.NewMock(provider.Response{Text: "hola mundo desde el modelo", StopReason: provider.StopEndTurn})
		var got string
		a := New(p, tool.NewRegistry(), approval.AllowAll{},
			WithStreaming(true), WithDeltaFn(func(s string) { got += s }))

		out, err := a.Run(ctx, "decime algo")
		if err != nil {
			t.Fatal(err)
		}
		if got != "hola mundo desde el modelo" || out != got {
			t.Fatalf("deltas=%q out=%q", got, out)
		}
	})

	t.Run("sin streaming, no emite deltas aunque el provider lo soporte", func(t *testing.T) {
		p := provider.NewMock(provider.Response{Text: "silencio", StopReason: provider.StopEndTurn})
		called := false
		a := New(p, tool.NewRegistry(), approval.AllowAll{},
			WithDeltaFn(func(string) { called = true })) // sin WithStreaming

		if _, err := a.Run(ctx, "x"); err != nil {
			t.Fatal(err)
		}
		if called {
			t.Fatal("no debería haber emitido deltas")
		}
	})

	t.Run("Usage acumula tokens de todas las llamadas", func(t *testing.T) {
		p := provider.NewMock(
			provider.Response{
				ToolCalls:  []provider.ToolCall{{ID: "c", Name: "echo", Input: []byte(`{}`)}},
				StopReason: provider.StopToolUse,
				Usage:      provider.Usage{InputTokens: 100, OutputTokens: 10},
			},
			provider.Response{
				Text:       "listo",
				StopReason: provider.StopEndTurn,
				Usage:      provider.Usage{InputTokens: 130, OutputTokens: 5},
			},
		)
		ft := &fakeTool{name: "echo"}
		a := New(p, tool.NewRegistry(ft), approval.AllowAll{})
		if _, err := a.Run(ctx, "dale"); err != nil {
			t.Fatal(err)
		}
		in, out := a.Usage()
		if in != 230 || out != 15 {
			t.Fatalf("Usage = %d/%d, quiero 230/15", in, out)
		}
	})

	t.Run("Usage pondera los tokens de caché de Anthropic", func(t *testing.T) {
		p := provider.NewMock(provider.Response{
			Text:       "listo",
			StopReason: provider.StopEndTurn,
			// 50 fresco + 20000 cache-read (0.1x = 2000) + 800 cache-write (1.25x = 1000) = 3050
			Usage: provider.Usage{
				InputTokens:              50,
				OutputTokens:             7,
				CacheReadInputTokens:     20_000,
				CacheCreationInputTokens: 800,
			},
		})
		a := New(p, tool.NewRegistry(), approval.AllowAll{})
		if _, err := a.Run(ctx, "dale"); err != nil {
			t.Fatal(err)
		}
		if in, out := a.Usage(); in != 3050 || out != 7 {
			t.Fatalf("Usage con caché = %d/%d, quiero 3050/7", in, out)
		}
	})

	t.Run("provider que no es Streamer cae a SendMessage", func(t *testing.T) {
		a := New(noStreamProvider{text: "respuesta directa"}, tool.NewRegistry(), approval.AllowAll{},
			WithStreaming(true), WithDeltaFn(func(string) { t.Fatal("no debería emitir deltas") }))

		out, err := a.Run(ctx, "x")
		if err != nil || out != "respuesta directa" {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})
}

func TestAgentCompactForce(t *testing.T) {
	ctx := context.Background()

	t.Run("Compact() fuerza sin importar el umbral", func(t *testing.T) {
		spy := &spyCompactor{}
		a := New(provider.NewMock(), tool.NewRegistry(), approval.AllowAll{},
			WithHistory([]provider.Message{{Role: provider.RoleUser, Text: "corto"}}),
			WithCompactor(spy)) // sin umbral

		before, after, err := a.Compact(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if spy.calls != 1 {
			t.Fatalf("Compact() no llamó al compactor")
		}
		if after >= before && before != 0 {
			t.Logf("before=%d after=%d", before, after)
		}
		if a.CompactorName() != "spy" {
			t.Errorf("CompactorName = %q", a.CompactorName())
		}
	})
}
