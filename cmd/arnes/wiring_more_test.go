package main

import (
	"testing"

	"github.com/MauricioJC3/arnes_ng/internal/config"
)

func TestIsFalseyIsTruthy(t *testing.T) {
	for _, s := range []string{"0", "false", "OFF", " no ", "False"} {
		if !isFalsey(s) {
			t.Errorf("isFalsey(%q) = false", s)
		}
	}
	for _, s := range []string{"1", "true", "ON", " yes ", "True"} {
		if !isTruthy(s) {
			t.Errorf("isTruthy(%q) = false", s)
		}
	}
	for _, s := range []string{"", "maybe", "2", "onoff"} {
		if isFalsey(s) || isTruthy(s) {
			t.Errorf("%q no debería ser ni falsey ni truthy", s)
		}
	}
}

func TestStartupConfigLayersEnv(t *testing.T) {
	base := config.Config{Provider: "anthropic", Model: "claude-x"}

	t.Run("sin env no cambia nada", func(t *testing.T) {
		t.Setenv("ARNES_PROVIDER", "")
		t.Setenv("ARNES_MODEL", "")
		got := startupConfig(base)
		if got.Provider != "anthropic" || got.Model != "claude-x" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("el env pisa provider y model, sin mutar el original", func(t *testing.T) {
		t.Setenv("ARNES_PROVIDER", "openai")
		t.Setenv("ARNES_MODEL", "gpt-4o")
		got := startupConfig(base)
		if got.Provider != "openai" || got.Model != "gpt-4o" {
			t.Fatalf("got %+v", got)
		}
		if base.Provider != "anthropic" {
			t.Fatal("startupConfig mutó el config original")
		}
	})
}
