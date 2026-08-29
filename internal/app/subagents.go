package app

// ListSubagents implements command.Subagents.
func (a *App) ListSubagents() []string {
	var out []string
	for _, d := range a.subagents.All() {
		out = append(out, d.Name+": "+d.Description)
	}
	return out
}
