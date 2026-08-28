package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestAnthropicListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, quiero /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "k" {
			t.Errorf("x-api-key = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"data": [
				{"id":"claude-opus-5","type":"model","display_name":"Opus 5","created_at":"2026-01-01T00:00:00Z","max_input_tokens":200000,"max_tokens":64000,"capabilities":{}},
				{"id":"claude-sonnet-5","type":"model","display_name":"Sonnet 5","created_at":"2025-09-01T00:00:00Z","max_input_tokens":200000,"max_tokens":64000,"capabilities":{}}
			],
			"has_more": false,
			"first_id": "claude-opus-5",
			"last_id": "claude-sonnet-5"
		}`)
	}))
	t.Cleanup(srv.Close)

	a := NewAnthropic(option.WithBaseURL(srv.URL), option.WithAPIKey("k"))
	got, err := a.ListModels(context.Background())
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if len(got) != 2 || got[0] != "claude-opus-5" || got[1] != "claude-sonnet-5" {
		t.Fatalf("modelos = %v", got)
	}
}
