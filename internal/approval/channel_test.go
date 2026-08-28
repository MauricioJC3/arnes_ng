package approval

import (
	"testing"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

func TestChannelApprove(t *testing.T) {
	c := NewChannel()
	go func() {
		req := <-c.Requests
		if req.Call.Name != "bash" {
			t.Errorf("Call.Name = %q", req.Call.Name)
		}
		req.Reply(true)
	}()

	got := make(chan bool, 1)
	go func() { got <- c.Confirm(provider.ToolCall{Name: "bash"}) }()

	select {
	case ok := <-got:
		if !ok {
			t.Fatal("esperaba aprobación")
		}
	case <-time.After(time.Second):
		t.Fatal("Confirm no volvió")
	}
}

func TestChannelDeny(t *testing.T) {
	c := NewChannel()
	go func() { (<-c.Requests).Reply(false) }()
	if c.Confirm(provider.ToolCall{Name: "rm"}) {
		t.Fatal("esperaba denegación")
	}
}

func TestRequestReplyIsSafeTwice(t *testing.T) {
	r := Request{Response: make(chan bool, 1)}
	r.Reply(true)
	r.Reply(false) // no debe bloquear ni panickear
	if got := <-r.Response; !got {
		t.Fatalf("la primera respuesta debería ganar, got %v", got)
	}
}
