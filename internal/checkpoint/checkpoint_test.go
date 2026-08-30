package checkpoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

func editCall(path string) provider.ToolCall {
	in, _ := json.Marshal(map[string]string{"path": path})
	return provider.ToolCall{Name: "edit_file", Input: in}
}

func TestRewindRestoresFiles(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(existing, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "new.txt") // does not exist yet

	s := NewStore()
	s.Begin([]provider.Message{{Role: provider.RoleUser, Text: "turno 1"}}, "turno 1")

	// The turn "touches" both files: capture their prior state.
	s.Observe(editCall(existing))
	s.Observe(editCall(fresh))

	// Now the turn's tools actually change them on disk.
	if err := os.WriteFile(existing, []byte("v2-modificado"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("creado en el turno"), 0o644); err != nil {
		t.Fatal(err)
	}

	cp, err := s.Rewind(1)
	if err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if cp.Files() != 2 {
		t.Fatalf("archivos capturados = %d, quiero 2", cp.Files())
	}

	got, _ := os.ReadFile(existing)
	if string(got) != "v1" {
		t.Fatalf("a.txt = %q, quiero v1", got)
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatal("new.txt debería haber sido borrado por el rewind")
	}
	if h := cp.History(); len(h) != 1 || h[0].Text != "turno 1" {
		t.Fatalf("history = %+v", h)
	}
	if len(s.List()) != 0 {
		t.Fatalf("el checkpoint no se descartó tras el rewind: %d", len(s.List()))
	}
}

func TestObserveCapturesOncePerFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("original"), 0o644)

	s := NewStore()
	s.Begin(nil, "t")
	s.Observe(editCall(p))
	os.WriteFile(p, []byte("cambio intermedio"), 0o644)
	s.Observe(editCall(p)) // no debe re-capturar

	os.WriteFile(p, []byte("cambio final"), 0o644)
	if _, err := s.Rewind(1); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "original" {
		t.Fatalf("f.txt = %q, quiero 'original' (la primera captura)", got)
	}
}

func TestObserveIgnoresNonWriteTools(t *testing.T) {
	// workdir a un directorio sin git: bash no captura baseline y nada más
	// captura archivos.
	s := NewStore(WithWorkdir(t.TempDir()))
	s.Begin(nil, "t")
	in, _ := json.Marshal(map[string]string{"path": "x"})
	s.Observe(provider.ToolCall{Name: "bash", Input: in})
	s.Observe(provider.ToolCall{Name: "read_file", Input: in})
	cp := s.List()[0]
	if cp.Files() != 0 {
		t.Fatalf("no debería capturar archivos para bash/read_file, capturó %d", cp.Files())
	}
	if cp.git != nil {
		t.Fatal("fuera de un repo git no debería haber baseline")
	}
}

func TestRewindUnknownIndex(t *testing.T) {
	s := NewStore()
	s.Begin(nil, "t")
	if _, err := s.Rewind(99); err == nil {
		t.Fatal("esperaba error para un índice inexistente")
	}
}

func TestRewindDropsLaterCheckpoints(t *testing.T) {
	s := NewStore()
	s.Begin(nil, "uno")
	s.Begin(nil, "dos")
	s.Begin(nil, "tres")
	if len(s.List()) != 3 {
		t.Fatalf("checkpoints = %d", len(s.List()))
	}
	if _, err := s.Rewind(2); err != nil {
		t.Fatal(err)
	}
	if got := s.List(); len(got) != 1 || got[0].Label != "uno" {
		t.Fatalf("tras rewind a 2 debería quedar solo 'uno': %+v", got)
	}
	// El próximo checkpoint re-toma el slot 2.
	s.Begin(nil, "dos-bis")
	if got := s.List(); got[len(got)-1].Index != 2 {
		t.Fatalf("el nuevo checkpoint debería ser el índice 2, es %d", got[len(got)-1].Index)
	}
}

func TestBeginKeepsCap(t *testing.T) {
	s := NewStore()
	for i := 0; i < keep+10; i++ {
		s.Begin(nil, "t")
	}
	if len(s.List()) != keep {
		t.Fatalf("checkpoints retenidos = %d, quiero %d", len(s.List()), keep)
	}
}
