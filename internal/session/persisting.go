package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/andresmjimenez/arnes/internal/provider"
)

// Agent is the slice of the agent the persister needs.
type Agent interface {
	Run(ctx context.Context, userInput string) (string, error)
	History() []provider.Message
}

// Persisting decorates an Agent: it runs the turn, then snapshots the history
// into the session and saves it. It satisfies the same Run contract, so the
// REPL uses it in place of the bare agent.
type Persisting struct {
	agent   Agent
	store   Store
	sess    *Session
	modelFn func() string
}

// PersistingOption tunes a Persisting.
type PersistingOption func(*Persisting)

// WithModelFn keeps sess.Model current with runtime model changes (e.g. the
// /model command). It is read on every save.
func WithModelFn(f func() string) PersistingOption {
	return func(p *Persisting) { p.modelFn = f }
}

// NewPersisting wraps agent so every turn is written to store under sess.
func NewPersisting(agent Agent, store Store, sess *Session, opts ...PersistingOption) *Persisting {
	p := &Persisting{agent: agent, store: store, sess: sess}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Session returns the live session being written to.
func (p *Persisting) Session() *Session { return p.sess }

// Run delegates to the agent, then persists. A save failure is surfaced in the
// returned error but never discards the agent's answer.
func (p *Persisting) Run(ctx context.Context, userInput string) (string, error) {
	out, runErr := p.agent.Run(ctx, userInput)

	p.sess.Messages = p.agent.History()
	p.sess.UpdatedAt = time.Now()
	if p.sess.Title == "" {
		p.sess.Title = title(userInput)
	}
	if p.modelFn != nil {
		if m := p.modelFn(); m != "" {
			p.sess.Model = m
		}
	}

	if saveErr := p.store.Save(p.sess); saveErr != nil {
		return out, errors.Join(runErr, fmt.Errorf("no se pudo guardar la sesión %s: %w", p.sess.ID, saveErr))
	}
	return out, runErr
}
