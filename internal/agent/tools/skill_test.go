package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoli/internal/agent/skills"
)

// loadTestSkills writes one SKILL.md per (name, body) pair under a
// tempdir and loads them through the real loader so BodyPath is valid.
func loadTestSkills(t *testing.T, defs map[string]string) []skills.LoadedSkill {
	t.Helper()
	dir := t.TempDir()
	for name, body := range defs {
		skillDir := filepath.Join(dir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		content := "---\ndescription: " + name + " skill\n---\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	loaded, err := skills.Load(skills.LoadOptions{ProjectDir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return loaded
}

func TestSkillTool_DefinitionNameAndRequiredParam(t *testing.T) {
	def := NewSkillTool(nil).Definition()
	if def.Name != "Skill" {
		t.Fatalf("name = %q", def.Name)
	}
	required, ok := def.Parameters["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "name" {
		t.Fatalf("required = %v", def.Parameters["required"])
	}
}

func TestSkillTool_RunReturnsBodyWithoutFrontmatter(t *testing.T) {
	tool := NewSkillTool(loadTestSkills(t, map[string]string{"foo": "# Foo Body"}))
	got, err := tool.Run(context.Background(), json.RawMessage(`{"name":"foo"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "# Foo Body" {
		t.Fatalf("body = %q", got)
	}
}

func TestSkillTool_UnknownSkillErrorListsAvailable(t *testing.T) {
	tool := NewSkillTool(loadTestSkills(t, map[string]string{"foo": "a", "bar": "b"}))
	_, err := tool.Run(context.Background(), json.RawMessage(`{"name":"nope"}`))
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
	msg := err.Error()
	if !strings.Contains(msg, "nope") || !strings.Contains(msg, "foo") || !strings.Contains(msg, "bar") {
		t.Fatalf("error should name the miss and list available skills: %q", msg)
	}
}

func TestSkillTool_InvalidArgs(t *testing.T) {
	tool := NewSkillTool(nil)
	if _, err := tool.Run(context.Background(), json.RawMessage(`not json`)); err == nil {
		t.Fatal("expected error for invalid arguments")
	}
}
