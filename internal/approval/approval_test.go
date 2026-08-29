package approval

import (
	"bufio"
	"strings"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

func TestAllowAllAndDenyAll(t *testing.T) {
	call := provider.ToolCall{Name: "bash"}
	if !(AllowAll{}).Confirm(call) {
		t.Error("AllowAll debería aprobar todo")
	}
	if (DenyAll{}).Confirm(call) {
		t.Error("DenyAll debería rechazar todo")
	}
}

func TestPromptConfirm(t *testing.T) {
	yes := []string{"y\n", "yes\n", "s\n", "si\n", "  Y  \n", "YES\n", "Si\n"}
	for _, in := range yes {
		var out strings.Builder
		p := Prompt{In: bufio.NewReader(strings.NewReader(in)), Out: &out}
		if !p.Confirm(provider.ToolCall{Name: "bash", Input: []byte(`{"command":"ls"}`)}) {
			t.Errorf("Confirm(%q) = false, debería aprobar", in)
		}
	}

	no := []string{"n\n", "no\n", "\n", "nope\n", "yeah\n", "quizás\n", ""} // "" = EOF sin línea
	for _, in := range no {
		var out strings.Builder
		p := Prompt{In: bufio.NewReader(strings.NewReader(in)), Out: &out}
		if p.Confirm(provider.ToolCall{Name: "bash"}) {
			t.Errorf("Confirm(%q) = true, debería rechazar (EOF y cualquier cosa que no sea sí es no)", in)
		}
	}
}

func TestPromptConfirmShowsToolAndArgs(t *testing.T) {
	var out strings.Builder
	p := Prompt{In: bufio.NewReader(strings.NewReader("n\n")), Out: &out}
	p.Confirm(provider.ToolCall{Name: "write_file", Input: []byte(`{"path":"x.txt"}`)})

	s := out.String()
	if !strings.Contains(s, "write_file") || !strings.Contains(s, `"path":"x.txt"`) {
		t.Fatalf("el prompt no mostró la tool y sus argumentos:\n%s", s)
	}
}
