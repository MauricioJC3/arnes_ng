package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Base URLs for the well-known OpenAI-compatible providers.
const (
	DeepSeekBaseURL = "https://api.deepseek.com/v1"
	KimiBaseURL     = "https://api.moonshot.ai/v1"
	OpenAIBaseURL   = "https://api.openai.com/v1"
	// NVIDIABaseURL is NVIDIA's hosted NIM catalog (build.nvidia.com). It speaks
	// the OpenAI Chat Completions API and, at time of writing, serves its models
	// on a free tier -- an nvapi- key from build.nvidia.com, low rate limits.
	NVIDIABaseURL = "https://integrate.api.nvidia.com/v1"
	// OpenCodeBaseURL is the opencode zen gateway's OpenAI-compatible path. Its
	// *-free models cost nothing (response `cost: "0"`) and work without a card
	// on file, but the free tier rate-limits hard (429 FreeUsageLimitError).
	OpenCodeBaseURL = "https://opencode.ai/zen/v1"
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
// APIKey is required by hosted providers; HTTP defaults to defaultHTTPClient().
type OpenAICompatConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// HTTP timeouts (package vars so tests can shrink them). A streaming turn on a
// heavy coding task legitimately runs for minutes, so there is NO whole-request
// timeout on the client -- that also caps the body read and was killing long
// streams with "context deadline exceeded ... while reading body". Liveness
// instead comes from: the caller's context (Ctrl+C), a per-connection dial
// timeout, a whole-call context deadline on non-streaming requests
// (requestTimeout), and a stall watchdog on streams (streamIdleTimeout).
var (
	requestTimeout    = 5 * time.Minute
	streamIdleTimeout = 3 * time.Minute
	dialTimeout       = 30 * time.Second
)

// defaultHTTPClient builds the client used when the caller supplies none. It has
// no http.Client.Timeout on purpose (see the note above).
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// NewOpenAICompat builds the adapter from an explicit config.
func NewOpenAICompat(cfg OpenAICompatConfig) *OpenAICompat {
	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = defaultHTTPClient()
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

func NewNVIDIA(apiKey, model string) *OpenAICompat {
	return NewOpenAICompat(OpenAICompatConfig{BaseURL: NVIDIABaseURL, APIKey: apiKey, Model: model})
}

func NewOpenCode(apiKey, model string) *OpenAICompat {
	return NewOpenAICompat(OpenAICompatConfig{BaseURL: OpenCodeBaseURL, APIKey: apiKey, Model: model})
}

func (o *OpenAICompat) Model() string { return o.model }

// setMaxTokens writes the output-token cap under the parameter name the target
// endpoint accepts. OpenAI's API deprecated `max_tokens` for chat completions
// and its newer models (gpt-5.x, the o-series) reject it outright, requiring
// `max_completion_tokens`. DeepSeek, Kimi, NVIDIA, OpenCode and local runners
// still expect `max_tokens` and may not understand the newer name, so the
// switch is gated on the OpenAI host only.
func (o *OpenAICompat) setMaxTokens(p *ocChatRequest, n int) {
	if strings.Contains(o.baseURL, "api.openai.com") {
		p.MaxCompletionTokens = n
		return
	}
	p.MaxTokens = n
}

func (o *OpenAICompat) SetModel(model string) {
	if model != "" {
		o.model = model
	}
}

// ListModels calls GET /models and returns the model ids. OpenAI's own endpoint
// also returns embedding, audio, image and moderation models, so for that base
// URL the list is filtered to the chat-capable families; the other providers
// return a short list that is passed through as-is. Results are sorted.
func (o *OpenAICompat) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai_compat: no se pudo leer la lista de modelos: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai_compat: HTTP %d: %s", resp.StatusCode, truncate(raw, 200))
	}

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("openai_compat: lista de modelos ilegible: %w", err)
	}

	filter := o.baseURL == OpenAIBaseURL
	ids := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.ID == "" || (filter && !isOpenAIChatModel(m.ID)) {
			continue
		}
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

