package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolInfo is one tool advertised by a server.
type ToolInfo struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Client is a live connection to one MCP server.
type Client struct {
	server string
	rt     transport
	tools  []ToolInfo
}

// Dial connects to the server described by cfg (stdio or Streamable HTTP) and
// completes the handshake.
func Dial(ctx context.Context, server string, cfg ServerConfig) (*Client, error) {
	var (
		rt  transport
		err error
	)
	if cfg.URL != "" {
		rt, err = dialHTTP(ctx, cfg.URL)
	} else {
		rt, err = spawn(ctx, cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("mcp %s: no arrancó: %w", server, err)
	}
	c, err := newClient(ctx, server, rt)
	if err != nil {
		rt.close()
		return nil, err
	}
	return c, nil
}

// newClient runs initialize + tools/list over an already-open transport. Split
// out from Dial so tests can drive it with in-memory pipes or a test server.
func newClient(ctx context.Context, server string, rt transport) (*Client, error) {
	initParams := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "arnes", "version": "0.1"},
	}
	if _, err := rt.call(ctx, "initialize", initParams); err != nil {
		return nil, fmt.Errorf("mcp %s: initialize falló: %w", server, err)
	}
	if err := rt.notify("notifications/initialized", nil); err != nil {
		return nil, fmt.Errorf("mcp %s: %w", server, err)
	}

	res, err := rt.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("mcp %s: tools/list falló: %w", server, err)
	}
	var listed struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(res, &listed); err != nil {
		return nil, fmt.Errorf("mcp %s: tools/list ilegible: %w", server, err)
	}

	c := &Client{server: server, rt: rt}
	for _, t := range listed.Tools {
		c.tools = append(c.tools, ToolInfo{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return c, nil
}

// Server is the configured name of this server.
func (c *Client) Server() string { return c.server }

// Tools lists what this server exposes.
func (c *Client) Tools() []ToolInfo { return c.tools }

// Call invokes a remote tool and returns the concatenated text content.
func (c *Client) Call(ctx context.Context, tool string, args json.RawMessage) (string, error) {
	arguments := any(map[string]any{})
	if len(args) > 0 {
		arguments = json.RawMessage(args)
	}
	res, err := c.rt.call(ctx, "tools/call", map[string]any{"name": tool, "arguments": arguments})
	if err != nil {
		return "", err
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", fmt.Errorf("respuesta de %q ilegible: %w", tool, err)
	}

	var b strings.Builder
	for _, part := range out.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	if out.IsError {
		return b.String(), fmt.Errorf("la tool MCP %q devolvió un error", tool)
	}
	return b.String(), nil
}

// Close shuts down the server process or HTTP session.
func (c *Client) Close() error { return c.rt.close() }
