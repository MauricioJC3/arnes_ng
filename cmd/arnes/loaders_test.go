package main

import (
	"os"
	"strings"
	"testing"
)

func TestModeAddendum(t *testing.T) {
	if s := modeAddendum(modeNormal); s != "" {
		t.Fatalf("modo normal no debería agregar nada, dio %q", s)
	}
	if s := modeAddendum(modePlan); !strings.Contains(s, "PLAN") {
		t.Fatalf("modo plan sin addendum: %q", s)
	}
	if s := modeAddendum(modeAuto); !strings.Contains(s, "AUTO") {
		t.Fatalf("modo auto sin addendum: %q", s)
	}
}

func TestHumanCount(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1500, "1.5k"},
		{999_999, "1000.0k"},
		{2_000_000, "2.0M"},
	}
	for _, c := range cases {
		if got := humanCount(c.n); got != c.want {
			t.Errorf("humanCount(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

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
