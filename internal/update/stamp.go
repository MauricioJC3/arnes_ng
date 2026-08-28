package update

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Stamp records when arnes last checked for an update, so the daily check
// doesn't run on every start.
type Stamp struct{ Path string }

// DefaultStampPath is ~/.arnes/last_update_check.
func DefaultStampPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arnes", "last_update_check"), nil
}

// Due reports whether at least every has elapsed since the recorded check. A
// missing or unreadable stamp is due.
func (s Stamp) Due(now time.Time, every time.Duration) bool {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return true
	}
	return now.Sub(last) >= every
}

// Mark writes now as the last-check time (best effort; creates ~/.arnes).
func (s Stamp) Mark(now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.Path, []byte(now.UTC().Format(time.RFC3339)+"\n"), 0o644)
}
