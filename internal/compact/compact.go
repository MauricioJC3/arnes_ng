// Package compact holds pluggable strategies for shrinking a conversation
// history before it is sent to the model, so long sessions don't overflow the
// context window.
package compact

import (
	"context"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// Strategy shrinks a history. Implementations MUST keep the result valid: a
// tool_use turn is never separated from its matching tool_result turn, and the
// first kept turn is never an orphan tool_result.
type Strategy interface {
	Name() string
	Compact(ctx context.Context, history []provider.Message) ([]provider.Message, error)
}

// EstimateTokens is a cheap ~4-characters-per-token approximation of how much
// context a history occupies. Good enough to decide when to compact.
func EstimateTokens(history []provider.Message) int {
	chars := 0
	for _, m := range history {
		chars += len(m.Text)
		for _, tc := range m.ToolCalls {
			chars += len(tc.Name) + len(tc.Input)
		}
		for _, tr := range m.ToolResults {
			chars += len(tr.Content)
		}
	}
	return chars / 4
}

// cleanBoundary returns the index of the first message at or after `from` that
// starts a fresh user turn (plain text, not tool results), or len(history) when
// there is none. Cutting the history there never orphans a tool_result.
func cleanBoundary(history []provider.Message, from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(history); i++ {
		m := history[i]
		if m.Role == provider.RoleUser && len(m.ToolResults) == 0 {
			return i
		}
	}
	return len(history)
}
