// Package provider abstracts any LLM behind one interface so the agent loop
// never depends on a concrete SDK (Anthropic, OpenAI, DeepSeek, Kimi...).
package provider

import (
	"context"
	"encoding/json"
)

// Role identifies who produced a message in the conversation history.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// StopReason tells the agent loop why the model stopped generating.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"   // model is done, hand text back to the user
	StopToolUse   StopReason = "tool_use"   // model wants the harness to run tool(s)
	StopMaxTokens StopReason = "max_tokens" // model hit its output budget
)

// Message is one turn in the conversation history. A single message carries
// EITHER plain text, OR tool calls (assistant asking the harness to act), OR
// tool results (harness reporting back). Mixed shapes are allowed to match
// provider wire formats.
type Message struct {
	Role        Role         `json:"role"`
	Text        string       `json:"text,omitempty"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`   // set on assistant turns that request tools
	ToolResults []ToolResult `json:"tool_results,omitempty"` // set on user turns that carry tool output
}

// ToolCall is the model asking the harness to run one tool.
type ToolCall struct {
	ID    string          `json:"id"`              // provider-assigned id; the matching result must echo it
	Name  string          `json:"name"`            // which tool
	Input json.RawMessage `json:"input,omitempty"` // raw JSON arguments, validated by the tool itself
}

// ToolResult is what the harness sends back after running (or refusing) a tool.
type ToolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// ToolDef is how a tool is described to the model.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Usage is the token accounting for one call (feeds the cost status bar later).
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Request is everything the harness sends for one model call.
type Request struct {
	System    string
	Messages  []Message
	Tools     []ToolDef
	MaxTokens int
}

// Response is the normalized reply, independent of the underlying SDK.
type Response struct {
	Text       string
	ToolCalls  []ToolCall
	StopReason StopReason
	Usage      Usage
}

// Provider is the single seam between the agent loop and any LLM backend.
type Provider interface {
	Model() string
	SetModel(model string)
	SendMessage(ctx context.Context, req Request) (Response, error)
}

// Streamer is an optional Provider capability: emit text as it is generated.
// onDelta is called with each text chunk; the returned Response is the full
// aggregated result, identical in shape to SendMessage's.
type Streamer interface {
	StreamMessage(ctx context.Context, req Request, onDelta func(delta string)) (Response, error)
}
