package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func rootDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("evalsymlinks: %v", err)
	}
	return root
}

func writeSkill(t *testing.T, dir, name, frontmatter, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\n" + frontmatter + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func skillNames(s []LoadedSkill) []string {
	out := make([]string, len(s))
	for i, sk := range s {
		out[i] = sk.Name
	}
	return out
}

func TestLoad_EmptyWhenAllDirsAbsent(t *testing.T) {
	root := rootDir(t)
	got, err := Load(LoadOptions{
		ProjectDir: filepath.Join(root, "no-project"),
		UserDir:    filepath.Join(root, "no-user"),
		BuiltInDir: filepath.Join(root, "no-builtin"),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestLoad_BuiltInOrigin(t *testing.T) {
	root := rootDir(t)
	dir := filepath.Join(root, "builtin")
	writeSkill(t, dir, "foo", "description: foo skill", "# body")
	got, err := Load(LoadOptions{BuiltInDir: dir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].Name != "foo" || got[0].Origin != OriginBuiltIn {
		t.Fatalf("got %+v", got)
	}
}

func TestLoad_UserOrigin(t *testing.T) {
	root := rootDir(t)
	dir := filepath.Join(root, "user")
	writeSkill(t, dir, "foo", "description: foo user skill", "# body")
	got, err := Load(LoadOptions{UserDir: dir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].Origin != OriginUser {
		t.Fatalf("got %+v", got)
	}
}

func TestLoad_ProjectOrigin(t *testing.T) {
	root := rootDir(t)
	dir := filepath.Join(root, "project")
	writeSkill(t, dir, "foo", "description: foo project skill", "# body")
	got, err := Load(LoadOptions{ProjectDir: dir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].Origin != OriginProject {
		t.Fatalf("got %+v", got)
	}
}

func TestLoad_ParsesFrontmatter(t *testing.T) {
	root := rootDir(t)
	dir := filepath.Join(root, "project")
	writeSkill(t, dir, "foo", "description: hello\nextra: bar", "# body")
	got, err := Load(LoadOptions{ProjectDir: dir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got[0].Description != "hello" {
		t.Fatalf("description = %q", got[0].Description)
	}
	if got[0].Frontmatter["description"] != "hello" {
		t.Fatalf("fm.description = %v", got[0].Frontmatter["description"])
	}
	if got[0].Frontmatter["extra"] != "bar" {
		t.Fatalf("fm.extra = %v", got[0].Frontmatter["extra"])
	}
}

func TestLoad_UsesDirectoryNameAsSkillName(t *testing.T) {
	root := rootDir(t)
	dir := filepath.Join(root, "project")
	writeSkill(t, dir, "my-skill", "description: x", "# body")
	got, err := Load(LoadOptions{ProjectDir: dir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got[0].Name != "my-skill" {
		t.Fatalf("name = %q", got[0].Name)
	}
}

func TestLoad_PrecedenceProjectOverUserOverBuiltIn(t *testing.T) {
	root := rootDir(t)
	projectDir := filepath.Join(root, "project")
	userDir := filepath.Join(root, "user")
	builtInDir := filepath.Join(root, "builtin")
	writeSkill(t, projectDir, "shared", "description: project version", "body")
	writeSkill(t, userDir, "shared", "description: user version", "body")
	writeSkill(t, builtInDir, "shared", "description: built-in version", "body")

	got, err := Load(LoadOptions{ProjectDir: projectDir, UserDir: userDir, BuiltInDir: builtInDir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].Origin != OriginProject || got[0].Description != "project version" {
		t.Fatalf("got %+v", got)
	}

	got2, err := Load(LoadOptions{UserDir: userDir, BuiltInDir: builtInDir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got2[0].Origin != OriginUser || got2[0].Description != "user version" {
		t.Fatalf("got %+v", got2)
	}
}

func TestLoad_SkipsSubdirWithoutSkillMd(t *testing.T) {
	root := rootDir(t)
	dir := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeSkill(t, dir, "real", "description: real", "body")
	got, err := Load(LoadOptions{ProjectDir: dir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(skillNames(got), []string{"real"}) {
		t.Fatalf("names = %v", skillNames(got))
	}
}

func TestLoad_SkipsInvalidYAML(t *testing.T) {
	root := rootDir(t)
	dir := filepath.Join(root, "project")
	bad := filepath.Join(dir, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bad, "SKILL.md"),
		[]byte("---\n: : : invalid yaml :::\n---\nbody\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	writeSkill(t, dir, "good", "description: good", "body")
	got, err := Load(LoadOptions{ProjectDir: dir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(skillNames(got), []string{"good"}) {
		t.Fatalf("names = %v", skillNames(got))
	}
}

func TestLoad_SkipsMissingDescription(t *testing.T) {
	root := rootDir(t)
	dir := filepath.Join(root, "project")
	writeSkill(t, dir, "no-desc", "name: no-desc", "body")
	writeSkill(t, dir, "has-desc", "description: has", "body")
	got, err := Load(LoadOptions{ProjectDir: dir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(skillNames(got), []string{"has-desc"}) {
		t.Fatalf("names = %v", skillNames(got))
	}
}

func TestLoad_MissingDirsDontError(t *testing.T) {
	root := rootDir(t)
	got, err := Load(LoadOptions{})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, err %v", got, err)
	}
	got, err = Load(LoadOptions{ProjectDir: filepath.Join(root, "nope")})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, err %v", got, err)
	}
}

func TestLoad_SortsCaseInsensitive(t *testing.T) {
	root := rootDir(t)
	dir := filepath.Join(root, "project")
	writeSkill(t, dir, "banana", "description: b", "body")
	writeSkill(t, dir, "Apple", "description: a", "body")
	writeSkill(t, dir, "cherry", "description: c", "body")
	got, err := Load(LoadOptions{ProjectDir: dir})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(skillNames(got), []string{"Apple", "banana", "cherry"}) {
		t.Fatalf("names = %v", skillNames(got))
	}
}
