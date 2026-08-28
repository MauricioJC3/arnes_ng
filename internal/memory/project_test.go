package memory

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizeRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:MauricioJC3/arnes_ng.git":     "mauriciojc3/arnes_ng",
		"https://github.com/MauricioJC3/arnes_ng.git": "mauriciojc3/arnes_ng",
		"https://github.com/MauricioJC3/arnes_ng":     "mauriciojc3/arnes_ng",
		"ssh://git@gitlab.com/group/sub/proj.git":     "sub/proj",
		"git@bitbucket.org:team/repo":                 "team/repo",
	}
	for in, want := range cases {
		if got := normalizeRemote(in); got != want {
			t.Errorf("normalizeRemote(%q) = %q, quiero %q", in, got, want)
		}
	}
}

func TestDetectIDFallsBackToPath(t *testing.T) {
	dir := t.TempDir() // no es un repo git
	if got := DetectID(dir); got != dir {
		// filepath.Abs de un TempDir ya es absoluto, así que debería devolverlo tal cual
		if abs, _ := filepath.Abs(dir); got != abs {
			t.Fatalf("DetectID sin git = %q, quiero la ruta %q", got, dir)
		}
	}
}

func TestDetectIDUsesGitRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git no está disponible")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "git@github.com:acme/widget.git"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if got := DetectID(dir); got != "acme/widget" {
		t.Fatalf("DetectID = %q, quiero acme/widget", got)
	}
}
