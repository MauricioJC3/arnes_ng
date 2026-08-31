package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/mcp"
)

// MCPList implements command.MCPAdmin: show the configured MCP servers. Live
// connection state is not tracked here -- the pool is fixed at startup -- so
// this reports what mcp.json holds.
func (a *App) MCPList() (string, error) {
	path, err := mcp.ResolvePath()
	if err != nil {
		return "", err
	}
	cfg, err := mcp.LoadFile(path)
	if err != nil {
		return "", err
	}
	if len(cfg.MCPServers) == 0 {
		return "no hay servidores MCP configurados (" + path + ")", nil
	}
	names := make([]string, 0, len(cfg.MCPServers))
	for n := range cfg.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteByte('\n')
		}
		sc := cfg.MCPServers[n]
		target := sc.URL
		if target == "" {
			target = sc.Command
			if len(sc.Args) > 0 {
				target += " " + strings.Join(sc.Args, " ")
			}
		}
		fmt.Fprintf(&b, "  %s  →  %s", n, target)
	}
	return b.String(), nil
}

// MCPAdd implements command.MCPAdmin: add an HTTP MCP server to mcp.json.
func (a *App) MCPAdd(name, url string) (string, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("la url debe empezar con http:// o https:// (para servidores stdio editá mcp.json a mano)")
	}
	path, err := mcp.ResolvePath()
	if err != nil {
		return "", err
	}
	if err := mcp.AddServer(path, name, mcp.ServerConfig{URL: url}); err != nil {
		return "", err
	}
	return fmt.Sprintf("servidor MCP %q agregado en %s — reiniciá arnes para conectarlo", name, path), nil
}

// MCPRemove implements command.MCPAdmin: drop a server from mcp.json.
func (a *App) MCPRemove(name string) (string, error) {
	path, err := mcp.ResolvePath()
	if err != nil {
		return "", err
	}
	if err := mcp.RemoveServer(path, name); err != nil {
		return "", err
	}
	return fmt.Sprintf("servidor MCP %q eliminado de %s — reiniciá arnes para soltarlo", name, path), nil
}
