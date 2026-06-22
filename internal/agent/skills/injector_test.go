package skills

import (
	"reflect"
	"strings"
	"testing"
)

func mkSkill(name, description string) LoadedSkill {
	return LoadedSkill{
		Name:        name,
		Description: description,
		Frontmatter: map[string]any{"description": description},
		BodyPath:    "/tmp/" + name + "/SKILL.md",
		Origin:      OriginProject,
	}
}

func bulletLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "- ") {
			out = append(out, l)
		}
	}
	return out
}

func TestInjectSection_EmptyOnNoSkills(t *testing.T) {
	if got := InjectSection(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestInjectSection_HeaderFollowedByBlankLine(t *testing.T) {
	got := InjectSection([]LoadedSkill{mkSkill("foo", "a foo")})
	if !strings.HasPrefix(got, "## Available Skills\n\n") {
		t.Fatalf("missing header: %q", got)
	}
}

func TestInjectSection_BulletFormat(t *testing.T) {
	got := InjectSection([]LoadedSkill{mkSkill("foo", "a foo"), mkSkill("bar", "a bar")})
	if !strings.Contains(got, "- foo: a foo") || !strings.Contains(got, "- bar: a bar") {
		t.Fatalf("bullets missing: %q", got)
	}
}

func TestInjectSection_SortsCaseInsensitive(t *testing.T) {
	got := InjectSection([]LoadedSkill{
		mkSkill("banana", "b"),
		mkSkill("Apple", "a"),
		mkSkill("cherry", "c"),
	})
	want := []string{"- Apple: a", "- banana: b", "- cherry: c"}
	if !reflect.DeepEqual(bulletLines(got), want) {
		t.Fatalf("bullets = %v", bulletLines(got))
	}
}

func TestInjectSection_StableOnCaseTies(t *testing.T) {
	got := InjectSection([]LoadedSkill{mkSkill("foo", "lower"), mkSkill("Foo", "upper")})
	lines := bulletLines(got)
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	hasLower := false
	hasUpper := false
	for _, l := range lines {
		if l == "- foo: lower" {
			hasLower = true
		}
		if l == "- Foo: upper" {
			hasUpper = true
		}
	}
	if !hasLower || !hasUpper {
		t.Fatalf("missing variants: %v", lines)
	}
}
