package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestAnthropicToParams(t *testing.T) {
	a := NewAnthropic()
	a.SetModel("claude-sonnet-5")

	req := Request{
		System:    "sos un asistente",
		MaxTokens: 0, // debe caer al default
		Messages: []Message{
			{Role: RoleUser, Text: "hola"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "t1", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)},
			}},
			{Role: RoleUser, ToolResults: []ToolResult{
				{CallID: "t1", Content: "archivo.txt"},
			}},
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
	}

	params, err := a.toParams(req)
	if err != nil {
		t.Fatalf("toParams devolvió error: %v", err)
	}
	if string(params.Model) != "claude-sonnet-5" {
		t.Errorf("Model = %q, quiero claude-sonnet-5", params.Model)
	}
	if params.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, quiero 4096 (default)", params.MaxTokens)
	}
	if len(params.Messages) != 3 {
		t.Fatalf("Messages = %d, quiero 3", len(params.Messages))
	}
	if len(params.System) != 1 || params.System[0].Text != "sos un asistente" {
		t.Errorf("System mal traducido: %+v", params.System)
	}
	if len(params.Tools) != 1 || params.Tools[0].OfTool == nil {
		t.Fatalf("Tools mal traducido: %+v", params.Tools)
	}
	if got := params.Tools[0].OfTool.InputSchema.Required; len(got) != 1 || got[0] != "command" {
		t.Errorf("'required' del schema mal traducido: %+v", got)
	}

	// caching: cache_control en el system y en el prefijo de mensajes
	raw, err := params.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "cache_control"); n < 2 {
		t.Fatalf("esperaba >=2 marcas cache_control (system + prefijo), hay %d\n%s", n, raw)
	}
}

func TestAnthropicToParamsRejectsUnknownRole(t *testing.T) {
	a := NewAnthropic()
	if _, err := a.toParams(Request{Messages: []Message{{Role: "system", Text: "x"}}}); err == nil {
		t.Fatal("esperaba error por rol desconocido")
	}
}

func TestAnthropicToParamsDropsEmptyAssistant(t *testing.T) {
	a := NewAnthropic()
	params, err := a.toParams(Request{Messages: []Message{
		{Role: RoleUser, Text: "hacé unos cambios"},
		{Role: RoleAssistant}, // poisoned: no text, no tool calls
		{Role: RoleUser, Text: "sigue"},
	}})
	if err != nil {
		t.Fatalf("toParams: %v", err)
	}
	if len(params.Messages) != 2 {
		t.Fatalf("esperaba 2 mensajes (el assistant vacío se descarta), tengo %d", len(params.Messages))
	}
}

func TestFromMessageNormalizesContent(t *testing.T) {
	fixture := `{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-opus-5",
		"stop_reason": "tool_use",
		"content": [
			{"type": "text", "text": "voy a listar"},
			{"type": "tool_use", "id": "tu_1", "name": "bash", "input": {"command": "ls"}}
		],
		"usage": {"input_tokens": 12, "output_tokens": 7, "cache_read_input_tokens": 900, "cache_creation_input_tokens": 40}
	}`
	msg := &anthropic.Message{}
	if err := json.Unmarshal([]byte(fixture), msg); err != nil {
		t.Fatalf("no se pudo armar el fixture: %v", err)
	}

	resp := fromMessage(msg)
	if resp.Text != "voy a listar" {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.StopReason != StopToolUse {
		t.Errorf("StopReason = %q, quiero %q", resp.StopReason, StopToolUse)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, quiero 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "tu_1" || tc.Name != "bash" {
		t.Errorf("tool call mal normalizada: %+v", tc)
	}
	var in map[string]string
	if err := json.Unmarshal(tc.Input, &in); err != nil || in["command"] != "ls" {
		t.Errorf("Input mal normalizado: %s (err %v)", tc.Input, err)
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 7 {
		t.Errorf("Usage mal normalizado: %+v", resp.Usage)
	}
	if resp.Usage.CacheReadInputTokens != 900 || resp.Usage.CacheCreationInputTokens != 40 {
		t.Errorf("tokens de caché no normalizados: %+v", resp.Usage)
	}
}

func TestMapStopReason(t *testing.T) {
	tests := []struct {
		in   anthropic.StopReason
		want StopReason
	}{
		{anthropic.StopReasonToolUse, StopToolUse},
		{anthropic.StopReasonEndTurn, StopEndTurn},
		{anthropic.StopReasonMaxTokens, StopMaxTokens},
		{anthropic.StopReasonModelContextWindowExceeded, StopMaxTokens},
		{anthropic.StopReasonRefusal, StopEndTurn},
	}
	for _, tt := range tests {
		if got := mapStopReason(tt.in); got != tt.want {
			t.Errorf("mapStopReason(%q) = %q, quiero %q", tt.in, got, tt.want)
		}
	}
}
