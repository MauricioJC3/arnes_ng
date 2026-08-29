package main

import (
	"os"
	"testing"
)

func TestLoadHooksFromEnv(t *testing.T) {
	t.Run("archivo ausente = config vacío", func(t *testing.T) {
		t.Setenv("ARNES_HOOKS", t.TempDir()+"/no-existe.json")
		cfg, err := loadHooks()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Empty() {
			t.Fatalf("un archivo ausente debería dar config vacío: %+v", cfg)
		}
	})

	t.Run("archivo con un pre-hook", func(t *testing.T) {
		path := t.TempDir() + "/hooks.json"
		body := `{"pre_tool":[{"match":"bash","command":"echo hola"}]}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ARNES_HOOKS", path)

		cfg, err := loadHooks()
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.PreTool) != 1 {
			t.Fatalf("esperaba 1 pre-hook, tengo %+v", cfg.PreTool)
		}
	})
}

func TestLoadLSPFromEnv(t *testing.T) {
	t.Run("archivo ausente = config default", func(t *testing.T) {
		t.Setenv("ARNES_LSP", t.TempDir()+"/no-existe.json")
		cfg, err := loadLSP()
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Servers) == 0 {
			t.Fatal("un archivo ausente debería caer al default (gopls)")
		}
	})

	t.Run("json inválido es error", func(t *testing.T) {
		path := t.TempDir() + "/lsp.json"
		if err := os.WriteFile(path, []byte("{no es json"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ARNES_LSP", path)
		if _, err := loadLSP(); err == nil {
			t.Fatal("esperaba error con json inválido")
		}
	})
}
