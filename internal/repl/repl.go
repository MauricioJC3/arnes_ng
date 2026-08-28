// Package repl is the plain line-based front-end: read a line, dispatch slash
// commands via internal/command, otherwise hand the line to the conversation.
package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/command"
	"github.com/MauricioJC3/arnes_ng/internal/goal"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// REPL wires a conversation to an input stream and an output stream.
type REPL struct {
	conv     command.Conversation
	provider provider.Provider
	in       *bufio.Reader
	out      io.Writer
}

// New builds a REPL. in must be the single bufio.Reader wrapping stdin, shared
// with the approval prompt.
func New(conv command.Conversation, p provider.Provider, in *bufio.Reader, out io.Writer) *REPL {
	return &REPL{conv: conv, provider: p, in: in, out: out}
}

// Run loops until /exit or EOF. EOF is a clean stop, not an error.
func (r *REPL) Run(ctx context.Context) error {
	fmt.Fprintln(r.out, "arnés — escribí un mensaje, o /help para ver los comandos.")
	for {
		fmt.Fprint(r.out, "\n> ")
		line, err := r.in.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		eof := errors.Is(err, io.EOF)

		if r.handle(ctx, strings.TrimSpace(line)) || eof {
			if eof {
				fmt.Fprintln(r.out)
			}
			return nil
		}
	}
}

// handle processes one line. It returns true when the loop should stop.
func (r *REPL) handle(ctx context.Context, line string) (stop bool) {
	if line == "" {
		return false
	}

	if strings.HasPrefix(line, "/") {
		res, err := command.Dispatch(line, r.conv, r.provider)
		if err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			return false
		}
		if res.Output != "" {
			fmt.Fprintln(r.out, res.Output)
		}
		if res.Goal != nil {
			r.runGoal(ctx, res.Goal)
		}
		return res.Exit
	}

	reply, err := r.conv.Run(ctx, line)
	if err != nil {
		fmt.Fprintf(r.out, "error: %v\n", err)
		return false
	}
	fmt.Fprintln(r.out, reply)
	return false
}

// runGoal runs a Ralph-style loop synchronously, printing the iteration count.
func (r *REPL) runGoal(ctx context.Context, req *command.GoalRequest) {
	cfg := goal.Config{
		MaxIterations: req.MaxIter,
		Progress:      func(n, max int) { fmt.Fprintf(r.out, "— iteración %d/%d —\n", n, max) },
	}
	if req.Fresh {
		if ff, ok := r.conv.(command.FreshFactory); ok {
			cfg.NewConversation = func() goal.Conversation { return ff.FreshConversation() }
		}
	}
	rep, err := goal.Run(ctx, r.conv, req.Text, cfg)
	if err != nil {
		fmt.Fprintf(r.out, "error: %v\n", err)
	}
	fmt.Fprintln(r.out, rep.Summary())
}
