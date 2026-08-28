package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ErrNotFound is returned by Load and Delete when the id has no saved session.
var ErrNotFound = errors.New("sesión no encontrada")

// Store persists sessions. The agent loop never touches it directly; the
// Persisting decorator does.
type Store interface {
	Save(s *Session) error
	Load(id string) (*Session, error)
	List() ([]Meta, error)
	Delete(id string) error
}

// DefaultDir is ~/.arnes/sessions.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arnes", "sessions"), nil
}

// FileStore keeps one pretty-printed JSON file per session under dir.
type FileStore struct {
	dir string
}

// NewFileStore creates dir if needed and returns a store rooted there.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("no se pudo crear %s: %w", dir, err)
	}
	return &FileStore{dir: dir}, nil
}

func (fs *FileStore) path(id string) string {
	return filepath.Join(fs.dir, id+".json")
}

// Save writes the session atomically: temp file in the same dir, then rename.
func (fs *FileStore) Save(s *Session) error {
	if s.ID == "" {
		return errors.New("la sesión no tiene id")
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(fs.dir, s.ID+".*.tmp")
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
	return os.Rename(tmpName, fs.path(s.ID))
}

// Load reads and decodes one session.
func (fs *FileStore) Load(id string) (*Session, error) {
	data, err := os.ReadFile(fs.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("sesión %s corrupta: %w", id, err)
	}
	return &s, nil
}

// List returns the metadata of every stored session, newest first. Corrupt or
// partially-written files are skipped rather than failing the whole listing.
func (fs *FileStore) List() ([]Meta, error) {
	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return nil, err
	}
	metas := make([]Meta, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-len(".json")]
		s, err := fs.Load(id)
		if err != nil {
			continue
		}
		metas = append(metas, s.Meta())
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].UpdatedAt.After(metas[j].UpdatedAt)
	})
	return metas, nil
}

// Delete removes one session file.
func (fs *FileStore) Delete(id string) error {
	err := os.Remove(fs.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
}
