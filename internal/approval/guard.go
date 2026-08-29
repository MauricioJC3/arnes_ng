package approval

import (
	"encoding/json"
	"path"
	"path/filepath"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// DefaultProtectedPaths is what Guard uses when the config names none: dotenv
// files. Patterns are matched with path.Match against the target's basename,
// and (when the pattern has a slash) against its slash-cleaned relative form.
var DefaultProtectedPaths = []string{".env", ".env.*"}

// guardWriteTools are the tools whose "path" argument Guard inspects. bash is
// deliberately absent: its shell string cannot be statically resolved to a
// target path, so a redirect into .env from bash is not caught here.
var guardWriteTools = map[string]bool{"write_file": true, "edit_file": true}

// Guard sits in front of an auto-approving Pass approver and pulls back the
// calls that would write to a protected path, routing those to Inner (normally
// the interactive prompt). It is how auto mode can still stop at secrets.
type Guard struct {
	Pass    Approver // non-protected calls (e.g. AllowAll)
	Inner   Approver // protected-path writes (e.g. the interactive Prompt)
	Protect []string // path.Match globs; empty means DefaultProtectedPaths
}

// Confirm routes a protected-path write to Inner and everything else to Pass.
func (g Guard) Confirm(call provider.ToolCall) bool {
	if g.protected(call) {
		return g.Inner.Confirm(call)
	}
	return g.Pass.Confirm(call)
}

// protected reports whether call is a file write whose target matches one of the
// protected globs. A write tool whose path cannot be read is treated as not
// protected: the tool validates its own input and will reject a bad call.
func (g Guard) protected(call provider.ToolCall) bool {
	if !guardWriteTools[call.Name] {
		return false
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(call.Input, &in); err != nil || in.Path == "" {
		return false
	}

	pats := g.Protect
	if len(pats) == 0 {
		pats = DefaultProtectedPaths
	}
	clean := filepath.ToSlash(filepath.Clean(in.Path))
	base := path.Base(clean)
	for _, p := range pats {
		if ok, _ := path.Match(p, base); ok {
			return true
		}
		if strings.Contains(p, "/") {
			if ok, _ := path.Match(p, strings.TrimPrefix(clean, "./")); ok {
				return true
			}
		}
	}
	return false
}
