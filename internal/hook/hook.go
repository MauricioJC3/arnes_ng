// Package hook runs user-configured shell commands around tool calls: a
// pre-tool hook can block a call (e.g. run tests before `git commit`), a
// post-tool hook reacts to one (e.g. `gofmt -w` after an edit). Hooks are
// declared in ~/.arnes/hooks.json.
package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

// Hook is one command bound to a set of tools by a name regex.
type Hook struct {
	// Match is a regexp tested against the tool name. Empty matches every tool.
	Match string `json:"match"`
	// Command is run through `sh -c`. It gets the tool-call JSON on stdin and
	// ARNES_TOOL_NAME in the environment.
	Command string `json:"command"`
	// Block, for a pre-tool hook, makes a non-zero exit cancel the tool call
	// (the hook output becomes the reason fed back to the model). Ignored for
	// post-tool hooks. Defaults to true; set "block": false to only warn.
	Block *bool `json:"block,omitempty"`

	re *regexp.Regexp
}

func (h Hook) blocks() bool { return h.Block == nil || *h.Block }

func (h Hook) matches(tool string) bool {
	if h.re == nil {
		return true
	}
	return h.re.MatchString(tool)
}

// Config is the hooks.json shape.
type Config struct {
	PreTool  []Hook `json:"pre_tool"`
	PostTool []Hook `json:"post_tool"`
}

// DefaultPath is ~/.arnes/hooks.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arnes", "hooks.json"), nil
}

// LoadFile reads hooks.json. A missing file yields an empty config.
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("%s inválido: %w", path, err)
	}
	if err := compile(c.PreTool); err != nil {
		return Config{}, err
	}
	if err := compile(c.PostTool); err != nil {
		return Config{}, err
	}
	return c, nil
}

func compile(hooks []Hook) error {
	for i := range hooks {
		if strings.TrimSpace(hooks[i].Command) == "" {
			return fmt.Errorf("hook %d sin 'command'", i+1)
		}
		if hooks[i].Match == "" {
			continue
		}
		re, err := regexp.Compile(hooks[i].Match)
		if err != nil {
			return fmt.Errorf("hook %d: 'match' %q inválido: %w", i+1, hooks[i].Match, err)
		}
		hooks[i].re = re
	}
	return nil
}

// Empty reports whether there are no hooks at all.
func (c Config) Empty() bool { return len(c.PreTool) == 0 && len(c.PostTool) == 0 }

// Runner executes the configured hooks. It implements agent.Hooks.
type Runner struct {
	cfg     Config
	timeout time.Duration
	// dir is the working directory for hook commands (default: process cwd).
	dir string
}

// New builds a Runner. A zero timeout means 30s.
func New(cfg Config, timeout time.Duration) *Runner {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Runner{cfg: cfg, timeout: timeout}
}

// PreTool runs every matching pre-tool hook in order. The first blocking hook
// that exits non-zero returns an error, which cancels the tool call.
func (r *Runner) PreTool(ctx context.Context, call provider.ToolCall) error {
	for _, h := range r.cfg.PreTool {
		if !h.matches(call.Name) {
			continue
		}
		out, err := r.run(ctx, h, call)
		if err == nil {
			continue
		}
		if h.blocks() {
			msg := strings.TrimSpace(out)
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("hook %q bloqueó %s: %s", h.Command, call.Name, msg)
		}
	}
	return nil
}

// PostTool runs every matching post-tool hook in order and returns their
// combined output as a note to append to the tool result (empty when the hooks
// produced nothing).
func (r *Runner) PostTool(ctx context.Context, call provider.ToolCall, _ string, _ bool) string {
	var notes []string
	for _, h := range r.cfg.PostTool {
		if !h.matches(call.Name) {
			continue
		}
		out, err := r.run(ctx, h, call)
		out = strings.TrimSpace(out)
		switch {
		case err != nil && out != "":
			notes = append(notes, fmt.Sprintf("%s (exit != 0): %s", h.Command, out))
		case err != nil:
			notes = append(notes, fmt.Sprintf("%s: %v", h.Command, err))
		case out != "":
			notes = append(notes, fmt.Sprintf("%s: %s", h.Command, out))
		}
	}
	return strings.Join(notes, "\n")
}

// run executes one hook command with the tool-call JSON on stdin. It returns the
// combined stdout+stderr and a non-nil error on non-zero exit or timeout.
func (r *Runner) run(ctx context.Context, h Hook, call provider.ToolCall) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", h.Command)
	cmd.Dir = r.dir
	cmd.Stdin = bytes.NewReader(payload(call))
	cmd.Env = append(os.Environ(), "ARNES_TOOL_NAME="+call.Name)
	// On timeout the shell is killed, but a still-running descendant (e.g. a
	// `sleep` the shell spawned) can keep the output pipe open and make Wait
	// block until it exits. WaitDelay caps that: Wait returns shortly after the
	// kill regardless.
	cmd.WaitDelay = 2 * time.Second

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return buf.String(), fmt.Errorf("timeout tras %s", r.timeout)
	}
	return buf.String(), err
}

func payload(call provider.ToolCall) []byte {
	b, err := json.Marshal(map[string]any{
		"tool":  call.Name,
		"input": json.RawMessage(call.Input),
	})
	if err != nil {
		return []byte("{}")
	}
	return b
}
