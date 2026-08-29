package main

import (
	"os"
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/config"
	"github.com/MauricioJC3/arnes_ng/internal/provider"
)

func TestLoadSubagentsFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sa.json"
	if err := os.WriteFile(path, []byte(`[{"name":"x","system":"s","description":"d"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARNES_SUBAGENTS", path)

	defs, err := loadSubagents()
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "x" {
		t.Fatalf("defs = %+v", defs)
	}
}

func TestCostLine(t *testing.T) {
	if got := costLine("claude-opus-5", 1_000_000, 0); got != "$5.0000" {
		t.Fatalf("costLine opus-5 1M in = %q, quiero $5.0000", got)
	}
	if got := costLine("modelo-fantasma", 999, 999); got != "" {
		t.Fatalf("un modelo sin tarifa debería dar cadena vacía, dio %q", got)
	}
}

func TestCompactionFromEnv(t *testing.T) {
	t.Run("sliding por defecto", func(t *testing.T) {
		t.Setenv("ARNES_COMPACT", "")
		s, at, err := compactionFromEnv(provider.NewMock())
		if err != nil || s == nil || s.Name() != "sliding-window" || at != 120000 {
			t.Fatalf("s=%v at=%d err=%v", s, at, err)
		}
	})

	t.Run("off explícito desactiva", func(t *testing.T) {
		t.Setenv("ARNES_COMPACT", "off")
		s, at, err := compactionFromEnv(provider.NewMock())
		if err != nil || s != nil || at != 0 {
			t.Fatalf("s=%v at=%d err=%v", s, at, err)
		}
	})

	t.Run("sliding con umbral custom", func(t *testing.T) {
		t.Setenv("ARNES_COMPACT", "sliding")
		t.Setenv("ARNES_COMPACT_AT", "50000")
		s, at, err := compactionFromEnv(provider.NewMock())
		if err != nil {
			t.Fatal(err)
		}
		if s == nil || s.Name() != "sliding-window" || at != 50000 {
			t.Fatalf("s=%v at=%d", s, at)
		}
	})

	t.Run("umbral inválido es error", func(t *testing.T) {
		t.Setenv("ARNES_COMPACT", "summarize")
		t.Setenv("ARNES_COMPACT_AT", "muchos")
		if _, _, err := compactionFromEnv(provider.NewMock()); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestMaxTokensFromEnv(t *testing.T) {
	t.Run("nada seteado devuelve 0 (el app pone el default)", func(t *testing.T) {
		t.Setenv("ARNES_MAX_TOKENS", "")
		n, err := maxTokensFromEnv(config.Config{})
		if err != nil || n != 0 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})

	t.Run("el env gana sobre la config", func(t *testing.T) {
		t.Setenv("ARNES_MAX_TOKENS", "16000")
		n, err := maxTokensFromEnv(config.Config{MaxTokens: 4096})
		if err != nil || n != 16000 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})

	t.Run("sin env cae a la config", func(t *testing.T) {
		t.Setenv("ARNES_MAX_TOKENS", "")
		n, err := maxTokensFromEnv(config.Config{MaxTokens: 4096})
		if err != nil || n != 4096 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})

	t.Run("valor inválido es error", func(t *testing.T) {
		t.Setenv("ARNES_MAX_TOKENS", "-3")
		if _, err := maxTokensFromEnv(config.Config{}); err == nil {
			t.Fatal("esperaba error")
		}
	})
}

func TestMaxStepsFromEnv(t *testing.T) {
	t.Run("nada seteado devuelve 0 (el app pone el default)", func(t *testing.T) {
		t.Setenv("ARNES_MAX_STEPS", "")
		n, err := maxStepsFromEnv(config.Config{})
		if err != nil || n != 0 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})

	t.Run("el env gana sobre la config", func(t *testing.T) {
		t.Setenv("ARNES_MAX_STEPS", "200")
		n, err := maxStepsFromEnv(config.Config{MaxSteps: 30})
		if err != nil || n != 200 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})

	t.Run("sin env cae a la config", func(t *testing.T) {
		t.Setenv("ARNES_MAX_STEPS", "")
		n, err := maxStepsFromEnv(config.Config{MaxSteps: 30})
		if err != nil || n != 30 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})

	t.Run("valor inválido es error", func(t *testing.T) {
		t.Setenv("ARNES_MAX_STEPS", "un montón")
		if _, err := maxStepsFromEnv(config.Config{}); err == nil {
			t.Fatal("esperaba error")
		}
	})
}
