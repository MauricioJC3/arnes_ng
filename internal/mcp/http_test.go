package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// httpState records what a fakeHTTPServer saw, for header assertions.
type httpState struct {
	mu       sync.Mutex
	sessions []string // Mcp-Session-Id header seen on each request
	methods  []string
}

func (s *httpState) record(method, session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.methods = append(s.methods, method)
	s.sessions = append(s.sessions, session)
}

func (s *httpState) sessionSeen(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.sessions {
		if v == id {
			return true
		}
	}
	return false
}

// fakeHTTPServer plays a minimal Streamable HTTP MCP server: it hands out a
// session id on initialize, answers tools/list with one "echo" tool, and echoes
// arguments on tools/call. When sse is true the tools/call reply is delivered as
// an event stream instead of a plain JSON body.
func fakeHTTPServer(t *testing.T, sse bool) (*httptest.Server, *httpState) {
	t.Helper()
	st := &httpState{}
	const sessionID = "sess-123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var probe struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&probe); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		st.record(probe.Method, r.Header.Get("Mcp-Session-Id"))

		if probe.ID == nil { // notification
			w.WriteHeader(http.StatusAccepted)
			return
		}

		writeJSON := func(result string) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(rpcResponse{
				JSONRPC: "2.0", ID: *probe.ID, Result: json.RawMessage(result),
			})
		}

		switch probe.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", sessionID)
			writeJSON(`{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"fake-http"}}`)
		case "tools/list":
			writeJSON(`{"tools":[{"name":"echo","description":"echoes input","inputSchema":{"type":"object","properties":{"msg":{"type":"string"}}}}]}`)
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(probe.Params, &p)
			if p.Name != "echo" {
				writeJSONError(w, *probe.ID, "unknown tool")
				return
			}
			result, _ := json.Marshal(map[string]any{
				"content": []map[string]any{{"type": "text", "text": "echoed: " + string(p.Arguments)}},
			})
			if sse {
				w.Header().Set("Content-Type", "text/event-stream")
				resp, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: *probe.ID, Result: result})
				w.Write([]byte("event: message\ndata: " + string(resp) + "\n\n"))
				return
			}
			writeJSON(string(result))
		default:
			writeJSONError(w, *probe.ID, "method not found")
		}
	}))
	t.Cleanup(srv.Close)
	return srv, st
}

func writeJSONError(w http.ResponseWriter, id int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: -32601, Message: msg}})
}

func dialTestHTTP(t *testing.T, url string) *Client {
	t.Helper()
	rt, err := dialHTTP(context.Background(), url)
	if err != nil {
		t.Fatalf("dialHTTP: %v", err)
	}
	c, err := newClient(context.Background(), "http-fake", rt)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

func TestHTTPHandshakeAndList(t *testing.T) {
	srv, _ := fakeHTTPServer(t, false)
	c := dialTestHTTP(t, srv.URL)

	if c.Server() != "http-fake" {
		t.Errorf("Server() = %q", c.Server())
	}
	tools := c.Tools()
	if len(tools) != 1 || tools[0].Name != "echo" || tools[0].Description != "echoes input" {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[0].InputSchema["type"] != "object" {
		t.Errorf("inputSchema mal parseado: %+v", tools[0].InputSchema)
	}
}

func TestHTTPCall(t *testing.T) {
	srv, _ := fakeHTTPServer(t, false)
	c := dialTestHTTP(t, srv.URL)

	out, err := c.Call(context.Background(), "echo", json.RawMessage(`{"msg":"hola"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(out, `echoed: {"msg":"hola"}`) {
		t.Fatalf("out = %q", out)
	}
}

func TestHTTPCallOverSSE(t *testing.T) {
	srv, _ := fakeHTTPServer(t, true)
	c := dialTestHTTP(t, srv.URL)

	out, err := c.Call(context.Background(), "echo", json.RawMessage(`{"msg":"sse"}`))
	if err != nil {
		t.Fatalf("Call over SSE: %v", err)
	}
	if !strings.Contains(out, `echoed: {"msg":"sse"}`) {
		t.Fatalf("out = %q", out)
	}
}

func TestHTTPSessionIDEchoedBack(t *testing.T) {
	srv, st := fakeHTTPServer(t, false)
	c := dialTestHTTP(t, srv.URL)

	if _, err := c.Call(context.Background(), "echo", json.RawMessage(`{"msg":"x"}`)); err != nil {
		t.Fatal(err)
	}
	// initialize handed out "sess-123"; every later request must carry it.
	if !st.sessionSeen("sess-123") {
		t.Fatalf("el cliente no reenvió Mcp-Session-Id; vistos: %v", st.sessions)
	}
	st.mu.Lock()
	first := st.sessions[0]
	st.mu.Unlock()
	if first != "" {
		t.Errorf("initialize no debería llevar session id, llevó %q", first)
	}
}

func TestHTTPServerErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	rt, err := dialHTTP(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newClient(context.Background(), "bad", rt); err == nil {
		t.Fatal("esperaba error del handshake ante HTTP 500")
	}
}

func TestDialHTTPRejectsNonHTTPURL(t *testing.T) {
	if _, err := dialHTTP(context.Background(), "ftp://nope"); err == nil {
		t.Fatal("esperaba rechazo de URL no http(s)")
	}
}

func TestHTTPCallUnknownTool(t *testing.T) {
	srv, _ := fakeHTTPServer(t, false)
	c := dialTestHTTP(t, srv.URL)
	if _, err := c.Call(context.Background(), "nope", json.RawMessage(`{}`)); err == nil {
		t.Fatal("esperaba error del servidor")
	}
}
