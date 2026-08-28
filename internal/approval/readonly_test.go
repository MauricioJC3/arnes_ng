package approval

import (
	"testing"

	"github.com/andresmjimenez/arnes/internal/provider"
)

func TestReadOnly(t *testing.T) {
	r := ReadOnly{Allowed: map[string]bool{"read_file": true, "recall": true}}

	if !r.Confirm(provider.ToolCall{Name: "read_file"}) {
		t.Error("read_file debería aprobarse")
	}
	if r.Confirm(provider.ToolCall{Name: "write_file"}) {
		t.Error("write_file debería denegarse")
	}
	if r.Confirm(provider.ToolCall{Name: "bash"}) {
		t.Error("bash debería denegarse")
	}
}
