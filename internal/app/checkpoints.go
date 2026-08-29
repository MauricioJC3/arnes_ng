package app

import "fmt"

// ListCheckpoints implements command.Rewinder.
func (a *App) ListCheckpoints() string { return a.checkpoints.Summary() }

// Rewind implements command.Rewinder: restore the files captured since
// checkpoint n and rebuild the agent from that checkpoint's history.
func (a *App) Rewind(n int) (string, error) {
	cp, err := a.checkpoints.Rewind(n)
	if cp == nil {
		return "", err
	}
	hist := cp.History()
	a.sess.Messages = hist
	a.rebuild(a.sess, hist)
	if saveErr := a.store.Save(a.sess); saveErr != nil {
		return "", fmt.Errorf("rewind aplicado, pero no se pudo guardar la sesión: %w", saveErr)
	}
	msg := fmt.Sprintf("rewind al checkpoint %d · %d archivo(s) restaurado(s) · historial en %d mensajes",
		n, cp.Files(), len(hist))
	if err != nil {
		return msg, err // partial: some files failed to restore
	}
	return msg, nil
}
