package shell

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestResolveHonorsArnesShell(t *testing.T) {
	t.Run("forma completa se usa verbatim", func(t *testing.T) {
		t.Setenv("ARNES_SHELL", "pwsh -NoProfile -Command")
		got := Resolve()
		want := Spec{Name: "pwsh", Args: []string{"-NoProfile", "-Command"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Resolve() = %+v, quiero %+v", got, want)
		}
	})

	t.Run("token suelto recibe -c", func(t *testing.T) {
		t.Setenv("ARNES_SHELL", "zsh")
		got := Resolve()
		want := Spec{Name: "zsh", Args: []string{"-c"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Resolve() = %+v, quiero %+v", got, want)
		}
	})
}

func TestResolveOSDefault(t *testing.T) {
	t.Setenv("ARNES_SHELL", "")
	got := Resolve()
	switch runtime.GOOS {
	case "windows":
		if got.Name != "pwsh" && got.Name != "powershell" {
			t.Fatalf("en Windows esperaba pwsh/powershell, tengo %q", got.Name)
		}
		if got.Args[len(got.Args)-1] != "-Command" {
			t.Fatalf("último arg debería ser -Command: %v", got.Args)
		}
	default:
		if got.Name != "bash" && got.Name != "sh" {
			t.Fatalf("en unix esperaba bash/sh, tengo %q", got.Name)
		}
		if !reflect.DeepEqual(got.Args, []string{"-c"}) {
			t.Fatalf("args = %v, quiero [-c]", got.Args)
		}
	}
}

func TestArgv(t *testing.T) {
	s := Spec{Name: "bash", Args: []string{"-c"}}
	got := s.Argv("echo hola")
	want := []string{"bash", "-c", "echo hola"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Argv = %v, quiero %v", got, want)
	}
}

func TestCommandContextRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("el runner de CI es unix; el path Windows se cubre por Resolve")
	}
	t.Setenv("ARNES_SHELL", "")
	out, err := CommandContext(context.Background(), "echo hola-arnes").Output()
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if strings.TrimSpace(string(out)) != "hola-arnes" {
		t.Fatalf("out = %q", out)
	}
}

func TestPOSIXAndLabel(t *testing.T) {
	t.Setenv("ARNES_SHELL", "bash -c")
	if !POSIX() || Label() != "bash" {
		t.Fatalf("POSIX=%v Label=%q", POSIX(), Label())
	}
	t.Setenv("ARNES_SHELL", "powershell -Command")
	if POSIX() || Label() != "PowerShell" {
		t.Fatalf("POSIX=%v Label=%q", POSIX(), Label())
	}
}