// isOpenAIChatModel keeps the model families that work with /chat/completions
// and drops everything else OpenAI's /models returns.
func isOpenAIChatModel(id string) bool {
	for _, p := range []string{"gpt-", "chatgpt-", "o1", "o3", "o4-"} {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
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
	// The client has no whole-request timeout (streams need that); bound the
	// non-streaming call here instead.
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	payload := ocChatRequest{
		Model:    o.model,
		Messages: toOCMessages(req),
	}
	o.setMaxTokens(&payload, maxTokens)
	if len(req.Tools) > 0 {
		payload.Tools = toOCTools(req.Tools)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	// A flaky gateway (free tiers especially) can answer 2xx with an HTML error
	// page instead of JSON. post() only retries 429/5xx, so retry the
	// non-JSON-body case here a couple of times before giving up.
	var raw []byte
	var status int
	for attempt := 0; attempt < retryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(backoff(attempt)):
			case <-ctx.Done():
				return Response{}, ctx.Err()
			}
		}
		httpResp, postErr := o.post(ctx, body, "")
		if postErr != nil {
			return Response{}, postErr
		}
		raw, err = io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		if err != nil {
			return Response{}, fmt.Errorf("openai_compat: no se pudo leer la respuesta: %w", err)
		}
		status = httpResp.StatusCode
		if status < 400 && !looksLikeJSON(raw) {
			continue // transient gateway page; retry
		}
		break
	}

	var parsed ocChatResponse
	if jsonErr := json.Unmarshal(raw, &parsed); jsonErr != nil {
		return Response{}, fmt.Errorf("openai_compat: respuesta ilegible (HTTP %d): %s", status, truncate(raw, 300))
	}
	if status >= 400 {
		msg := string(truncate(raw, 300))
		if parsed.Error != nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		return Response{}, fmt.Errorf("openai_compat: HTTP %d: %s", status, msg)
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
		maxTokens = 8192
	}
	payload := ocChatRequest{
		Model:         o.model,
		Messages:      toOCMessages(req),
		Stream:        true,
		StreamOptions: &ocStreamOpts{IncludeUsage: true},
	}
	o.setMaxTokens(&payload, maxTokens)
	if len(req.Tools) > 0 {
		payload.Tools = toOCTools(req.Tools)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	// A cancellable context the stall watchdog can trip -- the HTTP request must
	// be made with THIS context so cancelling it actually unblocks the body read.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var stalled atomic.Bool
	trip := func() { stalled.Store(true); cancel() }

	// Guard connect + time-to-first-byte too: with no client Timeout, o.post
	// would otherwise block forever on a server that accepts the socket but
	// never sends response headers. Stopped as soon as post returns.
	ttfb := time.AfterFunc(streamIdleTimeout, trip)

	// Retry only the connection + initial status; once bytes are streaming a
	// mid-stream failure can't be resumed.
	httpResp, err := o.post(ctx, body, "text/event-stream")
	ttfb.Stop()
	if err != nil {
		if stalled.Load() {
			return Response{}, fmt.Errorf("openai_compat: el proveedor no respondió en %s -- reintentá o cambiá de modelo con /connect", streamIdleTimeout)
		}
		return Response{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		raw, _ := io.ReadAll(httpResp.Body)
		return Response{}, fmt.Errorf("openai_compat: HTTP %d: %s", httpResp.StatusCode, truncate(raw, 300))
	}

	sr := newStallReader(httpResp.Body, streamIdleTimeout, trip)
	defer sr.Stop()
	resp, err := parseOCStream(sr, onDelta)
	if err != nil {
		if stalled.Load() {
			return Response{}, fmt.Errorf("openai_compat: el stream se cortó, no llegaron datos nuevos en %s "+
				"(el modelo o el proveedor dejó de responder) -- reintentá o cambiá de modelo con /connect", streamIdleTimeout)
		}
		// A mid-stream network drop (connection reset, EOF) can't be resumed.
		return Response{}, fmt.Errorf("%w -- el proveedor cortó el stream a mitad de la respuesta; reintentá o cambiá de modelo con /connect", err)
	}
	return resp, err
}

// stallReader wraps a streaming body and calls trip() if no bytes arrive for
// idle. Each successful read pushes the deadline out, so a healthy multi-minute
// stream is never cut -- only a silently stalled one.
type stallReader struct {
	r     io.Reader
	idle  time.Duration
	timer *time.Timer
	done  atomic.Bool
}

func newStallReader(r io.Reader, idle time.Duration, trip func()) *stallReader {
	s := &stallReader{r: r, idle: idle}
	s.timer = time.AfterFunc(idle, func() { s.done.Store(true); trip() })
	return s
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 && !s.done.Load() {
		s.timer.Reset(s.idle)
	}
	return n, err
}

func (s *stallReader) Stop() { s.timer.Stop() }

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
	sawData := false

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawData = true
			break
		}
		sawData = true

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
	// A 2xx whose body carried no SSE frames at all (an HTML error page, an
	// empty body, a proxy that closed early) would otherwise return a silent
	// empty answer. Surface it instead.
	if !sawData && resp.Text == "" && len(order) == 0 {
		return Response{}, fmt.Errorf("openai_compat: el stream no devolvió datos (respuesta vacía o ilegible del proveedor)")
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
	dropTruncatedToolCall(&resp, finish)
	if len(resp.ToolCalls) > 0 {
		resp.StopReason = StopToolUse
	}
	return resp, nil
}

