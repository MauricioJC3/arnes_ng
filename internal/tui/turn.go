package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MauricioJC3/arnes_ng/internal/command"
	goalpkg "github.com/MauricioJC3/arnes_ng/internal/goal"
)

// turn owns the lifecycle of the in-flight agent work: whether something is
// running, how to cancel it, the channels the background goroutine reports back
// on, and the progress counters for a /goal loop.
type turn struct {
	busy    bool
	cancel  context.CancelFunc // cancels the running agent turn / goal loop
	results chan runResult     // the goroutine posts its outcome here
	deltas  chan string        // streamed text chunks; nil when streaming is off

	goalCh   chan goalStepMsg
	goalIter int // >0 while a /goal loop is running
	goalMax  int
}

func newTurn(deltas chan string) turn {
	return turn{
		results: make(chan runResult, 1),
		goalCh:  make(chan goalStepMsg, 4),
		deltas:  deltas,
	}
}

// startAgent runs one plain agent turn in a cancellable goroutine and returns
// the command that waits for its result.
func (t *turn) startAgent(conv command.Conversation, text string) tea.Cmd {
	t.busy = true
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	results := t.results
	go func() {
		out, err := conv.Run(ctx, text)
		results <- runResult{text: out, err: err}
	}()
	return waitForResult(results)
}

// startGoal kicks off a Ralph-style goal loop as a cancellable background turn
// and returns the commands that wait for its progress and its result.
func (t *turn) startGoal(conv command.Conversation, req *command.GoalRequest) tea.Cmd {
	t.busy = true
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	results, ch := t.results, t.goalCh

	cfg := goalpkg.Config{
		MaxIterations: req.MaxIter,
		Progress:      func(n, max int) { ch <- goalStepMsg{n, max} },
	}
	if req.Fresh {
		if ff, ok := conv.(command.FreshFactory); ok {
			cfg.NewConversation = func() goalpkg.Conversation { return ff.FreshConversation() }
		}
	}

	go func() {
		rep, err := goalpkg.Run(ctx, conv, req.Text, cfg)
		results <- runResult{goal: &rep, err: err}
	}()
	return tea.Batch(waitForResult(results), waitForGoal(ch))
}

// stepGoal records a new goal iteration and re-arms the progress wait.
func (t *turn) stepGoal(msg goalStepMsg) tea.Cmd {
	t.goalIter, t.goalMax = msg[0], msg[1]
	return waitForGoal(t.goalCh)
}

// interrupt cancels the running work, if any. It never clears busy -- that
// happens when the resulting context.Canceled comes back through end().
func (t *turn) interrupt() {
	if t.cancel != nil {
		t.cancel()
	}
}

// end resets the turn state once the goroutine has reported back.
func (t *turn) end() {
	t.busy = false
	t.cancel = nil
	t.goalIter, t.goalMax = 0, 0
}

// awaitDelta re-arms the wait for the next streamed chunk.
func (t *turn) awaitDelta() tea.Cmd { return waitForDelta(t.deltas) }

// waitFor* are tea.Cmds that block on a channel.

func waitForResult(ch chan runResult) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func waitForDelta(ch chan string) tea.Cmd {
	return func() tea.Msg { return deltaMsg(<-ch) }
}

func waitForGoal(ch chan goalStepMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}
