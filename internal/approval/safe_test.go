package approval

import (
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

func TestSafe(t *testing.T) {
	s := NewSafe(DenyAll{}, "todo_write", "read_file", "grep")

	for _, ok := range []string{"todo_write", "read_file", "grep"} {
		if !s.Confirm(provider.ToolCall{Name: ok}) {
			t.Errorf("%s debería auto-aprobarse", ok)
		}
	}
	for _, gated := range []string{"bash", "write_file", "edit_file", "remember"} {
		if s.Confirm(provider.ToolCall{Name: gated}) {
			t.Errorf("%s debería caer en el Inner (DenyAll)", gated)
		}
	}

	s2 := NewSafe(AllowAll{}, "todo_write")
	if !s2.Confirm(provider.ToolCall{Name: "bash"}) {
		t.Error("con Inner AllowAll, bash debería pasar")
	}
}
