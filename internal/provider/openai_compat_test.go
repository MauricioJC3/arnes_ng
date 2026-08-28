package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ocFakeServer serves one canned reply and captures the request body it received.
func ocFakeServer(t *testing.T, reply string, captured *ocChatRequest) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, quiero /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if captured != nil {
			if err := json.Unmarshal(body, captured); err != nil {
				t.Errorf("body enviado ilegible: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestOC(t *testing.T, reply string, captured *ocChatRequest) *OpenAICompat {
	srv := ocFakeServer(t, reply, captured)
	return NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL, APIKey: "test-key", Model: "deepseek-chat"})
}

func TestOpenAICompatTextReply(t *testing.T) {
	var sent ocChatRequest
	oc := newTestOC(t, `{
		"choices": [{"finish_reason": "stop", "message": {"role": "assistant", "content": "hola de vuelta"}}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 3}
	}`, &sent)

	resp, err := oc.SendMessage(context.Background(), Request{
		System:   "sos un asistente",
		Messages: []Message{{Role: RoleUser, Text: "hola"}},
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if resp.Text != "hola de vuelta" {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.StopReason != StopEndTurn {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 3 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
	if len(sent.Messages) != 2 || sent.Messages[0].Role != "system" || sent.Messages[1].Role != "user" {
		t.Fatalf("mensajes enviados mal armados: %+v", sent.Messages)
	}
	if sent.MaxTokens != 4096 {
		t.Errorf("MaxTokens enviado = %d, quiero el default 4096", sent.MaxTokens)
	}
}

func TestOpenAICompatToolCallRoundTrip(t *testing.T) {
	var sent ocChatRequest
	oc := newTestOC(t, `{
		"choices": [{
			"finish_reason": "tool_calls",
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [
					{"id": "call_1", "type": "function", "function": {"name": "bash", "arguments": "{\"command\":\"ls\"}"}}
				]
			}
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 4}
	}`, &sent)

	resp, err := oc.SendMessage(context.Background(), Request{
		Messages: []Message{
			{Role: RoleUser, Text: "listá archivos"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "call_0", Name: "bash", Input: json.RawMessage(`{"command":"pwd"}`)},
			}},
			{Role: RoleUser, ToolResults: []ToolResult{{CallID: "call_0", Content: "/home"}}},
		},
		Tools: []ToolDef{{
			Name:        "bash",
			Description: "corre un comando",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"command": map[string]any{"type": "string"}},
				"required":   []string{"command"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}

	// respuesta normalizada
	if resp.StopReason != StopToolUse {
		t.Errorf("StopReason = %q, quiero %q", resp.StopReason, StopToolUse)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "bash" || resp.ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool call mal normalizada: %+v", resp.ToolCalls)
	}
	var args map[string]string
	if err := json.Unmarshal(resp.ToolCalls[0].Input, &args); err != nil || args["command"] != "ls" {
		t.Errorf("arguments mal parseados: %s", resp.ToolCalls[0].Input)
	}

	// request enviado: user -> assistant(tool_calls) -> tool
	var roles []string
	for _, m := range sent.Messages {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, ",") != "user,assistant,tool" {
		t.Fatalf("roles enviados = %v", roles)
	}
	if len(sent.Messages[1].ToolCalls) != 1 || sent.Messages[1].ToolCalls[0].Function.Arguments != `{"command":"pwd"}` {
		t.Errorf("assistant tool_calls mal serializado: %+v", sent.Messages[1].ToolCalls)
	}
	if sent.Messages[2].ToolCallID != "call_0" || sent.Messages[2].Content != "/home" {
		t.Errorf("tool message mal armado: %+v", sent.Messages[2])
	}
	if len(sent.Tools) != 1 || sent.Tools[0].Type != "function" || sent.Tools[0].Function.Name != "bash" {
		t.Fatalf("tools mal traducidas: %+v", sent.Tools)
	}
}

func TestOpenAICompatHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error": {"message": "Invalid API key", "type": "authentication_error"}}`)
	}))
	t.Cleanup(srv.Close)
	oc := NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL, APIKey: "bad", Model: "x"})

	_, err := oc.SendMessage(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hola"}}})
	if err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Fatalf("esperaba el error de la API propagado, tengo: %v", err)
	}
}

func TestOpenAICompatStreamText(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"content":"Hola"},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"content":", mundo"},"finish_reason":null}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":3}}`,
		`[DONE]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	t.Cleanup(srv.Close)
	oc := NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL, APIKey: "test-key", Model: "deepseek-chat"})

	var deltas []string
	resp, err := oc.StreamMessage(context.Background(),
		Request{Messages: []Message{{Role: RoleUser, Text: "hola"}}},
		func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatalf("StreamMessage: %v", err)
	}
	if strings.Join(deltas, "") != "Hola, mundo" || resp.Text != "Hola, mundo" {
		t.Fatalf("deltas=%v text=%q", deltas, resp.Text)
	}
	if resp.StopReason != StopEndTurn {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if resp.Usage.OutputTokens != 3 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

func TestOpenAICompatStreamToolCall(t *testing.T) {
	frames := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"bash","arguments":"{\"comm"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"and\":\"ls\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
	t.Cleanup(srv.Close)
	oc := NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL, APIKey: "test-key", Model: "x"})

	resp, err := oc.StreamMessage(context.Background(),
		Request{Messages: []Message{{Role: RoleUser, Text: "listá"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != StopToolUse || len(resp.ToolCalls) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	tc := resp.ToolCalls[0]
	var args map[string]string
	if tc.ID != "call_1" || tc.Name != "bash" || json.Unmarshal(tc.Input, &args) != nil || args["command"] != "ls" {
		t.Fatalf("tool call mal ensamblada: %+v (%s)", tc, tc.Input)
	}
}

func TestOpenAICompatStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	oc := NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL, Model: "x"})
	if _, err := oc.StreamMessage(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "x"}}}, nil); err == nil {
		t.Fatal("esperaba error HTTP")
	}
}

func TestOpenAICompatConstructors(t *testing.T) {
	tests := []struct {
		name string
		oc   *OpenAICompat
		want string
	}{
		{"deepseek", NewDeepSeek("k", "deepseek-chat"), DeepSeekBaseURL},
		{"kimi", NewKimi("k", "kimi-k2"), KimiBaseURL},
		{"openai", NewOpenAI("k", "gpt-4o"), OpenAIBaseURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.oc.baseURL != tt.want {
				t.Errorf("baseURL = %q, quiero %q", tt.oc.baseURL, tt.want)
			}
		})
	}
}
