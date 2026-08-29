package skill

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// defaultsFS holds the curated skills shipped with arnes. Each lives at
// defaults/<name>/SKILL.md; NOTICE.md alongside them records attribution.
//
//go:embed defaults
var defaultsFS embed.FS

const defaultsRoot = "defaults"

// Defaults parses the skills bundled with arnes from the embedded filesystem.
// They are the same SKILL.md format as user skills; the directory name is the
// fallback when a file omits its frontmatter name.
func Defaults() ([]Skill, error) {
	entries, err := defaultsFS.ReadDir(defaultsRoot)
	if err != nil {
		return nil, err
	}

	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := defaultsRoot + "/" + e.Name() + "/SKILL.md"
		data, err := defaultsFS.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		s := parse(string(data))
		s.Path = p
		if s.Name == "" {
			s.Name = e.Name()
		}
		skills = append(skills, s)
	}
	return skills, nil
}

// SeedDefaults makes sure every bundled default skill exists under dir. A skill
// whose directory is already there is left untouched, so a user's edits stick
// and their own skills are never disturbed -- SeedDefaults only ever adds what
// is missing, and never asks. It runs on every startup, so a default the user
// deletes reappears on the next run: the curated set is meant to always be
// available. It returns the names it wrote.
func SeedDefaults(dir string) ([]string, error) {
	entries, err := defaultsFS.ReadDir(defaultsRoot)
	if err != nil {
		return nil, err
	}

	var written []string
	for _, e := range entries {
		if !e.IsDir() {
			continue // top-level files (NOTICE.md) are not seeded to disk
		}
		dst := filepath.Join(dir, e.Name())
		switch _, err := os.Stat(dst); {
		case err == nil:
			continue // ya está: es del usuario
		case !errors.Is(err, os.ErrNotExist):
			return nil, err
		}
		if err := copyEmbeddedDir(defaultsRoot+"/"+e.Name(), dst); err != nil {
			return nil, err
		}
		written = append(written, e.Name())
	}
	return written, nil
}

// copyEmbeddedDir writes the embedded tree rooted at src (a slash path inside
// defaultsFS) to the on-disk directory dst, creating parents as needed.
func copyEmbeddedDir(src, dst string) error {
	return fs.WalkDir(defaultsFS, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, src), "/")
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := defaultsFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
