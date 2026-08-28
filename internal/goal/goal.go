// Package goal runs a "Ralph loop": it re-sends the same goal-oriented prompt to
// the agent, iteration after iteration, until the model signals completion, the
// iteration limit is hit, progress stalls, or the context is cancelled.
package goal

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sentinel is the line the model must output (on its own line) to end the loop.
const Sentinel = "ARNES_GOAL_DONE"

// DefaultMaxIterations bounds the loop when Config.MaxIterations is unset.
const DefaultMaxIterations = 15

// Conversation is one turn in, final text out.
type Conversation interface {
	Run(ctx context.Context, input string) (string, error)
}

// Config tunes a goal run.
type Config struct {
	MaxIterations int
	// Progress is called before each iteration with (n, max).
	Progress func(n, max int)
}

// Report is the outcome of a goal run.
type Report struct {
	Iterations int
	Reason     string // completado | límite | sin progreso | cancelado | error
	LastText   string
}

// Run drives conv toward goal.
func Run(ctx context.Context, conv Conversation, goal string, cfg Config) (Report, error) {
	max := cfg.MaxIterations
	if max <= 0 {
		max = DefaultMaxIterations
	}
	prompt := buildPrompt(goal)

	var last string
	for i := 1; i <= max; i++ {
		if ctx.Err() != nil {
			return Report{Iterations: i - 1, Reason: "cancelado", LastText: last}, nil
		}
		if cfg.Progress != nil {
			cfg.Progress(i, max)
		}

		text, err := conv.Run(ctx, prompt)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return Report{Iterations: i, Reason: "cancelado", LastText: last}, nil
			}
			return Report{Iterations: i, Reason: "error", LastText: text}, err
		}

		if hasSentinel(text) {
			return Report{Iterations: i, Reason: "completado", LastText: text}, nil
		}
		if i > 1 && strings.TrimSpace(text) != "" && strings.TrimSpace(text) == strings.TrimSpace(last) {
			return Report{Iterations: i, Reason: "sin progreso (dos respuestas idénticas)", LastText: text}, nil
		}
		last = text
	}
	return Report{Iterations: max, Reason: fmt.Sprintf("límite de %d iteraciones", max), LastText: last}, nil
}

// Summary renders a Report for display.
func (r Report) Summary() string {
	return fmt.Sprintf("objetivo: %s · %d iteración(es)", r.Reason, r.Iterations)
}

func hasSentinel(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == Sentinel {
			return true
		}
	}
	return false
}

func buildPrompt(goal string) string {
	return "OBJETIVO: " + strings.TrimSpace(goal) + "\n\n" +
		"Hacé el siguiente paso concreto hacia ese objetivo, usando las herramientas que necesites. " +
		"No pidas permiso para avanzar entre pasos. " +
		"Cuando el objetivo esté 100% terminado y verificado (los tests y el build pasan), " +
		"respondé exactamente una línea que diga " + Sentinel + " y nada más."
}
