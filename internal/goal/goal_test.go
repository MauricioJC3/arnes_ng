package goal

import (
	"context"
	"errors"
	"fmt"
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

// countingConv returns a distinct reply each call and reports fixed usage.
type countingConv struct {
	runs int
}

func (c *countingConv) Run(context.Context, string) (string, error) {
	c.runs++
	return fmt.Sprintf("paso %d", c.runs), nil
}
func (c *countingConv) Usage() (int, int) { return 10, 2 }

func TestRunFreshMode(t *testing.T) {
	made := 0
	var prompts []string
	c := &countingConv{}
	rep, err := Run(context.Background(), nil, "hacé Y", Config{
		MaxIterations: 3,
		NewConversation: func() Conversation {
			made++
			return &recordingConv{inner: c, prompts: &prompts}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if made != 3 {
		t.Fatalf("NewConversation llamado %d veces, quiero 3 (una por iteración)", made)
	}
	// tokens: 3 iteraciones × (10 in / 2 out) sumados
	if rep.TokensIn != 30 || rep.TokensOut != 6 {
		t.Fatalf("tokens = %d/%d, quiero 30/6", rep.TokensIn, rep.TokensOut)
	}
	// el prompt fresh incluye la instrucción de ponerse al día
	if len(prompts) == 0 || !strings.Contains(prompts[0], "git status") {
		t.Fatalf("el prompt fresh no tiene la instrucción de catch-up: %q", prompts)
	}
}

// recordingConv wraps a Conversation to record the prompts and forward Usage.
type recordingConv struct {
	inner   *countingConv
	prompts *[]string
}

func (r *recordingConv) Run(ctx context.Context, in string) (string, error) {
	*r.prompts = append(*r.prompts, in)
	return r.inner.Run(ctx, in)
}
func (r *recordingConv) Usage() (int, int) { return r.inner.Usage() }

func TestSentinelMustBeOwnLine(t *testing.T) {
	// mencionarlo en prosa NO cuenta
	c := &scriptConv{replies: []string{"voy a responder ARNES_GOAL_DONE cuando termine", "listo\nARNES_GOAL_DONE"}}
	rep, _ := Run(context.Background(), c, "x", Config{})
	if rep.Iterations != 2 || rep.Reason != "completado" {
		t.Fatalf("report = %+v (no debería completar en la iteración 1)", rep)
	}
}
