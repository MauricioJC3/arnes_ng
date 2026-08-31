package app

import (
	"context"
	"strings"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/shell"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

// checkTimeout bounds one run of the project verification command.
const checkTimeout = 5 * time.Minute

// checkOutputCap keeps the tail of a long check output (build/test failures land
// at the end) so the completion gate never floods the history.
const checkOutputCap = 16_000

// verifier builds the completion-gate check from the configured command, or nil
// when none is set. It shells out through the platform interpreter (see
// internal/shell), returns the combined output and whether the command exited 0.
func (a *App) verifier() func(context.Context) (string, bool) {
	cmd := strings.TrimSpace(a.checkCommand)
	if cmd == "" {
		return nil
	}
	return func(ctx context.Context) (string, bool) {
		ctx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()
		out, err := shell.CommandContext(ctx, cmd).CombinedOutput()
		s := string(out)
		if len(s) > checkOutputCap {
			s = "[... salida recortada; se conserva el final ...]\n" + s[len(s)-checkOutputCap:]
		}
		if err != nil && strings.TrimSpace(s) == "" {
			s = err.Error()
		}
		return s, err == nil
	}
}

// anchorText is the suffix appended to the system prompt on every model call: the
// session's original task and the live checklist, so neither is lost when
// compaction drops old history. It returns "" when there is nothing to anchor.
func (a *App) anchorText() string {
	var b strings.Builder
	if a.firstTask != "" {
		b.WriteString("\n\n# Tarea original de la sesión\n")
		b.WriteString(a.firstTask)
		b.WriteString("\n")
	}
	if a.todos != nil {
		if items := a.todos.Get(); len(items) > 0 {
			b.WriteString("\n# Plan actual (checklist)\n")
			for _, it := range items {
				b.WriteString("- [")
				b.WriteString(todoMark(it.Status))
				b.WriteString("] ")
				b.WriteString(it.Content)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func todoMark(s todo.Status) string {
	switch s {
	case todo.Done:
		return "x"
	case todo.InProgress:
		return "~"
	default:
		return " "
	}
}

// openTodos returns the contents of every checklist item that is not done, for
// the completion gate's unfinished-work nudge.
func (a *App) openTodos() []string {
	if a.todos == nil {
		return nil
	}
	var open []string
	for _, it := range a.todos.Get() {
		if it.Status != todo.Done {
			open = append(open, it.Content)
		}
	}
	return open
}

// firstUserText returns the text of the first real user turn in a history
// (skipping tool-result turns), or "" if there is none. Used on resume to
// recover the session's original task for the anchor.
func firstUserText(history []provider.Message) string {
	for _, m := range history {
		if m.Role == provider.RoleUser && len(m.ToolResults) == 0 && strings.TrimSpace(m.Text) != "" {
			return strings.TrimSpace(m.Text)
		}
	}
	return ""
}
