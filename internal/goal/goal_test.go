package goal

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scriptConv returns the next canned reply each Run, recording the prompts it saw.
type scriptConv struct {
	replies []string
	i       int
	prompts []string
	err     error
}

func (c *scriptConv) Run(_ context.Context, in string) (string, error) {
	c.prompts = append(c.prompts, in)
	if c.err != nil {
		return "", c.err
	}
	r := ""
	if c.i < len(c.replies) {
		r = c.replies[c.i]
	}
	c.i++
	return r, nil
}

func TestRunCompletesOnSentinel(t *testing.T) {
	c := &scriptConv{replies: []string{"trabajando...", "casi\nARNES_GOAL_DONE"}}
	var steps [][2]int
	rep, err := Run(context.Background(), c, "hacé X", Config{
		Progress: func(n, max int) { steps = append(steps, [2]int{n, max}) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reason != "completado" || rep.Iterations != 2 {
		t.Fatalf("report = %+v", rep)
	}
	if len(steps) != 2 || steps[0] != [2]int{1, DefaultMaxIterations} {
		t.Fatalf("progress = %v", steps)
	}
	// el prompt es el mismo en cada iteración
	if c.prompts[0] != c.prompts[1] || !strings.Contains(c.prompts[0], "OBJETIVO: hacé X") {
		t.Fatalf("prompts distintos o mal armados: %q", c.prompts)
	}
}

func TestRunStopsAtMaxIterations(t *testing.T) {
	c := &scriptConv{replies: []string{"1", "2", "3", "4", "5"}}
	rep, _ := Run(context.Background(), c, "x", Config{MaxIterations: 3})
	if rep.Iterations != 3 || !strings.Contains(rep.Reason, "límite de 3") {
		t.Fatalf("report = %+v", rep)
	}
}

func TestRunStopsOnNoProgress(t *testing.T) {
	c := &scriptConv{replies: []string{"mismo texto", "mismo texto"}}
	rep, _ := Run(context.Background(), c, "x", Config{MaxIterations: 10})
	if rep.Iterations != 2 || !strings.Contains(rep.Reason, "sin progreso") {
		t.Fatalf("report = %+v", rep)
	}
}

func TestRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &scriptConv{err: context.Canceled}
	cancel()
	rep, err := Run(ctx, c, "x", Config{})
	if err != nil {
		t.Fatalf("una cancelación no debería devolver error: %v", err)
	}
	if rep.Reason != "cancelado" {
		t.Fatalf("report = %+v", rep)
	}
}

func TestRunPropagatesRealError(t *testing.T) {
	c := &scriptConv{err: errors.New("503")}
	rep, err := Run(context.Background(), c, "x", Config{})
	if err == nil || rep.Reason != "error" {
		t.Fatalf("err=%v report=%+v", err, rep)
	}
}

func TestSentinelMustBeOwnLine(t *testing.T) {
	// mencionarlo en prosa NO cuenta
	c := &scriptConv{replies: []string{"voy a responder ARNES_GOAL_DONE cuando termine", "listo\nARNES_GOAL_DONE"}}
	rep, _ := Run(context.Background(), c, "x", Config{})
	if rep.Iterations != 2 || rep.Reason != "completado" {
		t.Fatalf("report = %+v (no debería completar en la iteración 1)", rep)
	}
}
