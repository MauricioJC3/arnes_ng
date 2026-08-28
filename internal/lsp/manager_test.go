package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestManagerLazyStartAndReuse(t *testing.T) {
	var mu sync.Mutex
	starts := 0
	m := NewManager(Config{Servers: map[string]ServerConfig{".go": {Command: "fake"}}}, "/proj")
	m.start = func(ctx context.Context, root string, sc ServerConfig) (*Client, error) {
		mu.Lock()
		starts++
		mu.Unlock()
		return &Client{root: root, diags: map[string][]Diagnostic{}, waiters: map[string][]chan struct{}{}}, nil
	}

	c1, err := m.For(context.Background(), "a.go")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := m.For(context.Background(), filepath.Join("sub", "b.go"))
	if err != nil {
		t.Fatal(err)
	}
	if c1 != c2 {
		t.Fatal("el mismo lenguaje debería reusar el cliente")
	}
	if starts != 1 {
		t.Fatalf("starts = %d, quiero 1", starts)
	}
}

func TestManagerNoServerForExtension(t *testing.T) {
	m := NewManager(Config{Servers: map[string]ServerConfig{".go": {Command: "fake"}}}, "/proj")
	if _, err := m.For(context.Background(), "script.py"); err == nil {
		t.Fatal("esperaba error: no hay server para .py")
	}
}

func TestManagerStartErrorNotCached(t *testing.T) {
	m := NewManager(Config{Servers: map[string]ServerConfig{".go": {Command: "fake"}}}, "/proj")
	calls := 0
	m.start = func(context.Context, string, ServerConfig) (*Client, error) {
		calls++
		return nil, errors.New("no arrancó")
	}
	if _, err := m.For(context.Background(), "a.go"); err == nil {
		t.Fatal("esperaba error")
	}
	if _, err := m.For(context.Background(), "a.go"); err == nil {
		t.Fatal("esperaba error de nuevo")
	}
	if calls != 2 {
		t.Fatalf("un start fallido no debe cachearse: calls = %d", calls)
	}
}

func TestLoadFileDefaults(t *testing.T) {
	c, err := LoadFile(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.serverFor("x.go"); !ok {
		t.Fatal("el default debería traer gopls para .go")
	}
}

func TestLoadFileNormalizesExtension(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lsp.json")
	if err := os.WriteFile(p, []byte(`{"servers":{"py":{"command":"pyright","args":["--stdio"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	sc, ok := c.serverFor("main.py")
	if !ok || sc.Command != "pyright" || len(sc.Args) != 1 {
		t.Fatalf("server .py mal cargado: %+v ok=%v", sc, ok)
	}
}
