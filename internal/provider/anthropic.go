package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultAnthropicModel is used until SetModel (or the /model command) changes it.
const DefaultAnthropicModel = "claude-opus-5"

// Anthropic adapts the official anthropic-sdk-go to the Provider port. It is a
// pure translator: it holds no conversation state, it converts Request -> SDK
// params and SDK Message -> Response on every call.
type Anthropic struct {
	client anthropic.Client
	model  string
}

// NewAnthropic builds the adapter. With no options the SDK reads its credentials
// (ANTHROPIC_API_KEY and the other sources) from the environment.
func NewAnthropic(opts ...option.RequestOption) *Anthropic {
	return &Anthropic{
		client: anthropic.NewClient(opts...),
		model:  DefaultAnthropicModel,
	}
}

func (a *Anthropic) Model() string { return a.model }

func (a *Anthropic) SetModel(model string) {
	if model != "" {
		a.model = model
	}
}

// SendMessage translates the request, calls the Messages API, and normalizes
// the reply back into a Response.
func (a *Anthropic) SendMessage(ctx context.Context, req Request) (Response, error) {
	params, err := a.toParams(req)
	if err != nil {
		return Response{}, err
	}
	msg, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: %w", err)
	}
	return fromMessage(msg), nil
}

// StreamMessage streams the reply, forwarding each text delta to onDelta, and
// returns the fully accumulated Response.
func (a *Anthropic) StreamMessage(ctx context.Context, req Request, onDelta func(string)) (Response, error) {
	params, err := a.toParams(req)
	if err != nil {
		return Response{}, err
	}

	stream := a.client.Messages.NewStreaming(ctx, params)
	var msg anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			return Response{}, fmt.Errorf("anthropic: %w", err)
		}
		if event.Type == "content_block_delta" {
			if d := event.AsContentBlockDelta(); d.Delta.Type == "text_delta" && d.Delta.Text != "" && onDelta != nil {
				onDelta(d.Delta.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return Response{}, fmt.Errorf("anthropic: %w", err)
	}
	return fromMessage(&msg), nil
}

// toParams converts a generic Request into anthropic.MessageNewParams.
func (a *Anthropic) toParams(req Request) (anthropic.MessageNewParams, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	msgs := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			msgs = append(msgs, anthropic.NewUserMessage(userBlocks(m)...))
		case RoleAssistant:
			msgs = append(msgs, anthropic.NewAssistantMessage(assistantBlocks(m)...))
		default:
			return anthropic.MessageNewParams{}, fmt.Errorf("anthropic: rol de mensaje desconocido %q", m.Role)
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: int64(maxTokens),
		Messages:  msgs,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if len(req.Tools) > 0 {
		params.Tools = toToolUnion(req.Tools)
	}
	return params, nil
}

// userBlocks builds the content of a user turn: either tool results, or plain text.
func userBlocks(m Message) []anthropic.ContentBlockParamUnion {
	if len(m.ToolResults) > 0 {
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.ToolResults))
		for _, tr := range m.ToolResults {
			blocks = append(blocks, anthropic.NewToolResultBlock(tr.CallID, tr.Content, tr.IsError))
		}
		return blocks
	}
	return []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(m.Text)}
}

// assistantBlocks rebuilds an assistant turn from our normalized form: optional
// text followed by the tool_use blocks the model previously emitted.
func assistantBlocks(m Message) []anthropic.ContentBlockParamUnion {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(m.ToolCalls))
	if m.Text != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.Text))
	}
	for _, tc := range m.ToolCalls {
		var input any
		if len(tc.Input) > 0 {
			_ = json.Unmarshal(tc.Input, &input)
		}
		blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Name))
	}
	return blocks
}

func toToolUnion(defs []ToolDef) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(defs))
	for _, d := range defs {
		schema := anthropic.ToolInputSchemaParam{}
		if d.InputSchema != nil {
			schema.Properties = d.InputSchema["properties"]
			schema.Required = asStringSlice(d.InputSchema["required"])
		}
		tool := anthropic.ToolParam{
			Name:        d.Name,
			Description: anthropic.String(d.Description),
			InputSchema: schema,
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return out
}

func asStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

// fromMessage normalizes an SDK Message into a Response.
func fromMessage(msg *anthropic.Message) Response {
	var resp Response
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			resp.Text += b.Text
		case anthropic.ToolUseBlock:
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:    b.ID,
				Name:  b.Name,
				Input: json.RawMessage(b.Input),
			})
		}
	}
	resp.StopReason = mapStopReason(msg.StopReason)
	// Invariant for the agent loop: tool calls always mean "run the tools".
	if len(resp.ToolCalls) > 0 {
		resp.StopReason = StopToolUse
	}
	resp.Usage = Usage{
		InputTokens:  int(msg.Usage.InputTokens),
		OutputTokens: int(msg.Usage.OutputTokens),
	}
	return resp
}

func mapStopReason(sr anthropic.StopReason) StopReason {
	switch sr {
	case anthropic.StopReasonToolUse:
		return StopToolUse
	case anthropic.StopReasonMaxTokens, anthropic.StopReasonModelContextWindowExceeded:
		return StopMaxTokens
	default:
		return StopEndTurn
	}
}
