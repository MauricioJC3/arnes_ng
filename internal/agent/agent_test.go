package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
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

// toolFunc is a Tool whose Execute is an arbitrary func (used to make one panic).
type toolFunc struct {
	name string
	fn   func() (string, error)
}

func (t toolFunc) Name() string                                             { return t.name }
func (t toolFunc) Description() string                                      { return "tool func para tests" }
func (t toolFunc) InputSchema() map[string]any                              { return map[string]any{"type": "object"} }
func (t toolFunc) Execute(context.Context, json.RawMessage) (string, error) { return t.fn() }

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
		// Cada paso pide una llamada DISTINTA (input único), así se agota el
		// presupuesto de pasos sin que salte el guardia de repetición.
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			n++
			return provider.Response{
				Text:       "voy por acá...",
				ToolCalls:  []provider.ToolCall{{ID: "c", Name: "echo", Input: json.RawMessage(`{"n":` + strconv.Itoa(n) + `}`)}},
				StopReason: provider.StopToolUse,
			}, nil
		}
		ft := &fakeTool{name: "echo", out: "eco"}
		a := New(p, tool.NewRegistry(ft), approval.AllowAll{}, WithMaxSteps(3))

		out, err := a.Run(ctx, "loop infinito")
		var incomplete *provider.IncompleteError
		if !errors.As(err, &incomplete) || !strings.Contains(err.Error(), "me detuve tras 3 pasos") {
			t.Fatalf("esperaba un *provider.IncompleteError por límite de pasos, tengo: %v", err)
		}
		if out != "voy por acá..." {
			t.Fatalf("texto parcial = %q, quiero el último texto del modelo", out)
		}
		if ft.calls != 3 {
			t.Fatalf("ejecuciones = %d, quiero 3 (una por paso)", ft.calls)
		}
	})

	t.Run("corta el turno si el modelo repite la misma llamada", func(t *testing.T) {
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			return provider.Response{
				ToolCalls:  []provider.ToolCall{{ID: "c", Name: "echo", Input: json.RawMessage(`{"x":1}`)}},
				StopReason: provider.StopToolUse,
			}, nil
		}
		ft := &fakeTool{name: "echo", out: "eco"}
		a := New(p, tool.NewRegistry(ft), approval.AllowAll{}, WithMaxSteps(50))

		_, err := a.Run(ctx, "hacelo")
		var incomplete *provider.IncompleteError
		if !errors.As(err, &incomplete) || !strings.Contains(err.Error(), `repitió la misma llamada a "echo" 3 veces`) {
			t.Fatalf("esperaba un *provider.IncompleteError por repetición, tengo: %v", err)
		}
		if ft.calls != 2 {
			t.Fatalf("ejecuciones = %d, quiero 2 (corta antes de la 3ra idéntica)", ft.calls)
		}
	})

	t.Run("argumentos truncados: error claro al modelo, sin ejecutar la tool", func(t *testing.T) {
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			n++
			if n == 1 {
				return provider.Response{
					ToolCalls:  []provider.ToolCall{{ID: "c", Name: "echo", Input: json.RawMessage(`{"path":"a`)}},
					StopReason: provider.StopToolUse,
				}, nil
			}
			return provider.Response{Text: "ok, lo parto", StopReason: provider.StopEndTurn}, nil
		}
		ft := &fakeTool{name: "echo", out: "eco"}
		a := New(p, tool.NewRegistry(ft), approval.AllowAll{}, WithMaxSteps(5))

		if _, err := a.Run(ctx, "escribí"); err != nil {
			t.Fatal(err)
		}
		if ft.calls != 0 {
			t.Fatalf("la tool no debería haberse ejecutado con args inválidos, calls = %d", ft.calls)
		}
		// El historial: assistant(call normalizado a {}) -> user(tool result de error) -> assistant final.
		res := a.History()[2].ToolResults
		if len(res) != 1 || !res[0].IsError || !strings.Contains(res[0].Content, "mal formados") {
			t.Fatalf("resultado realimentado = %+v", res)
		}
	})

	t.Run("un pánico en la tool vuelve como resultado de error, no rompe el turno", func(t *testing.T) {
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			n++
			if n == 1 {
				return provider.Response{
					ToolCalls:  []provider.ToolCall{{ID: "c", Name: "boom", Input: json.RawMessage(`{}`)}},
					StopReason: provider.StopToolUse,
				}, nil
			}
			return provider.Response{Text: "seguí igual", StopReason: provider.StopEndTurn}, nil
		}
		boom := toolFunc{name: "boom", fn: func() (string, error) { panic("kaboom") }}
		a := New(p, tool.NewRegistry(boom), approval.AllowAll{}, WithMaxSteps(5))

		out, err := a.Run(ctx, "usá boom")
		if err != nil {
			t.Fatalf("un pánico en la tool no debería fallar el turno: %v", err)
		}
		if out != "seguí igual" {
			t.Fatalf("out = %q", out)
		}
		res := a.History()[2].ToolResults
		if len(res) != 1 || !res[0].IsError || !strings.Contains(res[0].Content, "pánico") {
			t.Fatalf("resultado realimentado = %+v", res)
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

	t.Run("una respuesta cortada por tokens: reintenta y sigue", func(t *testing.T) {
		var n int
		p := &provider.MockProvider{}
		p.Handler = func(req provider.Request) (provider.Response, error) {
			n++
			if n == 1 {
				return provider.Response{Text: "func main() {", StopReason: provider.StopMaxTokens}, nil
			}
			// El nudge del arnés quedó en el historial como turno de usuario.
			last := req.Messages[len(req.Messages)-1]
			if last.Role != provider.RoleUser || !strings.Contains(last.Text, "se cortó por el límite de tokens") {
				return provider.Response{}, errors.New("no llegó el aviso de truncado")
			}
			return provider.Response{Text: "  println(\"ok\")\n}", StopReason: provider.StopEndTurn}, nil
		}
		a := New(p, tool.NewRegistry(), approval.AllowAll{}, WithMaxSteps(5))

		out, err := a.Run(ctx, "escribí main")
		if err != nil {
			t.Fatalf("un solo corte debería recuperarse: %v", err)
		}
		if out != "  println(\"ok\")\n}" || n != 2 {
			t.Fatalf("out=%q n=%d", out, n)
		}
	})

	t.Run("cortes por tokens repetidos: se rinde con IncompleteError", func(t *testing.T) {
		p := &provider.MockProvider{}
		p.Handler = func(provider.Request) (provider.Response, error) {
			return provider.Response{Text: "sigo escribiendo...", StopReason: provider.StopMaxTokens}, nil
		}
		a := New(p, tool.NewRegistry(), approval.AllowAll{}, WithMaxSteps(20))

		out, err := a.Run(ctx, "escribí algo enorme")
		var incomplete *provider.IncompleteError
		if !errors.As(err, &incomplete) || !strings.Contains(err.Error(), "límite de tokens de salida") {
			t.Fatalf("esperaba un *provider.IncompleteError por truncado repetido, tengo: %v", err)
		}
		if out != "sigo escribiendo..." {
			t.Fatalf("debería devolver el texto parcial, tengo: %q", out)
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
