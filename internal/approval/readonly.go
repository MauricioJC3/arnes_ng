package approval

import "github.com/MauricioJC3/arnes_ng/internal/provider"

// ReadOnly auto-approves a whitelist of (read-only) tools and auto-denies every
// other tool without prompting. Plan mode uses it.
type ReadOnly struct {
	Allowed map[string]bool
}

func (r ReadOnly) Confirm(call provider.ToolCall) bool {
	return r.Allowed[call.Name]
}
