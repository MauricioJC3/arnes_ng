// Package config is the harness's persisted settings: the active provider and
// model, plus API keys. Environment variables always override the file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the on-disk settings shape.
type Config struct {
	Provider   string            `json:"provider,omitempty"`
	Model      string            `json:"model,omitempty"`
	Mode       string            `json:"mode,omitempty"` // permission mode: normal | auto | plan
	Keys       map[string]string `json:"keys,omitempty"` // provider name -> api key
	AutoUpdate bool              `json:"auto_update,omitempty"`
	// MaxSteps is the tool round-trip budget for one turn. 0 uses the built-in
	// default; ARNES_MAX_STEPS overrides both.
	MaxSteps int `json:"max_steps,omitempty"`
	// ProtectedPaths are path.Match globs that still require confirmation in auto
	// mode (write_file / edit_file only). Empty falls back to the built-in
	// defaults (.env, .env.*).
	ProtectedPaths []string `json:"protected_paths,omitempty"`
}

// DefaultPath is ~/.arnes/config.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arnes", "config.json"), nil
}

// Load reads the config file. A missing file yields a zero Config, no error.
func Load(path string) (Config, error) {
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
	return c, nil
}

// Save writes the config atomically with 0600 perms (it holds API keys).
func (c Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "config.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// Clone returns a deep copy (the Keys map and ProtectedPaths slice are copied,
// not shared).
func (c Config) Clone() Config {
	out := c
	if c.Keys != nil {
		out.Keys = make(map[string]string, len(c.Keys))
		for k, v := range c.Keys {
			out.Keys[k] = v
		}
	}
	if c.ProtectedPaths != nil {
		out.ProtectedPaths = append([]string(nil), c.ProtectedPaths...)
	}
	return out
}

// SetKey stores an API key for a provider.
func (c *Config) SetKey(provider, key string) {
	if c.Keys == nil {
		c.Keys = map[string]string{}
	}
	c.Keys[provider] = key
}
