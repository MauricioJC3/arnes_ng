package memory

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultPath is ~/.arnes/memory/notes.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arnes", "memory", "notes.json"), nil
}

// FileStore keeps every note in one JSON array file. A mutex serializes tool
// calls so concurrent remember/recall don't race on the file.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore ensures the parent directory exists and returns a store at path.
func NewFileStore(path string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("no se pudo crear %s: %w", filepath.Dir(path), err)
	}
	return &FileStore{path: path}, nil
}

func (fs *FileStore) Add(text string, tags []string) (Note, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Note{}, errors.New("el texto a recordar no puede estar vacío")
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()

	notes, err := fs.load()
	if err != nil {
		return Note{}, err
	}
	n := Note{
		ID:        noteID(),
		Text:      text,
		Tags:      normalizeTags(tags),
		CreatedAt: time.Now(),
	}
	if err := fs.write(append(notes, n)); err != nil {
		return Note{}, err
	}
	return n, nil
}

func (fs *FileStore) All() ([]Note, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	notes, err := fs.load()
	if err != nil {
		return nil, err
	}
	sortNewestFirst(notes)
	return notes, nil
}

func (fs *FileStore) Search(query string, tags []string, limit int) ([]Note, error) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()

	notes, err := fs.load()
	if err != nil {
		return nil, err
	}
	terms := strings.Fields(strings.ToLower(query))
	wantTags := normalizeTags(tags)

	var out []Note
	for _, n := range notes {
		if !matchesTerms(n.Text, terms) {
			continue
		}
		if len(wantTags) > 0 && !hasAnyTag(n.Tags, wantTags) {
			continue
		}
		out = append(out, n)
	}
	sortNewestFirst(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- file io (call under fs.mu) -----------------------------------------------

func (fs *FileStore) load() ([]Note, error) {
	data, err := os.ReadFile(fs.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var notes []Note
	if err := json.Unmarshal(data, &notes); err != nil {
		return nil, fmt.Errorf("memoria corrupta en %s: %w", fs.path, err)
	}
	return notes, nil
}

func (fs *FileStore) write(notes []Note) error {
	data, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(fs.path), "notes.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, fs.path)
}

// --- helpers ----------------------------------------------------------------

func noteID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return time.Now().Format("20060102") + "-" + hex.EncodeToString(b[:])
}

func normalizeTags(tags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func matchesTerms(text string, terms []string) bool {
	lower := strings.ToLower(text)
	for _, term := range terms {
		if !strings.Contains(lower, term) {
			return false
		}
	}
	return true
}

func hasAnyTag(have, want []string) bool {
	set := make(map[string]bool, len(have))
	for _, t := range have {
		set[t] = true
	}
	for _, t := range want {
		if set[t] {
			return true
		}
	}
	return false
}

func sortNewestFirst(notes []Note) {
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].CreatedAt.After(notes[j].CreatedAt)
	})
}
