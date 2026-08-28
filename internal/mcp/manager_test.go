package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestConnectSkipsUnreachableServer(t *testing.T) {
	cfg := Config{MCPServers: map[string]ServerConfig{
		"roto": {Command: "arnes-no-existe-este-binario-xyz"},
	}}

	var warned []string
	m := Connect(context.Background(), cfg, func(err error) { warned = append(warned, err.Error()) })
	defer m.Close()

	if len(m.Tools()) != 0 {
		t.Fatalf("un servidor caído no debería aportar tools: %+v", m.Tools())
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "roto") {
		t.Fatalf("esperaba un warning nombrando el servidor: %v", warned)
	}
}

func TestConnectEmptyConfig(t *testing.T) {
	m := Connect(context.Background(), Config{}, nil)
	defer m.Close()
	if len(m.Tools()) != 0 {
		t.Fatalf("config vacía no debería tener tools")
	}
}
