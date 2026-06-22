// Package skills loads, lists, and expands user-authored "skill"
// directories (each containing a SKILL.md with YAML frontmatter) so the
// agent can advertise and lazily fetch their bodies on demand.
package skills

// Origin is where a skill was loaded from: a project-local directory, a
// user-level directory, or the binary's built-in bundle.
type Origin string

const (
	OriginProject Origin = "project"
	OriginUser    Origin = "user"
	OriginBuiltIn Origin = "built-in"
)

// LoadedSkill is a parsed SKILL.md descriptor. The body is not read at
// load time; callers retrieve it via expander.Expand.
type LoadedSkill struct {
	Name        string
	Description string
	Frontmatter map[string]any
	BodyPath    string
	Origin      Origin
}
