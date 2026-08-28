package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ServerConfig is how to launch one language server (it must speak LSP over
// stdio).
type ServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Config is the lsp.json shape: a map from file extension (".go", ".ts", ...)
// to the server that handles it.
type Config struct {
	Servers map[string]ServerConfig `json:"servers"`
}

// DefaultPath is ~/.arnes/lsp.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arnes", "lsp.json"), nil
}

// Default is the built-in config used when no file exists: gopls for Go.
func Default() Config {
	return Config{Servers: map[string]ServerConfig{
		".go": {Command: "gopls"},
	}}
}

// LoadFile reads lsp.json. A missing file yields Default().
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("%s inválido: %w", path, err)
	}
	norm := make(map[string]ServerConfig, len(c.Servers))
	for ext, sc := range c.Servers {
		if sc.Command == "" {
			return Config{}, fmt.Errorf("servidor LSP para %q sin 'command'", ext)
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		norm[strings.ToLower(ext)] = sc
	}
	c.Servers = norm
	return c, nil
}

// serverFor returns the server configured for path's extension, if any.
func (c Config) serverFor(path string) (ServerConfig, bool) {
	sc, ok := c.Servers[strings.ToLower(filepath.Ext(path))]
	return sc, ok
}
