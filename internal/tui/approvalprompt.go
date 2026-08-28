package tui

import (
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
)

// approvalPrompt holds the tool call currently waiting for a y/n decision.
type approvalPrompt struct {
	pending *approval.Request
}

// active reports whether a decision is being awaited.
func (a *approvalPrompt) active() bool { return a.pending != nil }

// open records a new request as the one awaiting a decision.
func (a *approvalPrompt) open(req approval.Request) {
	r := req
	a.pending = &r
}

// answer applies a keypress. It replies to the agent and returns the line to log
// for a decision key (y/s/enter to allow, n/esc to deny); for any other key it
// leaves the request pending and returns "".
func (a *approvalPrompt) answer(key string) string {
	if a.pending == nil {
		return ""
	}
	name := a.pending.Call.Name
	switch strings.ToLower(key) {
	case "y", "s", "enter":
		a.pending.Reply(true)
		a.pending = nil
		return "✓ " + name + " permitido"
	case "n", "esc":
		a.pending.Reply(false)
		a.pending = nil
		return "✗ " + name + " denegado"
	}
	return ""
}
