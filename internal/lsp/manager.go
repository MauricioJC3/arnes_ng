package lsp

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// starter builds a client for a server config; swapped out in tests.
type starter func(ctx context.Context, root string, sc ServerConfig) (*Client, error)

func realStarter(ctx context.Context, root string, sc ServerConfig) (*Client, error) {
	if _, err := exec.LookPath(sc.Command); err != nil {
		return nil, fmt.Errorf("no encuentro %q en el PATH; instalá el language server o configuralo en ~/.arnes/lsp.json", sc.Command)
	}
	return Start(ctx, root, sc.Command, sc.Args)
}

// Manager lazily starts one language server per file extension and keeps it
// alive for the process. It is safe for concurrent use.
type Manager struct {
	cfg   Config
	root  string
	start starter

	mu      sync.Mutex
	clients map[string]*Client // keyed by extension
}

// NewManager builds a manager rooted at workspace root.
func NewManager(cfg Config, root string) *Manager {
	return &Manager{cfg: cfg, root: root, start: realStarter, clients: map[string]*Client{}}
}

// Configured reports whether any server is set up (for the startup summary).
func (m *Manager) Configured() int { return len(m.cfg.Servers) }

// For returns the running client for path's language, starting it on first use.
// It returns a clear error when no server is configured for that file type.
func (m *Manager) For(ctx context.Context, path string) (*Client, error) {
	ext := strings.ToLower(filepath.Ext(path))
	sc, ok := m.cfg.serverFor(path)
	if !ok {
		return nil, fmt.Errorf("no hay language server configurado para %q (configuralo en ~/.arnes/lsp.json)", ext)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.clients[ext]; c != nil {
		return c, nil
	}
	c, err := m.start(ctx, m.root, sc)
	if err != nil {
		return nil, err
	}
	m.clients[ext] = c
	return c, nil
}

// CloseAll shuts every running server down.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ext, c := range m.clients {
		_ = c.Close()
		delete(m.clients, ext)
	}
}
