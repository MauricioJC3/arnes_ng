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

	c := Config{Provider: "deepseek", Model: "deepseek-chat", ProtectedPaths: []string{".env", "secrets/*"}}
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
	if len(got.ProtectedPaths) != 2 || got.ProtectedPaths[0] != ".env" || got.ProtectedPaths[1] != "secrets/*" {
		t.Fatalf("round-trip perdió protected_paths: %+v", got.ProtectedPaths)
	}
}

func TestCloneIsDeep(t *testing.T) {
	c := Config{ProtectedPaths: []string{".env"}}
	c.SetKey("openai", "k1")

	clone := c.Clone()
	clone.SetKey("openai", "k2")
	clone.ProtectedPaths[0] = "changed"

	if c.Keys["openai"] != "k1" {
		t.Fatalf("Clone compartió el map: original quedó en %q", c.Keys["openai"])
	}
	if c.ProtectedPaths[0] != ".env" {
		t.Fatalf("Clone compartió el slice: original quedó en %q", c.ProtectedPaths[0])
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte("{roto"), 0o644)
	if _, err := Load(path); err == nil {
		t.Fatal("esperaba error")
	}
}

// TestSaveSurfacesWriteFailure: if the config can't be written (e.g. a read-only
// parent), Save must return an error so /connect doesn't claim a key was saved
// when it wasn't.
func TestSaveSurfacesWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignora los permisos de directorio")
	}
	ro := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	c := Config{Provider: "deepseek"}
	c.SetKey("deepseek", "sk-secret")
	if err := c.Save(filepath.Join(ro, "sub", "config.json")); err == nil {
		t.Fatal("guardar bajo un directorio de solo lectura debería fallar, no silenciarse")
	}
}
