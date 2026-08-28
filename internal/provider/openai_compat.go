package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Base URLs for the well-known OpenAI-compatible providers.
const (
	DeepSeekBaseURL = "https://api.deepseek.com/v1"
	KimiBaseURL     = "https://api.moonshot.ai/v1"
	OpenAIBaseURL   = "https://api.openai.com/v1"
)

// OpenAICompat talks to any service that implements the OpenAI Chat Completions
// API: OpenAI itself, DeepSeek, Kimi (Moonshot) and most local runners. It is a
// hand-rolled HTTP client -- no SDK -- because the wire format we need is small
// and stable.
type OpenAICompat struct {
	http    *http.Client
	baseURL string
	apiKey  string
	model   string
}

// OpenAICompatConfig configures the adapter. BaseURL and Model are required;
// APIKey is required by hosted providers; HTTP defaults to a 120s client.
type OpenAICompatConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// NewOpenAICompat builds the adapter from an explicit config.
func NewOpenAICompat(cfg OpenAICompatConfig) *OpenAICompat {
	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 120 * time.Second}
	}
	return &OpenAICompat{
		http:    httpClient,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
	}
}

// NewDeepSeek, NewKimi and NewOpenAI are convenience constructors over
// NewOpenAICompat with the provider's base URL filled in.
func NewDeepSeek(apiKey, model string) *OpenAICompat {
	return NewOpenAICompat(OpenAICompatConfig{BaseURL: DeepSeekBaseURL, APIKey: apiKey, Model: model})
}

func NewKimi(apiKey, model string) *OpenAICompat {
	return NewOpenAICompat(OpenAICompatConfig{BaseURL: KimiBaseURL, APIKey: apiKey, Model: model})
}

func NewOpenAI(apiKey, model string) *OpenAICompat {
	return NewOpenAICompat(OpenAICompatConfig{BaseURL: OpenAIBaseURL, APIKey: apiKey, Model: model})
}

func (o *OpenAICompat) Model() string { return o.model }

func (o *OpenAICompat) SetModel(model string) {
	if model != "" {
		o.model = model
	}
}

// retry tuning (package vars so tests can shrink the backoff).
var (
	retryAttempts = 4
	retryBase     = 500 * time.Millisecond
	retryCap      = 8 * time.Second
)

// post sends the marshalled payload to /chat/completions, retrying 429 and 5xx
// (and connection errors) with exponential backoff + jitter, honoring a
// Retry-After header when present. The caller closes the returned body.
func (o *OpenAICompat) post(ctx context.Context, body []byte, accept string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(backoff(attempt)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		if o.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+o.apiKey)
		}

		resp, err := o.http.Do(req)
		if err != nil {
			lastErr = err
			continue // connection error -> retry
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			wait := retryAfter(resp)
			resp.Body.Close()
			lastErr = fmt.Errorf("openai_compat: HTTP %d", resp.StatusCode)
			if wait > 0 && attempt < retryAttempts-1 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			continue
		}
		return resp, nil // success, or a non-retryable 4xx
	}
	return nil, fmt.Errorf("openai_compat: sin respuesta tras %d intentos: %w", retryAttempts, lastErr)
}

func backoff(attempt int) time.Duration {
	d := retryBase << (attempt - 1)
	if d > retryCap {
		d = retryCap
	}
	jitter := time.Duration(rand.Int63n(int64(d)/2 + 1))
	return d/2 + jitter
}

func retryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}

// SendMessage builds the chat-completions payload, POSTs it, and normalizes the
// reply into a Response.
func (o *OpenAICompat) SendMessage(ctx context.Context, req Request) (Response, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	payload := ocChatRequest{
		Model:     o.model,
		Messages:  toOCMessages(req),
		MaxTokens: maxTokens,
	}
	if len(req.Tools) > 0 {
		payload.Tools = toOCTools(req.Tools)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	httpResp, err := o.post(ctx, body, "")
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("openai_compat: no se pudo leer la respuesta: %w", err)
	}

	var parsed ocChatResponse
	if jsonErr := json.Unmarshal(raw, &parsed); jsonErr != nil {
		return Response{}, fmt.Errorf("openai_compat: respuesta ilegible (HTTP %d): %s", httpResp.StatusCode, truncate(raw, 300))
	}
	if httpResp.StatusCode >= 400 {
		msg := string(truncate(raw, 300))
		if parsed.Error != nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		return Response{}, fmt.Errorf("openai_compat: HTTP %d: %s", httpResp.StatusCode, msg)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("openai_compat: la respuesta no trae choices")
	}
	return fromOCResponse(parsed), nil
}

// StreamMessage streams the reply over SSE, forwarding each text delta to
// onDelta, and returns the aggregated Response.
func (o *OpenAICompat) StreamMessage(ctx context.Context, req Request, onDelta func(string)) (Response, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	payload := ocChatRequest{
		Model:         o.model,
		Messages:      toOCMessages(req),
		MaxTokens:     maxTokens,
		Stream:        true,
		StreamOptions: &ocStreamOpts{IncludeUsage: true},
	}
	if len(req.Tools) > 0 {
		payload.Tools = toOCTools(req.Tools)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	// Retry only the connection + initial status; once bytes are streaming a
	// mid-stream failure can't be resumed.
	httpResp, err := o.post(ctx, body, "text/event-stream")
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		raw, _ := io.ReadAll(httpResp.Body)
		return Response{}, fmt.Errorf("openai_compat: HTTP %d: %s", httpResp.StatusCode, truncate(raw, 300))
	}
	return parseOCStream(httpResp.Body, onDelta)
}

