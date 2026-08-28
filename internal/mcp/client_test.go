package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// fakeServer plays a minimal MCP server over the given pipes: it answers
// initialize, ignores the initialized notification, lists one "echo" tool, and
// echoes the arguments back on tools/call.
func fakeServer(t *testing.T, in io.Reader, out io.Writer) {
	t.Helper()
	r := bufio.NewReader(in)
	write := func(v any) {
		b, _ := json.Marshal(v)
		out.Write(append(b, '\n'))
	}
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req rpcRequest
		if json.Unmarshal(line, &req) != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			write(rpcResponse{JSONRPC: "2.0", ID: req.ID,
				Result: json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"fake"}}`)})
		case "notifications/initialized":
			// notification: no response
		case "tools/list":
			write(rpcResponse{JSONRPC: "2.0", ID: req.ID,
				Result: json.RawMessage(`{"tools":[{"name":"echo","description":"echoes input","inputSchema":{"type":"object","properties":{"msg":{"type":"string"}}}}]}`)})
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Name != "echo" {
				write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: "unknown tool"}})
				continue
			}
			result, _ := json.Marshal(map[string]any{
				"content": []map[string]any{{"type": "text", "text": "echoed: " + string(p.Arguments)}},
			})
			write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
		default:
			write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found"}})
		}
	}
}

// pipePair wires a client conn to a fakeServer goroutine.
func pipePair(t *testing.T) *conn {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	go fakeServer(t, c2sR, s2cW)
	cn := newConn(c2sW, s2cR, nil)
	t.Cleanup(func() { c2sW.Close(); s2cW.Close() })
	return cn
}

func TestClientHandshakeAndList(t *testing.T) {
	c, err := newClient(context.Background(), "fake", pipePair(t))
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	if c.Server() != "fake" {
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

func TestClientCall(t *testing.T) {
	c, err := newClient(context.Background(), "fake", pipePair(t))
	if err != nil {
		t.Fatal(err)
	}

	out, err := c.Call(context.Background(), "echo", json.RawMessage(`{"msg":"hola"}`))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(out, `echoed: {"msg":"hola"}`) {
		t.Fatalf("out = %q", out)
	}
}

func TestClientCallUnknownTool(t *testing.T) {
	c, err := newClient(context.Background(), "fake", pipePair(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), "nope", json.RawMessage(`{}`)); err == nil {
		t.Fatal("esperaba error del servidor")
	}
}

func TestAdaptedToolWrapsClient(t *testing.T) {
	c, err := newClient(context.Background(), "fake", pipePair(t))
	if err != nil {
		t.Fatal(err)
	}
	at := adaptedTool{client: c, info: c.Tools()[0], name: "fake__echo"}

	if at.Name() != "fake__echo" || at.Description() != "echoes input" {
		t.Fatalf("adaptador mal armado: %s / %s", at.Name(), at.Description())
	}
	out, err := at.Execute(context.Background(), json.RawMessage(`{"msg":"x"}`))
	if err != nil || !strings.Contains(out, "echoed") {
		t.Fatalf("Execute: out=%q err=%v", out, err)
	}
}
