// Package mcp connects the harness to external Model Context Protocol servers
// over stdio or Streamable HTTP and exposes their tools as native tool.Tool
// values.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// protocolVersion is the MCP revision the harness advertises in `initialize`.
const protocolVersion = "2025-06-18"

// ServerConfig describes one MCP server. Exactly one transport must be set:
// Command (+Args/Env) launches a stdio server; URL points at a Streamable HTTP
// endpoint.
type ServerConfig struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

// Config is the mcp.json shape (compatible with the common `mcpServers` key).
type Config struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// DefaultPath is ~/.arnes/mcp.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arnes", "mcp.json"), nil
}

// LoadFile reads mcp.json. A missing file yields an empty config (no servers).
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("%s inválido: %w", path, err)
	}
	for name, sc := range c.MCPServers {
		switch {
		case sc.Command == "" && sc.URL == "":
			return Config{}, fmt.Errorf("servidor MCP %q: falta 'command' o 'url'", name)
		case sc.Command != "" && sc.URL != "":
			return Config{}, fmt.Errorf("servidor MCP %q: 'command' y 'url' son mutuamente excluyentes", name)
		}
	}
	return c, nil
}

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
