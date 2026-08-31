// Package shell resolves the command interpreter arnes uses to run a shell
// command string. It honors $ARNES_SHELL, then falls back per OS: a POSIX
// bash/sh on Unix, and PowerShell on Windows (pwsh when it is on PATH, else the
// built-in Windows PowerShell 5.1).
package shell

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Spec is a resolved interpreter: the binary plus the arguments that precede the
// command string.
type Spec struct {
	Name string
	Args []string
}

// Resolve picks the interpreter:
//
//  1. $ARNES_SHELL, whitespace-split, used verbatim as "<binary> <args...>"
//     (e.g. "bash -c", "pwsh -NoProfile -Command"). A lone token gets "-c"
//     appended, which covers the common POSIX case ("zsh", "dash").
//  2. Unix: `bash -c`, or `sh -c` when bash is not on PATH.
//  3. Windows: `pwsh -NoProfile -NonInteractive -Command` when pwsh is on PATH,
//     otherwise `powershell -NoProfile -NonInteractive -Command`.
func Resolve() Spec {
	if custom := strings.Fields(os.Getenv("ARNES_SHELL")); len(custom) > 0 {
		if len(custom) == 1 {
			return Spec{Name: custom[0], Args: []string{"-c"}}
		}
		return Spec{Name: custom[0], Args: custom[1:]}
	}
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err == nil {
			return Spec{Name: "pwsh", Args: []string{"-NoProfile", "-NonInteractive", "-Command"}}
		}
		return Spec{Name: "powershell", Args: []string{"-NoProfile", "-NonInteractive", "-Command"}}
	}
	if _, err := exec.LookPath("bash"); err != nil {
		return Spec{Name: "sh", Args: []string{"-c"}}
	}
	return Spec{Name: "bash", Args: []string{"-c"}}
}

// Argv is the full argument vector to run command through this interpreter:
// [Name, Args..., command].
func (s Spec) Argv(command string) []string {
	out := make([]string, 0, len(s.Args)+2)
	out = append(out, s.Name)
	out = append(out, s.Args...)
	out = append(out, command)
	return out
}

// CommandContext builds an *exec.Cmd that runs command through the resolved
// interpreter.
func CommandContext(ctx context.Context, command string) *exec.Cmd {
	argv := Resolve().Argv(command)
	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

// Label is a short human name for the resolved interpreter, for tool
// descriptions and prompt hints.
func Label() string {
	switch n := Resolve().Name; n {
	case "powershell":
		return "PowerShell"
	default:
		return n
	}
}

// POSIX reports whether the resolved interpreter takes POSIX-shell syntax
// (so `&&`, `$(...)`, `>` redirection and the usual coreutils are available).
func POSIX() bool {
	switch Resolve().Name {
	case "bash", "sh":
		return true
	default:
		return false
	}
}
