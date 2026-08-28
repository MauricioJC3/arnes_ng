package subagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultPath is ~/.arnes/subagents.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arnes", "subagents.json"), nil
}

// LoadFile reads a JSON array of Definitions from path. A missing file returns
// Defaults() so the harness always has subagents to offer.
func LoadFile(path string) ([]Definition, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return nil, err
	}

	var defs []Definition
	if err := json.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("%s inválido: %w", path, err)
	}
	for i, d := range defs {
		if d.Name == "" {
			return nil, fmt.Errorf("subagente #%d sin 'name'", i+1)
		}
		if d.System == "" {
			return nil, fmt.Errorf("subagente %q sin 'system'", d.Name)
		}
	}
	return defs, nil
}
