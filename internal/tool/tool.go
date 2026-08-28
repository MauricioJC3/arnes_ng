// Package tool defines the contract every harness capability implements and a
// registry to look them up by name.
package tool

import (
	"context"
	"encoding/json"
)

// Tool is one action the agent can take on the local machine. Description and
// InputSchema are what the model sees: if they are vague, the model will not
// know when or how to call the tool.
type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// Registry holds the available tools and preserves insertion order so the
// definitions sent to the model are deterministic.
type Registry struct {
	byName map[string]Tool
	order  []string
}

// NewRegistry indexes the given tools by name. Later duplicates are ignored.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{byName: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		if _, exists := r.byName[t.Name()]; exists {
			continue
		}
		r.byName[t.Name()] = t
		r.order = append(r.order, t.Name())
	}
	return r
}

// Get returns the tool registered under name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

// All returns the tools in registration order.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.byName[name])
	}
	return out
}

// Subset returns a new Registry with only the named tools that exist in r, in
// the order given. Unknown names are skipped.
func (r *Registry) Subset(names ...string) *Registry {
	picked := make([]Tool, 0, len(names))
	for _, n := range names {
		if t, ok := r.byName[n]; ok {
			picked = append(picked, t)
		}
	}
	return NewRegistry(picked...)
}

// With returns a new Registry holding r's tools plus the given ones.
func (r *Registry) With(tools ...Tool) *Registry {
	return NewRegistry(append(r.All(), tools...)...)
}