// --- wire types -------------------------------------------------------------

type ocChatRequest struct {
	Model    string      `json:"model"`
	Messages []ocMessage `json:"messages"`
	Tools    []ocTool    `json:"tools,omitempty"`
	// Exactly one of the two token caps is populated per request -- see
	// (*OpenAICompat).setMaxTokens. OpenAI's own API rejects the legacy
	// `max_tokens` on its newer models and demands `max_completion_tokens`;
	// every other OpenAI-compatible backend still speaks `max_tokens`.
	MaxTokens           int           `json:"max_tokens,omitempty"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
	Stream              bool          `json:"stream,omitempty"`
	StreamOptions       *ocStreamOpts `json:"stream_options,omitempty"`
}

type ocStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

// ocMessage is one chat message on the wire. Content is a *string, always
// serialized (no omitempty): a nil pointer emits `"content":null` -- required by
// strict deserializers for an assistant turn that is only tool calls -- while a
// non-nil pointer emits the string, `""` included (an empty tool result still
// needs the key present).
type ocMessage struct {
	Role       string       `json:"role"`
	Content    *string      `json:"content"`
	ToolCalls  []ocToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

func ocStr(s string) *string { return &s }

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
		msgs = append(msgs, ocMessage{Role: "system", Content: ocStr(req.System)})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if len(m.ToolResults) > 0 {
				// OpenAI wants one message per tool result, role "tool".
				for _, tr := range m.ToolResults {
					msgs = append(msgs, ocMessage{Role: "tool", ToolCallID: tr.CallID, Content: ocStr(tr.Content)})
				}
				continue
			}
			msgs = append(msgs, ocMessage{Role: "user", Content: ocStr(m.Text)})
		case RoleAssistant:
			// A tool-only assistant turn keeps Content nil -> `"content":null`.
			am := ocMessage{Role: "assistant"}
			if m.Text != "" {
				am.Content = ocStr(m.Text)
			}
			for _, tc := range m.ToolCalls {
				am.ToolCalls = append(am.ToolCalls, ocToolCall{
					ID:       tc.ID,
					Type:     "function",
					Function: ocFunctionCall{Name: tc.Name, Arguments: string(tc.Input)},
				})
			}
			// Drop an assistant turn that carries nothing: the API rejects
			// {"role":"assistant","content":null} with no tool_calls, and once
			// such a message is in a persisted session every later call fails.
			if am.Content == nil && len(am.ToolCalls) == 0 {
				continue
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
	text := ""
	if choice.Message.Content != nil {
		text = *choice.Message.Content
	}
	resp := Response{
		Text:       text,
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
	dropTruncatedToolCall(&resp, choice.FinishReason)
	// Invariant for the agent loop: tool calls always mean "run the tools".
	if len(resp.ToolCalls) > 0 {
		resp.StopReason = StopToolUse
	}
	return resp
}

// dropTruncatedToolCall handles a completion cut off mid tool call: on a
// "length" finish the last tool call's JSON arguments are chopped, so running it
// only feeds the model a parse error it retries into. Drop that call and let the
// turn stay a max-tokens stop, so the agent nudges the model to go smaller
// instead of dispatching a broken call.
func dropTruncatedToolCall(resp *Response, finishReason string) {
	if finishReason != "length" || len(resp.ToolCalls) == 0 {
		return
	}
	if last := resp.ToolCalls[len(resp.ToolCalls)-1]; !json.Valid(last.Input) {
		resp.ToolCalls = resp.ToolCalls[:len(resp.ToolCalls)-1]
	}
	if len(resp.ToolCalls) == 0 {
		resp.StopReason = StopMaxTokens
	}
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

// looksLikeJSON reports whether b plausibly starts a JSON document -- enough to
// tell a real chat-completions reply from a gateway's HTML error page.
func looksLikeJSON(b []byte) bool {
	b = bytes.TrimSpace(b)
	return len(b) > 0 && (b[0] == '{' || b[0] == '[')
}
