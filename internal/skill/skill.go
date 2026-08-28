// Package skill loads reusable instruction files (SKILL.md, the same format
// Claude Code uses) so the agent can pull a task-specific playbook into a turn
// on demand via the `skill` tool.
package skill

import (
	"bufio"
	"fmt"
	"strings"
)

// Skill is one loaded SKILL.md: its frontmatter name/description plus the full
// markdown body the model reads when it invokes the skill.
type Skill struct {
	Name        string
	Description string
	Path        string // source file, for diagnostics
	Body        string // everything after the frontmatter
}

// parse splits a SKILL.md into frontmatter fields and body. The frontmatter is a
// leading `---` fenced block of simple `key: value` lines (name, description).
// A file without frontmatter is still valid: the whole thing is the body and
// name/description stay empty for the caller to fill from the path.
func parse(content string) Skill {
	s := Skill{}
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)

	lines := make([]string, 0, 64)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	i := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		i = 1
		for ; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				i++
				break
			}
			key, val, ok := strings.Cut(lines[i], ":")
			if !ok {
				continue
			}
			val = strings.TrimSpace(strings.Trim(strings.TrimSpace(val), `"'`))
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "name":
				s.Name = val
			case "description":
				s.Description = val
			}
		}
	}
	s.Body = strings.TrimSpace(strings.Join(lines[i:], "\n"))
	return s
}

// Registry indexes skills by name, preserving load order. Later duplicates of a
// name are ignored (project skills load before global ones, so they win).
type Registry struct {
	byName map[string]Skill
	order  []string
}

// NewRegistry indexes skills by name; entries without a name and later
// duplicates are dropped.
func NewRegistry(skills ...Skill) *Registry {
	r := &Registry{byName: make(map[string]Skill, len(skills))}
	for _, s := range skills {
		if s.Name == "" {
			continue
		}
		if _, dup := r.byName[s.Name]; dup {
			continue
		}
		r.byName[s.Name] = s
		r.order = append(r.order, s.Name)
	}
	return r
}

// Get returns the skill registered under name.
func (r *Registry) Get(name string) (Skill, bool) {
	s, ok := r.byName[name]
	return s, ok
}

// All returns the skills in load order.
func (r *Registry) All() []Skill {
	out := make([]Skill, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.byName[n])
	}
	return out
}

// Len reports how many skills are registered.
func (r *Registry) Len() int { return len(r.order) }

// Catalog renders "- name: description" lines for the tool description.
func (r *Registry) Catalog() string {
	var b strings.Builder
	for _, n := range r.order {
		s := r.byName[n]
		desc := s.Description
		if desc == "" {
			desc = "(sin descripción)"
		}
		fmt.Fprintf(&b, "  - %s: %s\n", s.Name, desc)
	}
	return strings.TrimRight(b.String(), "\n")
}
