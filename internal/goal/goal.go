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
	// NewConversation, when set, is called for a FRESH conversation each
	// iteration (the classic Ralph loop: no accumulated context, state lives in
	// files/git). When nil, the passed-in conv is reused (accumulating).
	NewConversation func() Conversation
}

// Report is the outcome of a goal run.
type Report struct {
	Iterations         int
	Reason             string // completado | límite | sin progreso | cancelado | error
	LastText           string
	TokensIn, TokensOut int
}

// usager is the optional token-reporting side of a Conversation.
type usager interface {
	Usage() (in, out int)
}

// Run drives conv toward goal.
func Run(ctx context.Context, conv Conversation, goal string, cfg Config) (Report, error) {
	max := cfg.MaxIterations
	if max <= 0 {
		max = DefaultMaxIterations
	}
	fresh := cfg.NewConversation != nil
	prompt := buildPrompt(goal, fresh)

	var last string
	var totIn, totOut int
	for i := 1; i <= max; i++ {
		if ctx.Err() != nil {
			return report(i-1, "cancelado", last, totIn, totOut), nil
		}
		if cfg.Progress != nil {
			cfg.Progress(i, max)
		}

		c := conv
		if fresh {
			c = cfg.NewConversation()
		}

		text, err := c.Run(ctx, prompt)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return report(i, "cancelado", last, totIn, totOut), nil
			}
			return report(i, "error", text, totIn, totOut), err
		}

		if u, ok := c.(usager); ok {
			in, out := u.Usage()
			if fresh { // each conv is new: add its whole usage
				totIn, totOut = totIn+in, totOut+out
			} else { // same conv: usage is cumulative, take the latest
				totIn, totOut = in, out
			}
		}

		if hasSentinel(text) {
			return report(i, "completado", text, totIn, totOut), nil
		}
		if i > 1 && strings.TrimSpace(text) != "" && strings.TrimSpace(text) == strings.TrimSpace(last) {
			return report(i, "sin progreso (dos respuestas idénticas)", text, totIn, totOut), nil
		}
		last = text
	}
	return report(max, fmt.Sprintf("límite de %d iteraciones", max), last, totIn, totOut), nil
}

func report(iter int, reason, last string, in, out int) Report {
	return Report{Iterations: iter, Reason: reason, LastText: last, TokensIn: in, TokensOut: out}
}

// Summary renders a Report for display.
func (r Report) Summary() string {
	s := fmt.Sprintf("objetivo: %s · %d iteración(es)", r.Reason, r.Iterations)
	if tok := r.TokensIn + r.TokensOut; tok > 0 {
		s += " · ~" + humanK(tok) + " tok"
	}
	return s
}

func humanK(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func hasSentinel(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == Sentinel {
			return true
		}
	}
	return false
}

func buildPrompt(goal string, fresh bool) string {
	b := "OBJETIVO: " + strings.TrimSpace(goal) + "\n\n"
	if fresh {
		b += "Estás retomando una tarea en curso, sin memoria de las iteraciones anteriores. " +
			"Primero ponete al día: mirá `git log --oneline -8` y `git status`, y si hay un archivo " +
			"de progreso (PLAN.md, TODO.md, NOTES.md) leelo. Después seguí con el siguiente paso.\n\n"
	}
	b += "Hacé el siguiente paso concreto hacia el objetivo, usando las herramientas que necesites. " +
		"No pidas permiso para avanzar entre pasos. " +
		"Cuando el objetivo esté 100% terminado y verificado (los tests y el build pasan), " +
		"respondé exactamente una línea que diga " + Sentinel + " y nada más."
	return b
}
