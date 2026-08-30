package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
	"github.com/MauricioJC3/arnes_ng/internal/todo"
)

func TestFileStoreRoundTrip(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	s := New("anthropic", "claude-opus-5", "/tmp/proj")
	s.Title = "primera prueba"
	s.Messages = []provider.Message{
		{Role: provider.RoleUser, Text: "hola"},
		{Role: provider.RoleAssistant, Text: "buenas"},
	}

	if err := store.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load(s.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Title != "primera prueba" || len(got.Messages) != 2 || got.Messages[1].Text != "buenas" {
		t.Fatalf("sesión mal persistida: %+v", got)
	}

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != s.ID || metas[0].Messages != 2 {
		t.Fatalf("List devolvió %+v", metas)
	}

	if err := store.Delete(s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Load(s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("después de borrar, Load debería dar ErrNotFound, dio: %v", err)
	}
}

func TestFileStoreSaveNormalizesEmptyToolInput(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	s := New("anthropic", "claude-opus-5", "/tmp/proj")
	// A tool_use block truncated by max_tokens can leave Input empty; an empty
	// json.RawMessage is invalid JSON and used to break Save with
	// "unexpected end of JSON input".
	s.Messages = []provider.Message{
		{Role: provider.RoleUser, Text: "hacé algo"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "c1", Name: "read_file", Input: nil},
			{ID: "c2", Name: "grep", Input: []byte("")},
			{ID: "c3", Name: "glob", Input: []byte(`{"pattern":"*.go"}`)},
		}},
	}

	if err := store.Save(s); err != nil {
		t.Fatalf("Save no debería fallar por un Input vacío: %v", err)
	}
	got, err := store.Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	tc := got.Messages[1].ToolCalls
	if string(tc[0].Input) != "{}" || string(tc[1].Input) != "{}" {
		t.Fatalf("Input vacío no se normalizó a '{}': %q %q", tc[0].Input, tc[1].Input)
	}
	// A valid Input survives (MarshalIndent may re-indent it, so compare parsed).
	var m map[string]string
	if err := json.Unmarshal(tc[2].Input, &m); err != nil || m["pattern"] != "*.go" {
		t.Fatalf("un Input válido no debería alterarse: %q (%v)", tc[2].Input, err)
	}
}

func TestFileStoreLoadMissing(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	if _, err := store.Load("no-existe"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("quiero ErrNotFound, tengo: %v", err)
	}
}

// fakeAgent implements the Agent interface for the persister tests.
type fakeAgent struct {
	replies       []string
	calls         int
	hist          []provider.Message
	useIn, useOut int
}

func (f *fakeAgent) Run(_ context.Context, in string) (string, error) {
	f.hist = append(f.hist, provider.Message{Role: provider.RoleUser, Text: in})
	reply := ""
	if f.calls < len(f.replies) {
		reply = f.replies[f.calls]
	}
	f.calls++
	f.hist = append(f.hist, provider.Message{Role: provider.RoleAssistant, Text: reply})
	f.useIn += 100
	f.useOut += 20
	return reply, nil
}

func (f *fakeAgent) History() []provider.Message { return f.hist }
func (f *fakeAgent) Usage() (int, int)           { return f.useIn, f.useOut }

func TestPersistingSavesEveryTurn(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	sess := New("mock", "mock-1", "/tmp")
	p := NewPersisting(&fakeAgent{replies: []string{"r1", "r2"}}, store, sess)

	if _, err := p.Run(context.Background(), "primer mensaje"); err != nil {
		t.Fatal(err)
	}
	if sess.Title != "primer mensaje" {
		t.Errorf("Title = %q, se toma del primer input", sess.Title)
	}

	if _, err := p.Run(context.Background(), "segundo"); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("mensajes persistidos = %d, quiero 4", len(got.Messages))
	}
	if got.Title != "primer mensaje" {
		t.Errorf("título persistido = %q", got.Title)
	}
	// dos turnos del fakeAgent = 200 in / 40 out acumulados
	if got.UsageIn != 200 || got.UsageOut != 40 {
		t.Errorf("uso persistido = %d/%d, quiero 200/40", got.UsageIn, got.UsageOut)
	}
	if m := got.Meta(); m.UsageIn != 200 || m.UsageOut != 40 {
		t.Errorf("Meta no proyecta el uso: %+v", m)
	}
}

