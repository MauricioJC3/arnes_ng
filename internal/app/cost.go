package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// SessionUsage returns the cumulative token usage since this session started.
// The input side is weighted for Anthropic prompt caching (see
// provider.Usage.EffectiveInputTokens), so feeding it to provider.Cost gives the
// real spend rather than undercounting the replayed cached prefix.
func (a *App) SessionUsage() (in, out int) { return a.usedIn, a.usedOut }

// CostReport implements command.Coster: the current session's spend plus a
// per-session history, with a total for the models that have a known price.
func (a *App) CostReport() (string, error) {
	metas, err := a.store.List()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "sesión actual %s · %s · %s\n", a.sess.ID, a.prov.Model(), usageStr(a.prov.Model(), a.usedIn, a.usedOut))

	if len(metas) > 0 {
		b.WriteString("\nhistorial:\n")
		var total float64
		var haveTotal bool
		for _, m := range metas {
			mark := ""
			if m.ID == a.sess.ID {
				mark = "  ← actual"
			}
			fmt.Fprintf(&b, "  %s  %-16s  %8s tok  %s%s\n",
				m.ID, m.Model, humanCount(m.UsageIn+m.UsageOut), usageStr(m.Model, m.UsageIn, m.UsageOut), mark)
			if usd, ok := provider.Cost(m.Model, m.UsageIn, m.UsageOut); ok {
				total += usd
				haveTotal = true
			}
		}
		if haveTotal {
			fmt.Fprintf(&b, "\ntotal (modelos con tarifa conocida): $%.4f\n", total)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// usageStr renders a token pair as a dollar figure, or "sin tarifa" when the
// model has no known price.
func usageStr(model string, in, out int) string {
	if usd, ok := provider.Cost(model, in, out); ok {
		return fmt.Sprintf("$%.4f", usd)
	}
	return "sin tarifa"
}

// humanCount abbreviates a token count: 1234 -> "1.2k", 2_000_000 -> "2.0M".
func humanCount(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}
