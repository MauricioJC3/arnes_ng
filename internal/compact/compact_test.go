package compact

import (
	"context"
	"strings"
	"testing"

	"github.com/andresmjimenez/arnes/internal/provider"
)

// history builds a simple alternating user/assistant history of n turns.
func history(n int) []provider.Message {
	var h []provider.Message
	for i := 0; i < n; i++ {
		h = append(h,
			provider.Message{Role: provider.RoleUser, Text: "pregunta"},
			provider.Message{Role: provider.RoleAssistant, Text: "respuesta"},
		)
	}
	return h
}

func TestNone(t *testing.T) {
	h := history(5)
	got, err := None{}.Compact(context.Background(), h)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(h) {
		t.Fatalf("None cambió el largo: %d -> %d", len(h), len(got))
	}
}

func TestSlidingWindow(t *testing.T) {
	t.Run("historial corto no se toca", func(t *testing.T) {
		h := history(3) // 6 mensajes
		got, _ := SlidingWindow{Keep: 20}.Compact(context.Background(), h)
		if len(got) != 6 {
			t.Fatalf("len = %d, quiero 6", len(got))
		}
	})

	t.Run("recorta y arranca en un turno de usuario limpio", func(t *testing.T) {
		h := history(10) // 20 mensajes
		got, _ := SlidingWindow{Keep: 5}.Compact(context.Background(), h)
		if len(got) == 0 || len(got) > 6 {
			t.Fatalf("len = %d, esperaba ~5-6", len(got))
		}
		if got[0].Role != provider.RoleUser {
			t.Fatalf("el primer mensaje conservado no es de usuario: %+v", got[0])
		}
	})

	t.Run("no deja un tool_result huérfano", func(t *testing.T) {
		h := []provider.Message{
			{Role: provider.RoleUser, Text: "hacé algo"},
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash"}}},
			{Role: provider.RoleUser, ToolResults: []provider.ToolResult{{CallID: "c1", Content: "ok"}}},
			{Role: provider.RoleAssistant, Text: "listo"},
			{Role: provider.RoleUser, Text: "otra cosa"},
			{Role: provider.RoleAssistant, Text: "hecho"},
		}
		// Keep=3 caería en el medio del intercambio de tool; debe snapear hacia adelante.
		got, _ := SlidingWindow{Keep: 3}.Compact(context.Background(), h)
		if len(got) == 0 || got[0].Role != provider.RoleUser || len(got[0].ToolResults) != 0 {
			t.Fatalf("arrancó en un punto inválido: %+v", got)
		}
	})
}

func TestSummarize(t *testing.T) {
	t.Run("reemplaza lo viejo por un resumen y conserva el tramo reciente", func(t *testing.T) {
		p := provider.NewMock(provider.Response{Text: "resumen: hablaron de X e Y", StopReason: provider.StopEndTurn})
		h := history(10) // 20 mensajes
		got, err := Summarize{Provider: p, Recent: 4}.Compact(context.Background(), h)
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Role != provider.RoleUser || !strings.Contains(got[0].Text, "RESUMEN") {
			t.Fatalf("el primer mensaje no es el bloque de resumen: %+v", got[0])
		}
		if !strings.Contains(got[0].Text, "resumen: hablaron de X e Y") {
			t.Fatalf("no incorporó la respuesta del modelo: %q", got[0].Text)
		}
		// 1 resumen + ~4 recientes, mucho menos que 20
		if len(got) >= len(h) {
			t.Fatalf("no compactó: %d -> %d", len(h), len(got))
		}
		// el summarizer recibió un transcript de lo viejo
		if len(p.Calls) != 1 || !strings.Contains(p.Calls[0].Messages[0].Text, "pregunta") {
			t.Fatalf("no se le pasó el transcript al modelo: %+v", p.Calls)
		}
	})

	t.Run("historial corto no se toca", func(t *testing.T) {
		p := provider.NewMock()
		h := history(2)
		got, _ := Summarize{Provider: p, Recent: 6}.Compact(context.Background(), h)
		if len(got) != len(h) {
			t.Fatalf("tocó un historial corto: %d -> %d", len(h), len(got))
		}
		if len(p.Calls) != 0 {
			t.Fatal("no debería haber llamado al modelo")
		}
	})

	t.Run("propaga el error del modelo", func(t *testing.T) {
		p := &provider.MockProvider{Err: errString("502")}
		h := history(10)
		if _, err := (Summarize{Provider: p}).Compact(context.Background(), h); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestEstimateTokens(t *testing.T) {
	h := []provider.Message{{Role: provider.RoleUser, Text: strings.Repeat("x", 400)}}
	if got := EstimateTokens(h); got != 100 {
		t.Fatalf("EstimateTokens = %d, quiero 100 (400 chars / 4)", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
