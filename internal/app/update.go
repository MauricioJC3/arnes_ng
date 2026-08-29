package app

import (
	"context"
	"fmt"

	"github.com/MauricioJC3/arnes_ng/internal/update"
)

// SelfUpdate implements command.Updater: check GitHub for a newer release and,
// if there is one, replace this binary with it. It blocks while downloading.
func (a *App) SelfUpdate(ctx context.Context) (string, error) {
	rel, newer, err := update.Check(ctx, update.GitHub{Repo: a.repo}, a.version)
	if err != nil {
		return "", err
	}
	if !newer {
		return "arnes " + a.version + " ya está al día", nil
	}
	self, err := update.SelfPath()
	if err != nil {
		return "", err
	}
	if err := update.Apply(ctx, rel, self); err != nil {
		return "", err
	}
	return fmt.Sprintf("actualizado %s → %s · reiniciá arnes para usar la nueva versión", a.version, rel.Version), nil
}
