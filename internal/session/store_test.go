package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreSaveRejectsEmptyID(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Save(&Session{}); err == nil {
		t.Fatal("guardar una sesión sin id debería fallar, no escribir '.json'")
	}
}

func TestFileStoreLoadCorruptSurfacesError(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rota.json"), []byte("{ no es json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := fs.Load("rota")
	if err == nil {
		t.Fatal("una sesión corrupta debería dar error, no una sesión vacía")
	}
	if s != nil {
		t.Fatalf("no debería devolver sesión junto al error: %+v", s)
	}
}

func TestFileStoreListSkipsCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	good := New("mock", "m", "")
	if err := fs.Save(good); err != nil {
		t.Fatal(err)
	}
	// un archivo .json corrupto en el mismo dir no debe romper el listado
	if err := os.WriteFile(filepath.Join(dir, "basura.json"), []byte("a medio escribir"), 0o644); err != nil {
		t.Fatal(err)
	}

	metas, err := fs.List()
	if err != nil {
		t.Fatalf("List no debería fallar por un archivo corrupto: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != good.ID {
		t.Fatalf("esperaba solo la sesión sana, tengo %+v", metas)
	}
}

func TestFileStoreDeleteMissing(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Delete("20990101-000000-ffff"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("borrar una sesión inexistente debería dar ErrNotFound, dio %v", err)
	}
}
