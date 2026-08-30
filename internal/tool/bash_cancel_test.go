//go:build unix

package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBashCancelStopsPromptly: a hanging command must not outlive a cancelled
// context by more than the post-kill grace period. This is the regression for
// "Ctrl+C is accepted but the turn never stops".
func TestBashCancelStopsPromptly(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short: ejecuta comandos reales")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan time.Duration, 1)

	go func() {
		start := time.Now()
		_, _ = Bash{Timeout: time.Minute}.Execute(ctx,
			mustJSON(t, map[string]string{"command": "sleep 30"}))
		done <- time.Since(start)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case elapsed := <-done:
		if elapsed > killGrace+2*time.Second {
			t.Fatalf("Execute tardó %s en volver tras el cancel; el comando quedó colgado", elapsed)
		}
	case <-time.After(killGrace + 5*time.Second):
		t.Fatal("Execute nunca volvió tras el cancel: el turno queda trabado (el bug)")
	}
}

// TestBashCancelKillsChildProcesses: a command that spawns a background child
// (a server, a watcher, a pipeline) must have that child killed too when the
// context is cancelled -- otherwise the child keeps the output pipe open and
// wedges Wait forever.
func TestBashCancelKillsChildProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("omitido en -short: ejecuta comandos reales")
	}
	marker := filepath.Join(t.TempDir(), "child-ran")
	cmd := "(sleep 1; touch " + marker + ") & sleep 30"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = Bash{Timeout: time.Minute}.Execute(ctx, mustJSON(t, map[string]string{"command": cmd}))
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	// The child was scheduled to create the marker at +1s; give it well past
	// that. If the process group was killed it never runs.
	time.Sleep(2 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("el proceso hijo sobrevivió al cancel: no se mató el grupo de procesos")
	}
}
