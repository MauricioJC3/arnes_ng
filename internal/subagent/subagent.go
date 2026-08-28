// Package subagent adds delegation: the main agent can hand a scoped task to a
// named specialized subagent, which runs its own inner loop and reports back.
package subagent

// Definition describes a specialized subagent.
type Definition struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	System         string   `json:"system"`
	Tools          []string `json:"tools,omitempty"`           // tool names; empty = all (never delegate)
	Model          string   `json:"model,omitempty"`           // optional model override
	InheritHistory bool     `json:"inherit_history,omitempty"` // start from the parent's history
}

// Registry indexes definitions by name, preserving insertion order.
type Registry struct {
	byName map[string]Definition
	order  []string
}

// NewRegistry indexes defs by name. Entries without a name, and later
// duplicates, are ignored.
func NewRegistry(defs ...Definition) *Registry {
	r := &Registry{byName: make(map[string]Definition, len(defs))}
	for _, d := range defs {
		if d.Name == "" {
			continue
		}
		if _, exists := r.byName[d.Name]; exists {
			continue
		}
		r.byName[d.Name] = d
		r.order = append(r.order, d.Name)
	}
	return r
}

// Get returns the definition registered under name.
func (r *Registry) Get(name string) (Definition, bool) {
	d, ok := r.byName[name]
	return d, ok
}

// All returns the definitions in registration order.
func (r *Registry) All() []Definition {
	out := make([]Definition, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.byName[n])
	}
	return out
}

// Len reports how many subagents are registered.
func (r *Registry) Len() int { return len(r.order) }

// Defaults are the built-in subagents used when no config file exists.
func Defaults() []Definition {
	return []Definition{
		{
			Name:        "research",
			Description: "Explora el código y responde preguntas amplias de 'cómo funciona X' o 'dónde está Y'. No modifica nada; devuelve un resumen con archivos y hallazgos.",
			System: "Sos un agente de investigación de código. Explorás con grep (buscar texto/patrones), " +
				"glob (encontrar archivos por patrón) y read_file. Usá bash solo para cosas que esas tres no cubren. " +
				"No modificás nada. Devolvés un resumen conciso: qué encontraste, en qué archivos y las líneas relevantes.",
			Tools: []string{"grep", "glob", "read_file", "bash"},
		},
		{
			Name:        "test-writer",
			Description: "Escribe tests para código existente. Pasale la ruta del archivo a testear en la tarea.",
			System: "Sos un agente que escribe tests idiomáticos y table-driven. Leé el código objetivo, escribí el archivo " +
				"_test.go al lado y verificá que compile. Devolvé qué casos cubriste.",
		},
	}
}
