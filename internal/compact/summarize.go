package compact

import (
	"context"
	"fmt"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// DefaultRecent is how many trailing messages Summarize keeps verbatim.
const DefaultRecent = 6

const defaultSummarizePrompt = `Resumí la conversación de abajo de forma concisa pero COMPLETA.
Conservá sí o sí: decisiones tomadas, archivos creados o modificados, comandos importantes y sus
resultados, datos del proyecto (stack, convenciones, rutas) y tareas pendientes. No inventes nada.`

// Summarize asks the model to condense the older part of the conversation into
// one block, then replaces that part with the block while keeping the last
// Recent messages verbatim. It costs one model call but preserves meaning.
type Summarize struct {
	Provider provider.Provider
	Recent   int
	Prompt   string
}

func (Summarize) Name() string { return "summarize" }

func (s Summarize) Compact(ctx context.Context, history []provider.Message) ([]provider.Message, error) {
	recent := s.Recent
	if recent <= 0 {
		recent = DefaultRecent
	}
	if len(history) <= recent+2 {
		return history, nil
	}

	cut := cleanBoundary(history, len(history)-recent)
	older, tail := history[:cut], history[cut:]
	if len(older) == 0 {
		return history, nil
	}

	instruction := s.Prompt
	if instruction == "" {
		instruction = defaultSummarizePrompt
	}
	resp, err := s.Provider.SendMessage(ctx, provider.Request{
		System:    "Sos un compactador de contexto: resumís sin perder información accionable.",
		Messages:  []provider.Message{{Role: provider.RoleUser, Text: instruction + "\n\n---\n" + renderTranscript(older)}},
		MaxTokens: 2048,
	})
	if err != nil {
		return nil, fmt.Errorf("summarize: %w", err)
	}

	out := make([]provider.Message, 0, 1+len(tail))
	out = append(out, provider.Message{
		Role: provider.RoleUser,
		Text: "[RESUMEN DE LA CONVERSACIÓN PREVIA]\n" + resp.Text,
	})
	return append(out, tail...), nil
}

// renderTranscript flattens messages into a plain-text log for the summarizer.
func renderTranscript(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch {
		case len(m.ToolCalls) > 0:
			if m.Text != "" {
				fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Text)
			}
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "%s -> tool %s %s\n", m.Role, tc.Name, string(tc.Input))
			}
		case len(m.ToolResults) > 0:
			for _, tr := range m.ToolResults {
				fmt.Fprintf(&b, "tool_result: %s\n", clip(tr.Content, 600))
			}
		default:
			fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Text)
		}
	}
	return b.String()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(cortado)"
}
