package tui

import (
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

func newReq() approval.Request {
	return approval.Request{
		Call:     provider.ToolCall{Name: "bash"},
		Response: make(chan bool, 1),
	}
}

func TestApprovalPromptAllow(t *testing.T) {
	var a approvalPrompt
	if a.active() {
		t.Fatal("no debería arrancar activo")
	}
	req := newReq()
	a.open(req)
	if !a.active() {
		t.Fatal("open no dejó una decisión pendiente")
	}

	line := a.answer("y")
	if line == "" || a.active() {
		t.Fatalf("answer(y) = %q / active=%v", line, a.active())
	}
	if got := <-req.Response; !got {
		t.Fatal("se respondió 'y' pero llegó false")
	}
}

func TestApprovalPromptDeny(t *testing.T) {
	var a approvalPrompt
	req := newReq()
	a.open(req)

	if line := a.answer("n"); line == "" || a.active() {
		t.Fatalf("answer(n) = %q / active=%v", line, a.active())
	}
	if got := <-req.Response; got {
		t.Fatal("se respondió 'n' pero llegó true")
	}
}

func TestApprovalPromptIgnoresNonDecisionKeys(t *testing.T) {
	var a approvalPrompt
	a.open(newReq())

	if line := a.answer("x"); line != "" {
		t.Fatalf("una tecla cualquiera devolvió %q", line)
	}
	if !a.active() {
		t.Fatal("una tecla que no decide no debería cerrar la aprobación")
	}
}

func TestApprovalPromptAnswerWithoutPendingIsNoop(t *testing.T) {
	var a approvalPrompt
	if line := a.answer("y"); line != "" {
		t.Fatalf("answer sin pending devolvió %q", line)
	}
}

func TestApprovalPromptEscDenies(t *testing.T) {
	var a approvalPrompt
	req := newReq()
	a.open(req)
	if line := a.answer("esc"); line == "" {
		t.Fatal("esc debería denegar")
	}
	if got := <-req.Response; got {
		t.Fatal("esc respondió true")
	}
}
