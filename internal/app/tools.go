package app

import (
	"context"

	"github.com/MauricioJC3/arnes_ng/internal/lsp"
	"github.com/MauricioJC3/arnes_ng/internal/memory"
	"github.com/MauricioJC3/arnes_ng/internal/skill"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
	"github.com/MauricioJC3/arnes_ng/internal/tool"
)

// BaseToolDeps is what BuildBaseTools needs from the composition root.
type BaseToolDeps struct {
	Todos  *todo.Store
	LSPMgr *lsp.Manager
	Skills *skill.Registry
	Mem    memory.Store
	Files  *tool.FileTracker // read-before-write guard; nil disables it
	CWD    string            // project root, for the optional codegraph tool
}

// BuildBaseTools assembles the tool pool shared by the agent and its subagents
// (everything except delegate, which would let subagents recurse). The agent's
// own registry is this pool plus the delegate tool, installed via SetTools.
func BuildBaseTools(d BaseToolDeps) *tool.Registry {
	reg := tool.NewRegistry(
		tool.Bash{Timeout: tool.DefaultBashTimeout},
		tool.Grep{},
		tool.Glob{},
		tool.ReadFile{Tracker: d.Files},
		tool.WriteFile{Tracker: d.Files},
		tool.EditFile{Tracker: d.Files},
		tool.TodoWrite{Store: d.Todos},
		tool.LSP{Client: func(ctx context.Context, path string) (tool.LSPClient, error) {
			return d.LSPMgr.For(ctx, path)
		}},
		tool.Skill{Skills: d.Skills},
		tool.Remember{Store: d.Mem},
		tool.Recall{Store: d.Mem},
	)
	// codegraph is optional: only offered when the CLI is installed and the
	// project has an index, so the model never sees a tool it can't use.
	if cg := tool.NewCodeGraph(d.CWD); cg != nil {
		reg = reg.With(cg)
	}
	return reg
}
