package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// httpConn is a Streamable HTTP (MCP 2025-06-18) transport to one server: every
// JSON-RPC message is an HTTP POST to the same endpoint, and the reply is
// either a single application/json body or a text/event-stream that we scan for
// the response with the matching id.
type httpConn struct {
	endpoint string
	client   *http.Client

	mu        sync.Mutex
	nextID    int
	sessionID string // set from the Mcp-Session-Id response header, echoed back
}

// dialHTTP builds an HTTP transport. It does no network I/O; the handshake runs
// on the first call.
func dialHTTP(_ context.Context, endpoint string) (*httpConn, error) {
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return nil, fmt.Errorf("url MCP inválida: %q", endpoint)
	}
	return &httpConn{endpoint: endpoint, client: http.DefaultClient, nextID: 1}, nil
}

// close ends the HTTP session if the server handed us one. A failure here is
// best-effort and swallowed.
func (h *httpConn) close() error {
	h.mu.Lock()
	sid := h.sessionID
	h.mu.Unlock()
	if sid == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodDelete, h.endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Mcp-Session-Id", sid)
	if resp, err := h.client.Do(req); err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	return nil
}

func (h *httpConn) notify(method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	_, err = h.post(context.Background(), rpcNotification{JSONRPC: "2.0", Method: method, Params: raw}, 0)
	return err
}

func (h *httpConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	h.mu.Lock()
	id := h.nextID
	h.nextID++
	h.mu.Unlock()

	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}
	return h.post(ctx, rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: raw}, id)
}

// post sends one JSON-RPC message. wantID is the id to match in the reply, or 0
// for a notification (no reply body expected).
func (h *httpConn) post(ctx context.Context, msg any, wantID int) (json.RawMessage, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	h.mu.Lock()
	sid := h.sessionID
	h.mu.Unlock()
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	if got := resp.Header.Get("Mcp-Session-Id"); got != "" {
		h.mu.Lock()
		h.sessionID = got
		h.mu.Unlock()
	}

	if wantID == 0 || resp.StatusCode == http.StatusAccepted {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("MCP HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return readSSE(resp.Body, wantID)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return decodeRPC(data, wantID)
}

// decodeRPC parses one JSON-RPC response object and returns its result.
func decodeRPC(data []byte, wantID int) (json.RawMessage, error) {
	var resp rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(data), &resp); err != nil {
		return nil, fmt.Errorf("respuesta MCP ilegible: %w", err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	if resp.ID != wantID {
		return nil, fmt.Errorf("respuesta MCP con id %d, esperaba %d", resp.ID, wantID)
	}
	return resp.Result, nil
}

// readSSE scans an event stream for the first data payload that decodes to a
// JSON-RPC response with the matching id.
func readSSE(r io.Reader, wantID int) (json.RawMessage, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)

	var data strings.Builder
	flush := func() (json.RawMessage, bool, error) {
		if data.Len() == 0 {
			return nil, false, nil
		}
		payload := data.String()
		data.Reset()
		var resp rpcResponse
		if json.Unmarshal([]byte(payload), &resp) != nil || resp.ID != wantID {
			return nil, false, nil
		}
		if resp.Error != nil {
			return nil, true, resp.Error
		}
		return resp.Result, true, nil
	}

	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if res, done, err := flush(); done || err != nil {
				return res, err
			}
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if res, done, err := flush(); done || err != nil {
		return res, err
	}
	return nil, fmt.Errorf("stream SSE sin respuesta para id %d", wantID)
}
