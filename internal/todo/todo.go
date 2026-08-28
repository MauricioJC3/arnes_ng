// Package todo holds the checklist for the current task: a short, ordered list
// of work items the model maintains through the todo_write tool and the TUI
// renders live so the user can see progress.
package todo

import "sync"

// Status is where an item stands.
type Status string

const (
	Pending    Status = "pending"
	InProgress Status = "in_progress"
	Done       Status = "completed"
)

// Valid reports whether s is one of the known statuses.
func (s Status) Valid() bool {
	switch s {
	case Pending, InProgress, Done:
		return true
	default:
		return false
	}
}

// Item is one checklist entry.
type Item struct {
	Content string `json:"content"`
	Status  Status `json:"status"`
}

// Store holds the current list. It is safe for concurrent use: the tool writes
// from the agent goroutine, the front-end reads from the UI goroutine.
type Store struct {
	mu    sync.RWMutex
	items []Item
	onSet func([]Item)
}

// NewStore returns an empty store.
func NewStore() *Store { return &Store{} }

// OnChange registers a callback fired after every Set with a copy of the new
// list. Pass nil to clear it. Only one callback is kept.
func (s *Store) OnChange(fn func([]Item)) {
	s.mu.Lock()
	s.onSet = fn
	s.mu.Unlock()
}

// Set replaces the whole list (the model always sends the full checklist) and
// notifies the callback, if any.
func (s *Store) Set(items []Item) {
	cp := append([]Item(nil), items...)
	s.mu.Lock()
	s.items = cp
	fn := s.onSet
	s.mu.Unlock()
	if fn != nil {
		fn(append([]Item(nil), cp...))
	}
}

// Get returns a copy of the current list.
func (s *Store) Get() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Item(nil), s.items...)
}

// Counts returns how many items are done and the total.
func (s *Store) Counts() (done, total int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, it := range s.items {
		if it.Status == Done {
			done++
		}
	}
	return done, len(s.items)
}
