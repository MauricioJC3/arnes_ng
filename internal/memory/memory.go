// Package memory is the harness's persistent memory: short notes the model
// chooses to save with `remember` and later find with `recall`, surviving
// across sessions.
package memory

import "time"

// Note is one remembered fact. Project scopes it to a codebase (see DetectID);
// empty means a pre-scoping / global note, which every project still sees.
type Note struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Tags      []string  `json:"tags,omitempty"`
	Project   string    `json:"project,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Store holds notes.
type Store interface {
	// Add saves a note and returns it with its assigned id.
	Add(text string, tags []string) (Note, error)
	// Search returns notes containing every space-separated term in query
	// (case-insensitive substring) and, when tags are given, at least one of
	// them. Newest first, capped at limit (<=0 uses a default).
	Search(query string, tags []string, limit int) ([]Note, error)
	// All returns every note, newest first.
	All() ([]Note, error)
}

const defaultSearchLimit = 10
