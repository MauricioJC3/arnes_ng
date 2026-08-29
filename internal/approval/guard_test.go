package approval

import (
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// spyApprover records whether it was asked and answers a fixed verdict.
type spyApprover struct {
	verdict bool
	asked   bool
}

func (s *spyApprover) Confirm(provider.ToolCall) bool {
	s.asked = true
	return s.verdict
}

func call(name, input string) provider.ToolCall {
	return provider.ToolCall{Name: name, Input: []byte(input)}
}

func TestGuardRoutesProtectedWritesToInner(t *testing.T) {
	cases := []struct {
		name      string
		call      provider.ToolCall
		toInner   bool
		protified string
	}{
		{"write_file a .env", call("write_file", `{"path":".env"}`), true, ""},
		{"write_file a ./.env", call("write_file", `{"path":"./.env"}`), true, ""},
		{"edit_file a .env.local", call("edit_file", `{"path":".env.local"}`), true, ""},
		{"write_file a config/.env", call("write_file", `{"path":"config/.env"}`), true, ""},
		{"write_file a src/main.go", call("write_file", `{"path":"src/main.go"}`), false, ""},
		{"edit_file a README.md", call("edit_file", `{"path":"README.md"}`), false, ""},
		{"bash con redirect a .env NO se inspecciona", call("bash", `{"command":"echo x > .env"}`), false, ""},
		{"read_file a .env NO es escritura", call("read_file", `{"path":".env"}`), false, ""},
		{"write_file sin path legible", call("write_file", `{"nope":true}`), false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pass := &spyApprover{verdict: true}
			inner := &spyApprover{verdict: false}
			g := Guard{Pass: pass, Inner: inner}

			got := g.Confirm(tc.call)

			if tc.toInner {
				if !inner.asked || pass.asked {
					t.Fatalf("esperaba ruteo a Inner; inner.asked=%v pass.asked=%v", inner.asked, pass.asked)
				}
				if got != false {
					t.Fatalf("verdict = %v, quiero el de Inner (false)", got)
				}
			} else {
				if !pass.asked || inner.asked {
					t.Fatalf("esperaba ruteo a Pass; pass.asked=%v inner.asked=%v", pass.asked, inner.asked)
				}
				if got != true {
					t.Fatalf("verdict = %v, quiero el de Pass (true)", got)
				}
			}
		})
	}
}

func TestGuardCustomProtectOverridesDefaults(t *testing.T) {
	pass := &spyApprover{verdict: true}
	inner := &spyApprover{verdict: false}
	g := Guard{Pass: pass, Inner: inner, Protect: []string{"secrets.yaml", "deploy/*.key"}}

	// .env ya no está protegido con una lista propia
	if !g.Confirm(call("write_file", `{"path":".env"}`)) {
		t.Fatal(".env debería ir a Pass cuando Protect no lo incluye")
	}
	// pero el patrón propio sí
	inner.asked = false
	if g.Confirm(call("write_file", `{"path":"deploy/prod.key"}`)) {
		t.Fatal("deploy/prod.key debería ir a Inner (deny)")
	}
	if !inner.asked {
		t.Fatal("deploy/prod.key no fue ruteado a Inner")
	}
}
