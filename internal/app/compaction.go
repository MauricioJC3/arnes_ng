package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/compact"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// SetStrategy implements command.Compaction: swap the strategy at runtime.
func (a *App) SetStrategy(name string) (string, error) {
	s, err := StrategyByName(name, a.prov)
	if err != nil {
		return "", err
	}
	a.ag.SetCompactor(s)
	return "estrategia de compactación: " + s.Name(), nil
}

// Compact implements command.Compaction: force compaction now and persist.
func (a *App) Compact() (string, error) {
	before, after, err := a.ag.Compact(context.Background())
	if err != nil {
		return "", err
	}
	a.sess.Messages = a.ag.History()
	if saveErr := a.store.Save(a.sess); saveErr != nil {
		return "", saveErr
	}
	return fmt.Sprintf("compactado con %s: ~%d → ~%d tokens", a.ag.CompactorName(), before, after), nil
}

// StrategyByName resolves a /compact argument (or ARNES_COMPACT) to a strategy.
func StrategyByName(name string, p provider.Provider) (compact.Strategy, error) {
	switch strings.ToLower(name) {
	case "none", "off":
		return compact.None{}, nil
	case "sliding", "sliding-window":
		return compact.SlidingWindow{}, nil
	case "summarize", "summary":
		return compact.Summarize{Provider: p}, nil
	default:
		return nil, fmt.Errorf("estrategia desconocida: %q (none|sliding|summarize)", name)
	}
}
