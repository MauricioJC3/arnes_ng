package session

import (
	"context"
	"errors"
	"testing"

	"github.com/andresmjimenez/arnes/internal/provider"
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

func TestFileStoreLoadMissing(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	if _, err := store.Load("no-existe"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("quiero ErrNotFound, tengo: %v", err)
	}
}

// fakeAgent implements the Agent interface for the persister tests.
type fakeAgent struct {
	replies      []string
	calls        int
	hist         []provider.Message
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
