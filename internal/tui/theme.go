package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
)

// Theme is the user-configurable color set. Values are lipgloss color strings:
// ANSI indices ("12"), 256-color indices ("212") or hex ("#7D56F4").
type Theme struct {
	User      string `json:"user"`
	Assistant string `json:"assistant"`
	Accent    string `json:"accent"`
	Muted     string `json:"muted"`
	Error     string `json:"error"`
	Border    string `json:"border"`
	Tool      string `json:"tool"`
	Success   string `json:"success"`
}

// DefaultTheme is used when no theme file exists.
func DefaultTheme() Theme {
	return Theme{
		User:      "12", // blue
		Assistant: "15", // bright white
		Accent:    "13", // magenta
		Muted:     "8",  // grey
		Error:     "9",  // red
		Border:    "8",  // grey
		Tool:      "6",  // cyan
		Success:   "10", // green
	}
}

// ThemePath is ~/.arnes/theme.json.
func ThemePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arnes", "theme.json"), nil
}

// LoadTheme reads a theme file. A missing file yields DefaultTheme; any field
// left empty falls back to its default.
func LoadTheme(path string) (Theme, error) {
	base := DefaultTheme()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return base, nil
	}
	if err != nil {
		return base, err
	}
	var loaded Theme
	if err := json.Unmarshal(data, &loaded); err != nil {
		return base, err
	}
	return base.merge(loaded), nil
}

func (t Theme) merge(o Theme) Theme {
	pick := func(def, override string) string {
		if override != "" {
			return override
		}
		return def
	}
	return Theme{
		User:      pick(t.User, o.User),
		Assistant: pick(t.Assistant, o.Assistant),
		Accent:    pick(t.Accent, o.Accent),
		Muted:     pick(t.Muted, o.Muted),
		Error:     pick(t.Error, o.Error),
		Border:    pick(t.Border, o.Border),
		Tool:      pick(t.Tool, o.Tool),
		Success:   pick(t.Success, o.Success),
	}
}

// Styles are the lipgloss styles derived from a Theme.
type Styles struct {
	User      lipgloss.Style
	Assistant lipgloss.Style
	Accent    lipgloss.Style
	Muted     lipgloss.Style
	Error     lipgloss.Style
	Border    lipgloss.Style
	Tool      lipgloss.Style
	Success   lipgloss.Style
}

// Styles builds the render styles for this theme.
func (t Theme) Styles() Styles {
	fg := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	return Styles{
		User:      fg(t.User).Bold(true),
		Assistant: fg(t.Assistant),
		Accent:    fg(t.Accent),
		Muted:     fg(t.Muted),
		Error:     fg(t.Error).Bold(true),
		Border:    fg(t.Border),
		Tool:      fg(t.Tool),
		Success:   fg(t.Success),
	}
}
