package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, name, content string) {
	t.Helper()
	sd := filepath.Join(dir, name)
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParse(t *testing.T) {
	s := parse("---\nname: deploy\ndescription: \"cómo deployar\"\n---\n\n# Deploy\n\npaso 1\n")
	if s.Name != "deploy" || s.Description != "cómo deployar" {
		t.Fatalf("frontmatter mal parseado: %+v", s)
	}
	if !strings.HasPrefix(s.Body, "# Deploy") || !strings.Contains(s.Body, "paso 1") {
		t.Fatalf("body = %q", s.Body)
	}

	// sin frontmatter: todo es body
	s2 := parse("solo contenido\nsin frontmatter")
	if s2.Name != "" || !strings.Contains(s2.Body, "sin frontmatter") {
		t.Fatalf("s2 = %+v", s2)
	}
}

func TestLoad(t *testing.T) {
	projDir := t.TempDir()
	global := t.TempDir()
	proj := filepath.Join(projDir, ".arnes", "skills")

	writeSkill(t, global, "review", "---\nname: review\ndescription: revisión de código\n---\nchecklist global\n")
	writeSkill(t, global, "deploy", "---\nname: deploy\ndescription: global deploy\n---\nglobal\n")
	// el proyecto redefine 'deploy' y agrega 'testing'
	writeSkill(t, proj, "deploy", "---\nname: deploy\ndescription: deploy del proyecto\n---\nproyecto\n")
	writeSkill(t, proj, "testing", "---\nname: testing\n---\ncómo testear acá\n")
	// carpeta sin SKILL.md: se ignora
	if err := os.MkdirAll(filepath.Join(global, "vacia"), 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := Dirs(projDir, global)
	if dirs[0] != proj {
		t.Fatalf("Dirs no armó la ruta del proyecto: %v", dirs)
	}
	skills, err := Load(dirs...)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(skills...)

	if reg.Len() != 3 {
		t.Fatalf("skills = %d (%v), quiero 3", reg.Len(), catalogNames(reg))
	}
	d, _ := reg.Get("deploy")
	if !strings.Contains(d.Body, "proyecto") {
		t.Fatalf("el skill del proyecto debería ganarle al global: %q", d.Body)
	}
	tst, ok := reg.Get("testing")
	if !ok || tst.Name != "testing" { // name faltante → nombre de la carpeta
		t.Fatalf("skill 'testing' mal cargado: %+v ok=%v", tst, ok)
	}
}

func TestLoadMissingDirIsOK(t *testing.T) {
	skills, err := Load(filepath.Join(t.TempDir(), "no-existe"))
	if err != nil || len(skills) != 0 {
		t.Fatalf("skills=%v err=%v", skills, err)
	}
}

func TestLoadEmptyBodyIsError(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "vacio", "---\nname: vacio\ndescription: x\n---\n   \n")
	if _, err := Load(dir); err == nil {
		t.Fatal("esperaba error por cuerpo vacío")
	}
}

func TestCatalog(t *testing.T) {
	reg := NewRegistry(
		Skill{Name: "a", Description: "hace a"},
		Skill{Name: "b"},
	)
	c := reg.Catalog()
	if !strings.Contains(c, "- a: hace a") || !strings.Contains(c, "- b: (sin descripción)") {
		t.Fatalf("catálogo = %q", c)
	}
}

func catalogNames(r *Registry) []string {
	var out []string
	for _, s := range r.All() {
		out = append(out, s.Name)
	}
	return out
}
