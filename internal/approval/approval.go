// Package approval is the safety gateway: nothing the model requests runs until
// an Approver says yes.
package approval

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// Approver decides whether a single tool call is allowed to execute.
type Approver interface {
	Confirm(call provider.ToolCall) bool
}

// AllowAll approves every call. Tests and an explicit --yes flag only.
type AllowAll struct{}

func (AllowAll) Confirm(provider.ToolCall) bool { return true }

// DenyAll rejects every call.
type DenyAll struct{}

func (DenyAll) Confirm(provider.ToolCall) bool { return false }

// Prompt asks the human on Out and reads the answer from In. In must be the
// single bufio.Reader wrapping stdin (shared with the REPL) so buffered input
// is not lost between the two readers.
type Prompt struct {
	In  *bufio.Reader
	Out io.Writer
}

// Confirm shows the tool name and raw arguments, then accepts y/yes/s/si
// (case-insensitive). Anything else, EOF included, is a no.
func (p Prompt) Confirm(call provider.ToolCall) bool {
	fmt.Fprintf(p.Out, "\n  la IA quiere ejecutar %q con:\n  %s\n  ¿permitir? [y/N] ", call.Name, string(call.Input))
	line, err := p.In.ReadString('\n')
	if err != nil && err != io.EOF {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "s", "si":
		return true
	default:
		return false
	}
}
