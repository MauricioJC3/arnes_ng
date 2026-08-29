package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/session"
)

// ListSessions implements command.Sessions.
func (a *App) ListSessions() ([]session.Meta, error) { return a.store.List() }

// ResumeSession implements command.Sessions: load by id or unique prefix, then swap.
func (a *App) ResumeSession(id string) (string, error) {
	s, err := a.resolve(id)
	if err != nil {
		return "", err
	}
	if s.Model != "" {
		a.prov.SetModel(s.Model)
	}
	a.usedIn, a.usedOut = s.UsageIn, s.UsageOut // continue the session's spend
	a.rebuild(s, s.Messages)
	return fmt.Sprintf("reanudada %s (%d mensajes)", s.ID, len(s.Messages)), nil
}

// NewSession implements command.Sessions.
func (a *App) NewSession() (string, error) {
	cwd, _ := os.Getwd()
	s := session.New(a.providerName, a.prov.Model(), cwd)
	a.usedIn, a.usedOut = 0, 0
	a.rebuild(s, nil)
	return "sesión nueva: " + s.ID, nil
}

// resolve finds a session by exact id, or by an unambiguous id prefix.
func (a *App) resolve(id string) (*session.Session, error) {
	switch s, err := a.store.Load(id); {
	case err == nil:
		return s, nil
	case !errors.Is(err, session.ErrNotFound):
		return nil, err
	}

	metas, err := a.store.List()
	if err != nil {
		return nil, err
	}
	var match string
	for _, m := range metas {
		if !strings.HasPrefix(m.ID, id) {
			continue
		}
		if match != "" {
			return nil, fmt.Errorf("el prefijo %q es ambiguo", id)
		}
		match = m.ID
	}
	if match == "" {
		return nil, session.ErrNotFound
	}
	return a.store.Load(match)
}
