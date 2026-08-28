package approval

import (
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

func TestSafe(t *testing.T) {
	s := NewSafe(DenyAll{}, "todo_write")

	if !s.Confirm(provider.ToolCall{Name: "todo_write"}) {
		t.Error("todo_write debería auto-aprobarse")
	}
	if s.Confirm(provider.ToolCall{Name: "bash"}) {
		t.Error("bash debería caer en el Inner (DenyAll)")
	}

	s2 := NewSafe(AllowAll{}, "todo_write")
	if !s2.Confirm(provider.ToolCall{Name: "bash"}) {
		t.Error("con Inner AllowAll, bash debería pasar")
	}
}
