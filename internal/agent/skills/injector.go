package skills

import (
	"sort"
	"strings"
)

// InjectSection renders the "Available Skills" Markdown section that the
// agent appends to its system prompt. Returns "" when skills is empty.
// Skills are sorted alphabetically by name (case-insensitive). Bodies are
// not inlined; the agent fetches them on demand via the Skill tool.
func InjectSection(skills []LoadedSkill) string {
	if len(skills) == 0 {
		return ""
	}
	sorted := make([]LoadedSkill, len(skills))
	copy(sorted, skills)
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
	})
	var b strings.Builder
	b.WriteString("## Available Skills\n\n")
	b.WriteString("Skills are task-specific instruction sets loaded on demand. " +
		"When the task matches a skill's trigger, call the `Skill` tool with " +
		"the skill's name and follow the returned instructions before proceeding.\n\n")
	for i, s := range sorted {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(s.Name)
		b.WriteString(": ")
		b.WriteString(s.Description)
		if t := s.Trigger(); t != "" {
			b.WriteString(" (trigger: ")
			b.WriteString(t)
			b.WriteString(")")
		}
	}
	return b.String()
}
