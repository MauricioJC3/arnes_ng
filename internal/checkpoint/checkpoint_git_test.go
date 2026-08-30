package checkpoint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

func bashCall() provider.ToolCall {
	in, _ := json.Marshal(map[string]string{"command": "true"})
	return provider.ToolCall{Name: "bash", Input: in}
}

// initRepo makes a git repo in dir with one committed file and returns its path.
func initRepo(t *testing.T, dir string) string {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	tracked := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return tracked
}

func TestRewindRestoresBashChangesViaGit(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: usa git de verdad")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no disponible")
	}

	dir := t.TempDir()
	tracked := initRepo(t, dir)

	s := NewStore(WithWorkdir(dir))
	s.Begin([]provider.Message{{Role: provider.RoleUser, Text: "t"}}, "t")

	// A turn runs bash: the baseline is captured before the command's effect.
	s.Observe(bashCall())

	// The shell command's effect: a clean tracked file is rewritten in place
	// (the sed -i case) and a new file appears.
	if err := os.WriteFile(tracked, []byte("mutado por el comando\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(dir, "generado.txt")
	if err := os.WriteFile(newFile, []byte("nuevo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Rewind(1); err != nil {
		t.Fatalf("rewind: %v", err)
	}

	got, _ := os.ReadFile(tracked)
	if string(got) != "committed\n" {
		t.Fatalf("tracked.txt = %q, quiero el contenido committeado", got)
	}
	// A newly created file is left in place (documented behaviour).
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("un archivo nuevo creado por bash no debería borrarse: %v", err)
	}
}

func TestRewindGitBaselineWithPreexistingDirtyState(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: usa git de verdad")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no disponible")
	}

	dir := t.TempDir()
	tracked := initRepo(t, dir)

	// The tree is already dirty when the turn starts.
	if err := os.WriteFile(tracked, []byte("edición previa al turno\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(WithWorkdir(dir))
	s.Begin(nil, "t")
	s.Observe(bashCall()) // baseline = stash-create of the dirty state

	if err := os.WriteFile(tracked, []byte("lo que hizo el comando\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Rewind(1); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	got, _ := os.ReadFile(tracked)
	if string(got) != "edición previa al turno\n" {
		t.Fatalf("tracked.txt = %q, quiero la edición previa al turno (no el commit, no el comando)", got)
	}
}

func TestObserveGitBaselineOncePerTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: usa git de verdad")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no disponible")
	}
	dir := t.TempDir()
	initRepo(t, dir)

	s := NewStore(WithWorkdir(dir))
	s.Begin(nil, "t")
	s.Observe(bashCall())
	first := s.List()[0].git
	if first == nil {
		t.Fatal("la primera llamada a bash debería fijar el baseline")
	}
	s.Observe(bashCall())
	if s.List()[0].git != first {
		t.Fatal("una segunda llamada a bash no debería re-capturar el baseline")
	}
}
