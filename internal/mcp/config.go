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

// ResolvePath returns the mcp.json path in effect: $ARNES_MCP when set,
// otherwise DefaultPath.
func ResolvePath() (string, error) {
	if p := os.Getenv("ARNES_MCP"); p != "" {
		return p, nil
	}
	return DefaultPath()
}

// validate reports whether one server entry has exactly one transport set.
func validate(name string, sc ServerConfig) error {
	switch {
	case sc.Command == "" && sc.URL == "":
		return fmt.Errorf("servidor MCP %q: falta 'command' o 'url'", name)
	case sc.Command != "" && sc.URL != "":
		return fmt.Errorf("servidor MCP %q: 'command' y 'url' son mutuamente excluyentes", name)
	}
	return nil
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
		if err := validate(name, sc); err != nil {
			return Config{}, err
		}
	}
	return c, nil
}

// Save writes c to path as pretty JSON, creating the parent directory.
func Save(path string, c Config) error {
	if c.MCPServers == nil {
		c.MCPServers = map[string]ServerConfig{}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// AddServer loads the config at path, adds server name, and saves. It errors if
// name already exists or sc is not a valid single-transport entry.
func AddServer(path, name string, sc ServerConfig) error {
	if name == "" {
		return errors.New("el nombre del servidor MCP no puede estar vacío")
	}
	if err := validate(name, sc); err != nil {
		return err
	}
	c, err := LoadFile(path)
	if err != nil {
		return err
	}
	if c.MCPServers == nil {
		c.MCPServers = map[string]ServerConfig{}
	}
	if _, exists := c.MCPServers[name]; exists {
		return fmt.Errorf("el servidor MCP %q ya está configurado", name)
	}
	c.MCPServers[name] = sc
	return Save(path, c)
}

// RemoveServer loads the config at path, drops server name, and saves. It errors
// if name is not present.
func RemoveServer(path, name string) error {
	c, err := LoadFile(path)
	if err != nil {
		return err
	}
	if _, exists := c.MCPServers[name]; !exists {
		return fmt.Errorf("no hay un servidor MCP llamado %q", name)
	}
	delete(c.MCPServers, name)
	return Save(path, c)
}

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
