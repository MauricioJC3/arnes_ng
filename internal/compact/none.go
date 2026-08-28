package compact

import (
	"context"

	"github.com/andresmjimenez/arnes/internal/provider"
)

// None leaves the history untouched. It is the default strategy.
type None struct{}

func (None) Name() string { return "none" }

func (None) Compact(_ context.Context, history []provider.Message) ([]provider.Message, error) {
	return history, nil
}
