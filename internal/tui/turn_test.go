package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTurnStartAgentDeliversResult(t *testing.T) {
	tr := newTurn(nil)
	conv := &fakeConv{reply: "hecho"}

	cmd := tr.startAgent(conv, "hacé algo")
	if !tr.busy || tr.cancel == nil {
		t.Fatal("startAgent no dejó el turno en curso")
	}
	if cmd == nil {
		t.Fatal("startAgent no devolvió un Cmd de espera")
	}

	select {
	case res := <-tr.results:
		if res.err != nil || res.text != "hecho" {
			t.Fatalf("resultado inesperado: %+v", res)
		}
	case <-time.After(time.Second):
		t.Fatal("la goroutine no reportó el resultado")
	}
	if conv.seen[0] != "hacé algo" {
		t.Fatalf("conv.Run recibió %v", conv.seen)
	}
}

// panicConv blows up inside Run -- the goroutine must not leave the UI hanging.
type panicConv struct{}

func (panicConv) Run(context.Context, string) (string, error) { panic("boom en el provider") }

func TestTurnStartAgentRecoversPanic(t *testing.T) {
	tr := newTurn(nil)
	tr.startAgent(panicConv{}, "hacé algo")

	select {
	case res := <-tr.results:
		if res.err == nil || !strings.Contains(res.err.Error(), "pánico en el turno") {
			t.Fatalf("un pánico debería volver como error, tengo: %+v", res)
		}
	case <-time.After(time.Second):
		t.Fatal("el pánico congeló el turno: results nunca recibió")
	}
}

func TestTurnInterruptCancelsContext(t *testing.T) {
	tr := newTurn(nil)
	c := &blockingConv{started: make(chan struct{})}

	tr.startAgent(c, "algo largo")
	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("la goroutine no arrancó")
	}

	tr.interrupt()
	select {
	case res := <-tr.results:
		if !errors.Is(res.err, context.Canceled) {
			t.Fatalf("err = %v, quería context.Canceled", res.err)
		}
	case <-time.After(time.Second):
		t.Fatal("interrupt no canceló el turno")
	}
}

func TestTurnEndResetsState(t *testing.T) {
	tr := newTurn(nil)
	tr.busy = true
	tr.cancel = func() {}
	tr.goalIter, tr.goalMax = 3, 7

	tr.end()
	if tr.busy || tr.cancel != nil || tr.goalIter != 0 || tr.goalMax != 0 {
		t.Fatalf("end no limpió el estado: %+v", tr)
	}
}

func TestTurnStepGoalRecordsProgress(t *testing.T) {
	tr := newTurn(nil)
	if cmd := tr.stepGoal(goalStepMsg{2, 5}); cmd == nil {
		t.Fatal("stepGoal no devolvió el Cmd de espera")
	}
	if tr.goalIter != 2 || tr.goalMax != 5 {
		t.Fatalf("stepGoal no guardó el progreso: iter=%d max=%d", tr.goalIter, tr.goalMax)
	}
}