// parseOCStream consumes an OpenAI-style SSE body: `data: {json}` lines ended by
// `data: [DONE]`. Tool-call fragments are keyed by index and concatenated.
func parseOCStream(body io.Reader, onDelta func(string)) (Response, error) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	type tcAcc struct {
		id, name string
		args     strings.Builder
	}
	accs := map[int]*tcAcc{}
	var order []int
	var resp Response
	finish := ""

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if chunk.Usage.CompletionTokens > 0 || chunk.Usage.PromptTokens > 0 {
			resp.Usage = Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]
		if ch.Delta.Content != "" {
			resp.Text += ch.Delta.Content
			if onDelta != nil {
				onDelta(ch.Delta.Content)
			}
		}
		for _, tc := range ch.Delta.ToolCalls {
			acc := accs[tc.Index]
			if acc == nil {
				acc = &tcAcc{}
				accs[tc.Index] = acc
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			acc.args.WriteString(tc.Function.Arguments)
		}
		if ch.FinishReason != "" {
			finish = ch.FinishReason
		}
	}
	if err := sc.Err(); err != nil {
		return Response{}, fmt.Errorf("openai_compat: stream: %w", err)
	}

	for _, idx := range order {
		acc := accs[idx]
		args := acc.args.String()
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{ID: acc.id, Name: acc.name, Input: json.RawMessage(args)})
	}
	resp.StopReason = mapOCFinishReason(finish)
	if len(resp.ToolCalls) > 0 {
		resp.StopReason = StopToolUse
	}
	return resp, nil
}

// --- wire types -------------------------------------------------------------

type ocChatRequest struct {
	Model         string        `json:"model"`
	Messages      []ocMessage   `json:"messages"`
	Tools         []ocTool      `json:"tools,omitempty"`
	MaxTokens     int           `json:"max_tokens,omitempty"`
	Stream        bool          `json:"stream,omitempty"`
	StreamOptions *ocStreamOpts `json:"stream_options,omitempty"`
}

type ocStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type ocMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content,omitempty"`
	ToolCalls  []ocToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type ocToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"` // always "function"
	Function ocFunctionCall `json:"function"`
}

type ocFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON, encoded as a string
}

type ocTool struct {
	Type     string     `json:"type"` // "function"
	Function ocFunction `json:"function"`
}

type ocFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ocChatResponse struct {
	Choices []struct {
		FinishReason string    `json:"finish_reason"`
		Message      ocMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// --- translation ----------------------------------------------------------

func toOCMessages(req Request) []ocMessage {
	msgs := make([]ocMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, ocMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if len(m.ToolResults) > 0 {
				// OpenAI wants one message per tool result, role "tool".
				for _, tr := range m.ToolResults {
					msgs = append(msgs, ocMessage{Role: "tool", ToolCallID: tr.CallID, Content: tr.Content})
				}
				continue
			}
			msgs = append(msgs, ocMessage{Role: "user", Content: m.Text})
		case RoleAssistant:
			am := ocMessage{Role: "assistant", Content: m.Text}
			for _, tc := range m.ToolCalls {
				am.ToolCalls = append(am.ToolCalls, ocToolCall{
					ID:       tc.ID,
					Type:     "function",
					Function: ocFunctionCall{Name: tc.Name, Arguments: string(tc.Input)},
				})
			}
			msgs = append(msgs, am)
		}
	}
	return msgs
}

func toOCTools(defs []ToolDef) []ocTool {
	out := make([]ocTool, 0, len(defs))
	for _, d := range defs {
		out = append(out, ocTool{
			Type: "function",
			Function: ocFunction{
				Name:        d.Name,
				Description: d.Description,
				Parameters:  d.InputSchema,
			},
		})
	}
	return out
}

func fromOCResponse(r ocChatResponse) Response {
	choice := r.Choices[0]
	resp := Response{
		Text:       choice.Message.Content,
		StopReason: mapOCFinishReason(choice.FinishReason),
		Usage: Usage{
			InputTokens:  r.Usage.PromptTokens,
			OutputTokens: r.Usage.CompletionTokens,
		},
	}
	for _, tc := range choice.Message.ToolCalls {
		args := tc.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(args),
		})
	}
	// Invariant for the agent loop: tool calls always mean "run the tools".
	if len(resp.ToolCalls) > 0 {
		resp.StopReason = StopToolUse
	}
	return resp
}

func mapOCFinishReason(fr string) StopReason {
	switch fr {
	case "tool_calls", "function_call":
		return StopToolUse
	case "length":
		return StopMaxTokens
	default:
		return StopEndTurn
	}
}

func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}
