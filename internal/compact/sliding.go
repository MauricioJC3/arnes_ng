package compact

import (
	"context"

	"github.com/andresmjimenez/arnes/internal/provider"
)

// DefaultKeep is how many recent messages SlidingWindow keeps when Keep is unset.
const DefaultKeep = 20

// SlidingWindow drops the oldest messages, keeping roughly the most recent Keep.
// The kept window is snapped forward to a clean user turn so no tool_result is
// left without its tool_use. It is cheap but loses old context outright.
type SlidingWindow struct {
	Keep int
}

func (SlidingWindow) Name() string { return "sliding-window" }

func (s SlidingWindow) Compact(_ context.Context, history []provider.Message) ([]provider.Message, error) {
	keep := s.Keep
	if keep <= 0 {
		keep = DefaultKeep
	}
	if len(history) <= keep {
		return history, nil
	}
	start := cleanBoundary(history, len(history)-keep)
	if start >= len(history) {
		return history, nil // no safe cut point; leave it alone
	}
	return append([]provider.Message(nil), history[start:]...), nil
}
