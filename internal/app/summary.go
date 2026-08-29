package app

import (
	"fmt"
	"path/filepath"
)

// StartupInfo carries the subsystem counts StartupSummary can't read off the App
// itself (they live in the composition root).
type StartupInfo struct {
	RulesLabel string
	Skills     int
	MCPTools   int
	Hooks      int
	LSPServers int
	ProjID     string
}

// StartupSummary is the one-line banner printed (plain UI) or shown as the TUI
// greeting: provider, model, mode and the size of every configured subsystem.
func (a *App) StartupSummary(info StartupInfo) string {
	memCount := 0
	if a.mem != nil {
		if notes, err := a.mem.All(); err == nil {
			memCount = len(notes)
		}
	}
	projLabel := info.ProjID
	if filepath.IsAbs(projLabel) { // no git remote: show just the folder name
		projLabel = filepath.Base(projLabel)
	}
	return fmt.Sprintf("proveedor %s · modelo %s · modo %s · %s · sesión %s · compactación %s · subagentes %d · skills %d · mcp %d tools · hooks %d · lsp %d · memoria %d [%s]",
		a.providerName, a.prov.Model(), a.mode, info.RulesLabel, a.sess.ID, a.ag.CompactorName(),
		a.subagents.Len(), info.Skills, info.MCPTools, info.Hooks, info.LSPServers, memCount, projLabel)
}
