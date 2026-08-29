package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

// These exercise the Anthropic adapter over real HTTP against a fake Messages
// API -- the SDK's request build, response parse, retry and error mapping, none
// of which the toParams/fromMessage unit tests touch.

func newTestAnthropic(t *testing.T, h http.HandlerFunc) *Anthropic {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewAnthropic(
		option.WithBaseURL(srv.URL),
		option.WithAPIKey("test-key"),
		option.WithMaxRetries(2),
	)
}

func TestAnthropicSendMessageOverHTTP(t *testing.T) {
	a := newTestAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"claude-x"`) {
			t.Errorf("payload sin modelo: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"msg_1","type":"message","role":"assistant","model":"claude-x",
			"content":[{"type":"text","text":"hola de vuelta"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":4,"cache_read_input_tokens":6}
		}`)
	})
	a.SetModel("claude-x")

	resp, err := a.SendMessage(context.Background(), Request{
		System:   "sos un asistente",
		Messages: []Message{{Role: RoleUser, Text: "hola"}},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.Text != "hola de vuelta" || resp.StopReason != StopEndTurn {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 4 || resp.Usage.CacheReadInputTokens != 6 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestAnthropicToolUseOverHTTP(t *testing.T) {
	a := newTestAnthropic(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"msg_2","type":"message","role":"assistant","model":"claude-x",
			"content":[
				{"type":"text","text":"ejecuto"},
				{"type":"tool_use","id":"tu_1","name":"bash","input":{"command":"ls -la"}}
			],
			"stop_reason":"tool_use",
			"usage":{"input_tokens":12,"output_tokens":9}
		}`)
	})

	resp, err := a.SendMessage(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "listá"}}})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.StopReason != StopToolUse || len(resp.ToolCalls) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.ToolCalls[0].Name != "bash" || !strings.Contains(string(resp.ToolCalls[0].Input), "ls -la") {
		t.Fatalf("tool call = %+v", resp.ToolCalls[0])
	}
}

func TestAnthropicRetriesA529(t *testing.T) {
	var hits int
	a := newTestAnthropic(t, func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(529) // overloaded
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"claude-x",
			"content":[{"type":"text","text":"ok tras retry"}],"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":1}}`)
	})

	resp, err := a.SendMessage(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hola"}}})
	if err != nil {
		t.Fatalf("el SDK debería reintentar el 529: %v", err)
	}
	if resp.Text != "ok tras retry" || hits < 2 {
		t.Fatalf("text=%q hits=%d", resp.Text, hits)
	}
}

func TestAnthropicHTTPErrorIsSurfaced(t *testing.T) {
	a := newTestAnthropic(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	})

	_, err := a.SendMessage(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hola"}}})
	if err == nil || !strings.Contains(err.Error(), "anthropic:") {
		t.Fatalf("esperaba el error mapeado, tengo: %v", err)
	}
}

func TestAnthropicStreamOverHTTP(t *testing.T) {
	a := newTestAnthropic(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		frames := []string{
			`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-x","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
			`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hola"}}`,
			`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" mundo"}}`,
			`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
			`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			`event: message_stop
data: {"type":"message_stop"}`,
		}
		for _, f := range frames {
			io.WriteString(w, f+"\n\n")
			fl.Flush()
		}
	})

	var deltas []string
	resp, err := a.StreamMessage(context.Background(),
		Request{Messages: []Message{{Role: RoleUser, Text: "hola"}}},
		func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatalf("StreamMessage: %v", err)
	}
	if strings.Join(deltas, "") != "Hola mundo" || resp.Text != "Hola mundo" {
		t.Fatalf("deltas=%v text=%q", deltas, resp.Text)
	}
	if resp.StopReason != StopEndTurn {
		t.Fatalf("StopReason = %q", resp.StopReason)
	}
}
