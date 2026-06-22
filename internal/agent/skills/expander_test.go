package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeSkill(t *testing.T, root, name, content string) LoadedSkill {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(body, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return LoadedSkill{
		Name:        name,
		Description: "desc",
		Frontmatter: map[string]any{"description": "desc"},
		BodyPath:    body,
		Origin:      OriginProject,
	}
}

func TestExpand_StripsFrontmatter(t *testing.T) {
	root := rootDir(t)
	s := makeSkill(t, root, "foo", "---\ndescription: x\n---\n# Foo body\n\nsome instructions")
	got, err := Expand("foo", []LoadedSkill{s})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.HasPrefix(got, "---") {
		t.Fatalf("frontmatter not stripped: %q", got)
	}
	if !strings.Contains(got, "# Foo body") || !strings.Contains(got, "some instructions") {
		t.Fatalf("body missing: %q", got)
	}
}

func TestExpand_PreservesBodyVerbatim(t *testing.T) {
	root := rootDir(t)
	body := "# Title\n\n- item one\n- item two\n\n```ts\nconst x = 1;\n```"
	s := makeSkill(t, root, "foo", "---\ndescription: x\n---\n"+body+"\n")
	got, err := Expand("foo", []LoadedSkill{s})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != body {
		t.Fatalf("got %q, want %q", got, body)
	}
}

func TestExpand_NoFrontmatterReturnsAll(t *testing.T) {
	root := rootDir(t)
	body := "# Just a body\n\nno frontmatter here"
	s := makeSkill(t, root, "foo", body)
	got, err := Expand("foo", []LoadedSkill{s})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != body {
		t.Fatalf("got %q", got)
	}
}

func TestExpand_ErrorWhenSkillMissing(t *testing.T) {
	root := rootDir(t)
	s := makeSkill(t, root, "foo", "---\ndescription: x\n---\nbody")
	_, err := Expand("missing", []LoadedSkill{s})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v", err)
	}
}
