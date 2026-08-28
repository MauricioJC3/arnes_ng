package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile(t *testing.T) {
	t.Run("archivo ausente = config vacía", func(t *testing.T) {
		c, err := LoadFile(filepath.Join(t.TempDir(), "no-existe.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(c.MCPServers) != 0 {
			t.Fatalf("esperaba vacío, tengo %+v", c.MCPServers)
		}
	})

	t.Run("JSON válido se parsea", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "mcp.json")
		body := `{"mcpServers":{"fs":{"command":"npx","args":["-y","srv"],"env":{"K":"V"}}}}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		c, err := LoadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fs, ok := c.MCPServers["fs"]
		if !ok || fs.Command != "npx" || fs.Args[1] != "srv" || fs.Env["K"] != "V" {
			t.Fatalf("parseo inesperado: %+v", c.MCPServers)
		}
	})

	t.Run("falta command es error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "mcp.json")
		_ = os.WriteFile(path, []byte(`{"mcpServers":{"x":{"args":["a"]}}}`), 0o644)
		if _, err := LoadFile(path); err == nil {
			t.Fatal("esperaba error por 'command' faltante")
		}
	})

	t.Run("JSON roto es error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "mcp.json")
		_ = os.WriteFile(path, []byte("{roto"), 0o644)
		if _, err := LoadFile(path); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestEnvSlice(t *testing.T) {
	got := envSlice(map[string]string{"A": "1"})
	if len(got) != 1 || got[0] != "A=1" {
		t.Fatalf("envSlice = %v", got)
	}
}
