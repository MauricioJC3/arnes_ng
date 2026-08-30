package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileTrackerGuardWrite(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.go")
	if err := os.WriteFile(existing, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "new.go")

	t.Run("nil tracker permite todo", func(t *testing.T) {
		var tr *FileTracker
		if err := tr.GuardWrite(existing); err != nil {
			t.Fatalf("nil tracker no debería bloquear: %v", err)
		}
	})

	t.Run("archivo existente no leído: bloquea", func(t *testing.T) {
		tr := NewFileTracker()
		if err := tr.GuardWrite(existing); err == nil {
			t.Fatal("esperaba que bloqueara un archivo existente sin lectura previa")
		}
	})

	t.Run("archivo nuevo: permite sin lectura previa", func(t *testing.T) {
		tr := NewFileTracker()
		if err := tr.GuardWrite(missing); err != nil {
			t.Fatalf("un archivo nuevo no necesita lectura previa: %v", err)
		}
	})

	t.Run("tras MarkRead: permite, y da igual la forma de la ruta", func(t *testing.T) {
		tr := NewFileTracker()
		tr.MarkRead(existing)
		rel, err := filepath.Rel(mustGetwd(t), existing)
		if err == nil {
			if err := tr.GuardWrite(rel); err != nil {
				t.Fatalf("misma ruta en forma relativa debería estar permitida: %v", err)
			}
		}
		if err := tr.GuardWrite(existing); err != nil {
			t.Fatalf("tras MarkRead debería permitir: %v", err)
		}
	})
}

func TestReadFileMarksRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.go")
	if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := NewFileTracker()

	if _, err := (ReadFile{Tracker: tr}).Execute(context.Background(),
		mustJSON(t, map[string]string{"path": p})); err != nil {
		t.Fatal(err)
	}
	if err := tr.GuardWrite(p); err != nil {
		t.Fatalf("read_file debería haber marcado el archivo como leído: %v", err)
	}
}

func TestEditFileBlocksUnreadFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.go")
	if err := os.WriteFile(p, []byte("hola\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := NewFileTracker()

	_, err := (EditFile{Tracker: tr}).Execute(context.Background(),
		mustJSON(t, map[string]any{"path": p, "old": "hola", "new": "chau"}))
	if err == nil {
		t.Fatal("edit_file sobre un archivo no leído debería fallar")
	}

	// After a read it goes through, and a second edit needs no re-read.
	if _, err := (ReadFile{Tracker: tr}).Execute(context.Background(),
		mustJSON(t, map[string]string{"path": p})); err != nil {
		t.Fatal(err)
	}
	if _, err := (EditFile{Tracker: tr}).Execute(context.Background(),
		mustJSON(t, map[string]any{"path": p, "old": "hola", "new": "chau"})); err != nil {
		t.Fatalf("tras leerlo, edit_file debería andar: %v", err)
	}
	if _, err := (EditFile{Tracker: tr}).Execute(context.Background(),
		mustJSON(t, map[string]any{"path": p, "old": "chau", "new": "hola"})); err != nil {
		t.Fatalf("una segunda edición no debería exigir re-lectura: %v", err)
	}
}

func TestWriteFileGuardAndMark(t *testing.T) {
	dir := t.TempDir()
	tr := NewFileTracker()

	// A brand-new file: allowed, and now counts as known.
	newp := filepath.Join(dir, "new.go")
	if _, err := (WriteFile{Tracker: tr}).Execute(context.Background(),
		mustJSON(t, map[string]string{"path": newp, "content": "package x\n"})); err != nil {
		t.Fatalf("write_file de un archivo nuevo: %v", err)
	}
	if _, err := (EditFile{Tracker: tr}).Execute(context.Background(),
		mustJSON(t, map[string]any{"path": newp, "old": "package x", "new": "package y"})); err != nil {
		t.Fatalf("tras escribirlo debería poder editarse sin re-lectura: %v", err)
	}

	// An existing file the model never read: write_file is refused too.
	other := filepath.Join(dir, "other.go")
	if err := os.WriteFile(other, []byte("package z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (WriteFile{Tracker: tr}).Execute(context.Background(),
		mustJSON(t, map[string]string{"path": other, "content": "nope"})); err == nil {
		t.Fatal("write_file sobre un archivo existente no leído debería fallar")
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}
