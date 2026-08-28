package approval

import "github.com/MauricioJC3/arnes_ng/internal/provider"

// Request is one pending tool approval handed to an async front-end (the TUI).
type Request struct {
	Call     provider.ToolCall
	Response chan bool
}

// Reply sends the decision back to the waiting agent. Safe to call once.
func (r Request) Reply(allow bool) {
	select {
	case r.Response <- allow:
	default:
	}
}

// Channel is an Approver that forwards each request to a channel and blocks
// until the front-end replies. Use it when approval cannot read stdin directly
// (a full-screen TUI owns the terminal).
type Channel struct {
	Requests chan Request
}

// NewChannel builds a Channel with an unbuffered request channel.
func NewChannel() Channel {
	return Channel{Requests: make(chan Request)}
}

// Confirm blocks the agent goroutine until the front-end replies.
func (c Channel) Confirm(call provider.ToolCall) bool {
	resp := make(chan bool, 1)
	c.Requests <- Request{Call: call, Response: resp}
	return <-resp
}
