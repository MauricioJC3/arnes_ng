package mcp

import (
	"context"
	"encoding/json"
)

// adaptedTool wraps one remote MCP tool so it satisfies tool.Tool. Its name is
// namespaced as "<server>__<tool>" to avoid collisions with local tools.
type adaptedTool struct {
	client *Client
	info   ToolInfo
	name   string
}

func (t adaptedTool) Name() string        { return t.name }
func (t adaptedTool) Description() string { return t.info.Description }

func (t adaptedTool) InputSchema() map[string]any {
	if t.info.InputSchema != nil {
		return t.info.InputSchema
	}
	return map[string]any{"type": "object"}
}

func (t adaptedTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	return t.client.Call(ctx, t.info.Name, input)
}
