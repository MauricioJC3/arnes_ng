package skill

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// wantDefaults is the curated set bundled with arnes. Keep in sync with the
// directories under defaults/.
var wantDefaults = []string{
	"architecture-review",
	"docker",
	"docker-compose",
	"software-architecture",
	"tdd-workflow",
}

func TestDefaults(t *testing.T) {
	skills, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, s := range skills {
		names = append(names, s.Name)
		if s.Description == "" {
			t.Errorf("skill %q sin description", s.Name)
		}
		if strings.TrimSpace(s.Body) == "" {
			t.Errorf("skill %q con cuerpo vacío", s.Name)
		}
	}
	sort.Strings(names)

	if strings.Join(names, ",") != strings.Join(wantDefaults, ",") {
		t.Fatalf("skills embebidas = %v, quiero %v", names, wantDefaults)
	}
}

func TestSeedDefaultsFreshDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills") // no existe todavía

	seeded, err := SeedDefaults(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(seeded)
	if strings.Join(seeded, ",") != strings.Join(wantDefaults, ",") {
		t.Fatalf("seeded = %v, quiero %v", seeded, wantDefaults)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != len(wantDefaults) {
		t.Fatalf("Load tras seed = %d skills, quiero %d", len(loaded), len(wantDefaults))
	}

	body, err := os.ReadFile(filepath.Join(dir, "docker", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "multi-stage") {
		t.Fatalf("SKILL.md de docker no tiene el contenido esperado")
	}
}

func TestSeedDefaultsAddsMissingKeepsExisting(t *testing.T) {
	dir := t.TempDir() // ya existe, con skills del usuario adentro
	writeSkill(t, dir, "docker", "---\nname: docker\ndescription: mío\n---\nmi versión\n")
	writeSkill(t, dir, "deploy", "---\nname: deploy\ndescription: mío\n---\ndeploy propio\n")

	seeded, err := SeedDefaults(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(seeded)
	want := []string{"architecture-review", "docker-compose", "software-architecture", "tdd-workflow"}
	if strings.Join(seeded, ",") != strings.Join(want, ",") {
		t.Fatalf("seeded = %v, quiero solo los que faltaban: %v", seeded, want)
	}

	// el 'docker' del usuario quedó intacto
	body, _ := os.ReadFile(filepath.Join(dir, "docker", "SKILL.md"))
	if !strings.Contains(string(body), "mi versión") {
		t.Fatalf("SeedDefaults pisó el skill del usuario: %q", body)
	}
	// su 'deploy' sigue ahí
	if _, err := os.Stat(filepath.Join(dir, "deploy", "SKILL.md")); err != nil {
		t.Fatalf("SeedDefaults tocó un skill ajeno: %v", err)
	}
}

func TestSeedDefaultsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")

	if _, err := SeedDefaults(dir); err != nil {
		t.Fatal(err)
	}
	seeded, err := SeedDefaults(dir) // segunda vuelta: ya están todos
	if err != nil {
		t.Fatal(err)
	}
	if len(seeded) != 0 {
		t.Fatalf("segunda llamada sembró %v, quiero nada", seeded)
	}
}

func TestSeedDefaultsReAddsDeleted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	if _, err := SeedDefaults(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(filepath.Join(dir, "docker")); err != nil {
		t.Fatal(err)
	}

	seeded, err := SeedDefaults(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(seeded, ",") != "docker" {
		t.Fatalf("seeded = %v, quiero que vuelva 'docker'", seeded)
	}
	if _, err := os.Stat(filepath.Join(dir, "docker", "SKILL.md")); err != nil {
		t.Fatalf("'docker' no volvió: %v", err)
	}
}

func TestSeedDefaultsPreservesUserEdit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	if _, err := SeedDefaults(dir); err != nil {
		t.Fatal(err)
	}

	edited := "---\nname: docker\ndescription: editado por mí\n---\ncontenido propio\n"
	if err := os.WriteFile(filepath.Join(dir, "docker", "SKILL.md"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SeedDefaults(dir); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "docker", "SKILL.md"))
	if string(body) != edited {
		t.Fatalf("SeedDefaults sobreescribió una edición del usuario: %q", body)
	}
}
