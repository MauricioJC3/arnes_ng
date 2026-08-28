package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "no-existe.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "" || c.Model != "" || c.Keys != nil {
		t.Fatalf("esperaba Config vacía, tengo %+v", c)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")

	c := Config{Provider: "deepseek", Model: "deepseek-chat"}
	c.SetKey("deepseek", "sk-secret")

	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permisos = %o, quiero 600 (tiene API keys)", perm)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "deepseek" || got.Model != "deepseek-chat" || got.Keys["deepseek"] != "sk-secret" {
		t.Fatalf("round-trip perdió datos: %+v", got)
	}
}

func TestCloneIsDeep(t *testing.T) {
	c := Config{}
	c.SetKey("openai", "k1")

	clone := c.Clone()
	clone.SetKey("openai", "k2")

	if c.Keys["openai"] != "k1" {
		t.Fatalf("Clone compartió el map: original quedó en %q", c.Keys["openai"])
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte("{roto"), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("esperaba error")
	}
}
