package app

import (
	"encoding/json"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// activityMaxLen caps a single activity line (in runes) so a huge bash command
// or path can't blow up the TUI panel.
const activityMaxLen = 140

// toolObserver builds the single passive pre-execute observer the agent runs
// before every approved tool call. It fans out to whatever is configured: the
// checkpoint file-snapshotter and the TUI "what is it doing" feed. Returns nil
// when neither is set, so agentOptions can skip the option entirely.
func (a *App) toolObserver() func(provider.ToolCall) {
	switch {
	case a.checkpoints != nil && a.activity != nil:
		return func(call provider.ToolCall) {
			a.checkpoints.Observe(call)
			a.emitActivity(call)
		}
	case a.checkpoints != nil:
		return a.checkpoints.Observe
	case a.activity != nil:
		return a.emitActivity
	default:
		return nil
	}
}

// emitActivity pushes one human-readable "doing X" line onto the activity feed.
// The send is non-blocking: it runs on the turn goroutine, so a full channel
// must never stall the agent -- a dropped status line is harmless.
func (a *App) emitActivity(call provider.ToolCall) {
	if a.activity == nil {
		return
	}
	select {
	case a.activity <- activityLine(call):
	default:
	}
}

// activityLine renders a tool call as a short past-tense-ish status line for the
// transcript. It leans on the well-known tool names; anything unknown falls back
// to the bare name. Input parsing is best-effort -- a missing field just yields
// a terser line.
func activityLine(call provider.ToolCall) string {
	var in map[string]any
	_ = json.Unmarshal(call.Input, &in)
	get := func(k string) string {
		s, _ := in[k].(string)
		return strings.TrimSpace(s)
	}

	var line string
	switch call.Name {
	case "bash":
		line = "$ " + oneLine(get("command"))
	case "write_file":
		line = "escribió " + get("path")
	case "edit_file":
		line = "editó " + get("path")
	case "read_file":
		line = "leyó " + get("path")
	case "grep":
		line = "grep " + get("pattern")
	case "glob":
		line = "glob " + get("pattern")
	case "todo_write":
		line = "actualizó la lista de tareas"
	case "skill":
		line = "cargó la skill " + get("name")
	case "remember":
		line = "guardó en memoria"
	case "recall":
		line = "consultó la memoria"
	case "lsp":
		line = "lsp " + get("action")
	case "code_graph":
		line = "code_graph " + strings.TrimSpace(get("op")+" "+get("query"))
	case "delegate":
		line = "delegó en el subagente " + get("agent")
	default:
		line = call.Name
	}

	line = strings.TrimSpace(line)
	if r := []rune(line); len(r) > activityMaxLen {
		line = string(r[:activityMaxLen-1]) + "…"
	}
	return line
}

// oneLine collapses a multi-line string to its first non-empty line, marking the
// truncation so a heredoc or a chained command still reads clearly.
func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " …"
	}
	return s
}
