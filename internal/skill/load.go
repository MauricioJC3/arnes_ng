package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultDir is ~/.arnes/skills.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".arnes", "skills"), nil
}

// Dirs returns the skill directories to scan, project first (so a project skill
// shadows a global one of the same name): <cwd>/.arnes/skills, then globalDir.
func Dirs(cwd, globalDir string) []string {
	var out []string
	if cwd != "" {
		out = append(out, filepath.Join(cwd, ".arnes", "skills"))
	}
	if globalDir != "" {
		out = append(out, globalDir)
	}
	return out
}

// Load scans each dir for skills and returns them in scan order. A skill lives
// at <dir>/<name>/SKILL.md (the Claude Code layout); a bare <dir>/<name>.md is
// also accepted. Missing directories are skipped. A malformed file is an error.
func Load(dirs ...string) ([]Skill, error) {
	var skills []Skill
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			var path, fallbackName string
			switch {
			case e.IsDir():
				path = filepath.Join(dir, e.Name(), "SKILL.md")
				fallbackName = e.Name()
				if _, statErr := os.Stat(path); statErr != nil {
					continue // a directory without SKILL.md is not a skill
				}
			case strings.EqualFold(filepath.Ext(e.Name()), ".md") && !strings.EqualFold(e.Name(), "README.md"):
				path = filepath.Join(dir, e.Name())
				fallbackName = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			default:
				continue
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("skill %s: %w", path, err)
			}
			s := parse(string(data))
			s.Path = path
			if s.Name == "" {
				s.Name = fallbackName
			}
			if strings.TrimSpace(s.Body) == "" {
				return nil, fmt.Errorf("skill %s: cuerpo vacío", path)
			}
			skills = append(skills, s)
		}
	}
	return skills, nil
}
