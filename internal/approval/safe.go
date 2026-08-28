package approval

import "github.com/MauricioJC3/arnes_ng/internal/provider"

// Safe auto-approves a small set of side-effect-free harness tools (e.g.
// todo_write, which only updates the in-memory checklist) and defers every
// other call to Inner. It keeps the human in the loop for anything that touches
// the machine while sparing them a prompt for pure bookkeeping.
type Safe struct {
	Inner Approver
	Auto  map[string]bool
}

// NewSafe wraps inner, auto-approving the named tools.
func NewSafe(inner Approver, names ...string) Safe {
	auto := make(map[string]bool, len(names))
	for _, n := range names {
		auto[n] = true
	}
	return Safe{Inner: inner, Auto: auto}
}

// Confirm approves auto-listed tools outright; everything else goes to Inner.
func (s Safe) Confirm(call provider.ToolCall) bool {
	if s.Auto[call.Name] {
		return true
	}
	return s.Inner.Confirm(call)
}
