package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileTracker records which files have been read (or freshly written) this
// session, so edit_file and write_file can refuse to touch an EXISTING file the
// model has not looked at -- the "never edit blind" rule. A nil *FileTracker
// disables the guard entirely: every operation is allowed. It is safe for
// concurrent use.
type FileTracker struct {
	mu   sync.Mutex
	seen map[string]bool
}

// NewFileTracker returns an empty tracker with the guard active.
func NewFileTracker() *FileTracker {
	return &FileTracker{seen: make(map[string]bool)}
}

// key normalises a path so "./x.go", "x.go" and an absolute form all collide.
func (t *FileTracker) key(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// MarkRead records that path's current contents are known to the model -- after
// a read_file, or after a successful write_file / edit_file.
func (t *FileTracker) MarkRead(path string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen == nil {
		t.seen = make(map[string]bool)
	}
	t.seen[t.key(path)] = true
}

// GuardWrite returns a non-nil error when path names a file that exists on disk
// but has not been read this session. Creating a new file (nothing at path) is
// always allowed, and a nil tracker allows everything.
func (t *FileTracker) GuardWrite(path string) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	seen := t.seen[t.key(path)]
	t.mu.Unlock()
	if seen {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil // does not exist (or unreadable) -> a new file, no prior read needed
	}
	return fmt.Errorf("leé %s con read_file antes de modificarlo: no edites a ciegas un archivo "+
		"que no viste esta sesión", path)
}
