// Package session persists conversations to disk so they can be listed and
// resumed across runs of the harness.
package session

import (
	"strings"
	"time"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

// Session is one persisted conversation.
type Session struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Provider  string             `json:"provider"`
	Model     string             `json:"model"`
	CWD       string             `json:"cwd"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	UsageIn   int                `json:"usage_in,omitempty"`
	UsageOut  int                `json:"usage_out,omitempty"`
	Messages  []provider.Message `json:"messages"`
	// Todos is the task checklist as of the last saved turn, so quitting
	// mid-task and resuming brings the panel back with its ticks intact.
	Todos []todo.Item `json:"todos,omitempty"`
}

// New starts an empty session with a fresh id and timestamps.
func New(providerName, model, cwd string) *Session {
	now := time.Now()
	return &Session{
		ID:        newID(now),
		Provider:  providerName,
		Model:     model,
		CWD:       cwd,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Meta is the lightweight view used to list sessions without loading every message.
type Meta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Model     string    `json:"model"`
	UpdatedAt time.Time `json:"updated_at"`
	Messages  int       `json:"messages"`
	UsageIn   int       `json:"usage_in"`
	UsageOut  int       `json:"usage_out"`
}

// Meta projects the session into its listing view.
func (s *Session) Meta() Meta {
	return Meta{
		ID:        s.ID,
		Title:     s.Title,
		Model:     s.Model,
		UpdatedAt: s.UpdatedAt,
		Messages:  len(s.Messages),
		UsageIn:   s.UsageIn,
		UsageOut:  s.UsageOut,
	}
}

// normalize repairs message shapes that would break json.Marshal. A tool_use
// block truncated by the model can leave ToolCall.Input as an empty
// json.RawMessage, which is invalid JSON; persist it as an empty object.
func (s *Session) normalize() {
	for i := range s.Messages {
		for j := range s.Messages[i].ToolCalls {
			if len(s.Messages[i].ToolCalls[j].Input) == 0 {
				s.Messages[i].ToolCalls[j].Input = []byte("{}")
			}
		}
	}
}

// title derives a short one-line label from the first user message.
func title(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:57]) + "..."
	}
	return s
}