func TestPersistingWithModelFnTracksRuntimeModel(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	sess := New("mock", "mock-1", "")
	model := "mock-1"
	p := NewPersisting(&fakeAgent{replies: []string{"r"}}, store, sess,
		WithModelFn(func() string { return model }))

	if p.Session() != sess {
		t.Fatal("Session() no devuelve la sesión viva")
	}

	if _, err := p.Run(context.Background(), "uno"); err != nil {
		t.Fatal(err)
	}
	model = "claude-opus-5" // el usuario hizo /model entre turnos
	if _, err := p.Run(context.Background(), "dos"); err != nil {
		t.Fatal(err)
	}

	got, _ := store.Load(sess.ID)
	if got.Model != "claude-opus-5" {
		t.Fatalf("Model persistido = %q, WithModelFn debería seguir el cambio en runtime", got.Model)
	}
}

func TestPersistingWithTodosFnSavesChecklist(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	sess := New("mock", "m", "")
	items := []todo.Item{{Content: "una", Status: todo.InProgress}}
	p := NewPersisting(&fakeAgent{replies: []string{"r1", "r2"}}, store, sess,
		WithTodosFn(func() []todo.Item { return items }))

	if _, err := p.Run(context.Background(), "arrancá"); err != nil {
		t.Fatal(err)
	}
	items = []todo.Item{
		{Content: "una", Status: todo.Done},
		{Content: "dos", Status: todo.Pending},
	}
	if _, err := p.Run(context.Background(), "seguí"); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Todos) != 2 || got.Todos[0].Status != todo.Done || got.Todos[1].Content != "dos" {
		t.Fatalf("la sesión no persistió el último estado del checklist: %+v", got.Todos)
	}
}

func TestMetaCarriesTodoProgress(t *testing.T) {
	s := New("mock", "m", "")
	s.Todos = []todo.Item{
		{Content: "a", Status: todo.Done},
		{Content: "b", Status: todo.Done},
		{Content: "c", Status: todo.InProgress},
	}
	m := s.Meta()
	if m.Todo.Done != 2 || m.Todo.Total != 3 {
		t.Fatalf("Meta.Todo = %+v, quiero 2/3", m.Todo)
	}
}

func TestTodoProgressLabel(t *testing.T) {
	cases := []struct {
		name string
		p    TodoProgress
		want string
	}{
		{"sin tareas", TodoProgress{}, ""},
		{"en progreso", TodoProgress{Done: 2, Total: 5}, "2/5 tareas"},
		{"todas hechas", TodoProgress{Done: 4, Total: 4}, "✓ tareas"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Label(); got != tt.want {
				t.Fatalf("Label() = %q, quiero %q", got, tt.want)
			}
		})
	}
}

// failOnSave wraps a real store but forces Save to fail.
type failOnSave struct{ *FileStore }

func (failOnSave) Save(*Session) error { return errors.New("disco lleno") }

func TestPersistingSurfacesSaveErrorButKeepsAnswer(t *testing.T) {
	real, _ := NewFileStore(t.TempDir())
	p := NewPersisting(&fakeAgent{replies: []string{"la respuesta"}}, failOnSave{real}, New("mock", "m", ""))

	out, err := p.Run(context.Background(), "hola")
	if out != "la respuesta" {
		t.Errorf("out = %q, la respuesta del agente no se debe perder", out)
	}
	if err == nil {
		t.Error("esperaba que el error de guardado se propague")
	}
}
