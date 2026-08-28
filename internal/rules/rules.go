// Package rules loads a project-context file (AGENTS.md and friends) whose
// contents get injected into the agent's system prompt.
package rules

import (
	"os"
	"path/filepath"
	"strings"
)

// candidates are the file names looked for in the working directory, in order.
var candidates = []string{
	"AGENTS.md",
	"agent.md",
	"ARNES.md",
	filepath.Join(".arnes", "agent.md"),
}

// Load returns the project rules text and the file it came from. override
// (ARNES_RULES) wins; otherwise the first candidate that exists in dir is used.
// When nothing is found it returns ("", "", nil).
func Load(dir, override string) (text, source string, err error) {
	if override != "" {
		b, err := os.ReadFile(override)
		if err != nil {
			return "", "", err
		}
		return strings.TrimSpace(string(b)), override, nil
	}
	for _, name := range candidates {
		p := filepath.Join(dir, name)
		b, readErr := os.ReadFile(p)
		if readErr == nil {
			return strings.TrimSpace(string(b)), name, nil
		}
	}
	return "", "", nil
}

// Wrap formats the rules for appending to the system prompt. It returns "" for
// empty rules.
func Wrap(text, source string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if source == "" {
		source = "el proyecto"
	}
	return "\n\n# Reglas del proyecto (" + source + ")\n\n" + text
}
