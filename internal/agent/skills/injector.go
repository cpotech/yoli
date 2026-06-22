package skills

import (
	"sort"
	"strings"
)

// InjectSection renders the "Available Skills" Markdown section that the
// agent appends to its system prompt. Returns "" when skills is empty.
// Skills are sorted alphabetically by name (case-insensitive).
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
	for i, s := range sorted {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(s.Name)
		b.WriteString(": ")
		b.WriteString(s.Description)
	}
	return b.String()
}
