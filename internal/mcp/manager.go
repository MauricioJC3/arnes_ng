package mcp

import (
	"context"
	"sync"
	"time"

	"github.com/andresmjimenez/arnes/internal/tool"
)

// dialTimeout bounds each server's handshake so one slow server can't hang startup.
const dialTimeout = 20 * time.Second

// Manager owns the connections to every configured server and the adapted tools.
type Manager struct {
	clients []*Client
	tools   []tool.Tool
}

// Connect dials every server in cfg concurrently. Servers that fail are passed
// to warn and skipped; the harness starts regardless.
func Connect(ctx context.Context, cfg Config, warn func(error)) *Manager {
	if warn == nil {
		warn = func(error) {}
	}
	m := &Manager{}

	type result struct {
		c   *Client
		err error
	}
	results := make(chan result, len(cfg.MCPServers))
	var wg sync.WaitGroup
	for name, sc := range cfg.MCPServers {
		wg.Add(1)
		go func(name string, sc ServerConfig) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, dialTimeout)
			defer cancel()
			c, err := Dial(cctx, name, sc)
			results <- result{c, err}
		}(name, sc)
	}
	go func() { wg.Wait(); close(results) }()

	for r := range results {
		if r.err != nil {
			warn(r.err)
			continue
		}
		m.clients = append(m.clients, r.c)
		for _, info := range r.c.Tools() {
			m.tools = append(m.tools, adaptedTool{
				client: r.c,
				info:   info,
				name:   r.c.Server() + "__" + info.Name,
			})
		}
	}
	return m
}

// Tools returns every adapted MCP tool, ready to add to a tool.Registry.
func (m *Manager) Tools() []tool.Tool { return m.tools }

// Close shuts down every server process.
func (m *Manager) Close() {
	for _, c := range m.clients {
		_ = c.Close()
	}
}
