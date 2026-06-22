package cli

import (
	"strings"
	"testing"

	"yoli/internal/agent/skills"
)

func TestResolveSkillDirs_ProjectDirIsCwdYoliSkills(t *testing.T) {
	d := ResolveSkillDirs("/tmp/project", "/home/user", "/opt/yoli/bin/yoli")
	if d.ProjectDir != "/tmp/project/.yoli/skills" {
		t.Fatalf("projectDir = %q", d.ProjectDir)
	}
}

func TestResolveSkillDirs_UserDirFromHome(t *testing.T) {
	d := ResolveSkillDirs("/tmp/project", "/home/user", "/opt/yoli/bin/yoli")
	if d.UserDir != "/home/user/.yoli/skills" {
		t.Fatalf("userDir = %q", d.UserDir)
	}
}

func TestResolveSkillDirs_UserDirEmptyWhenNoHome(t *testing.T) {
	d := ResolveSkillDirs("/tmp/project", "", "/opt/yoli/bin/yoli")
	if d.UserDir != "" {
		t.Fatalf("userDir = %q", d.UserDir)
	}
}

func TestResolveSkillDirs_BuiltInDirRelativeToCLIEntry(t *testing.T) {
	d := ResolveSkillDirs("/tmp/project", "/home/user", "/opt/yoli/bin/yoli")
	if d.BuiltInDir != "/opt/yoli/skills" {
		t.Fatalf("builtInDir = %q", d.BuiltInDir)
	}
}

func makeSkill(name, desc string, origin skills.Origin) skills.LoadedSkill {
	return skills.LoadedSkill{
		Name:        name,
		Description: desc,
		Origin:      origin,
		BodyPath:    "/skills/" + name + "/SKILL.md",
	}
}

func TestFormatSkillsList_EmptyState(t *testing.T) {
	got := FormatSkillsList(nil)
	if !strings.Contains(got, "No skills") {
		t.Fatalf("got %q", got)
	}
	if strings.Count(got, "\n") != 0 {
		t.Fatalf("multiline empty state: %q", got)
	}
}

func TestFormatSkillsList_OneLinePerSkill(t *testing.T) {
	got := FormatSkillsList([]skills.LoadedSkill{
		makeSkill("foo", "Foo skill", skills.OriginProject),
		makeSkill("bar", "Bar skill", skills.OriginUser),
	})
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	if !strings.Contains(lines[0], "foo") || !strings.Contains(lines[0], "project") ||
		!strings.Contains(lines[0], "Foo skill") {
		t.Fatalf("line0 = %q", lines[0])
	}
	if !strings.Contains(lines[1], "bar") || !strings.Contains(lines[1], "user") ||
		!strings.Contains(lines[1], "Bar skill") {
		t.Fatalf("line1 = %q", lines[1])
	}
}

func TestFormatSkillsList_PreservesInputOrder(t *testing.T) {
	got := FormatSkillsList([]skills.LoadedSkill{
		makeSkill("zeta", "Z", skills.OriginProject),
		makeSkill("alpha", "A", skills.OriginProject),
		makeSkill("mu", "M", skills.OriginProject),
	})
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], "zeta") ||
		!strings.Contains(lines[1], "alpha") ||
		!strings.Contains(lines[2], "mu") {
		t.Fatalf("order broken: %v", lines)
	}
}

func TestFormatSkillsList_AllOriginLabelsAppear(t *testing.T) {
	got := FormatSkillsList([]skills.LoadedSkill{
		makeSkill("a", "A", skills.OriginProject),
		makeSkill("b", "B", skills.OriginUser),
		makeSkill("c", "C", skills.OriginBuiltIn),
	})
	for _, want := range []string{"project", "user", "built-in"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}
