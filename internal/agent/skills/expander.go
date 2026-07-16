package skills

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Expand reads the body of the named skill (from its embedded filesystem
// for built-ins, from disk otherwise), strips any leading YAML
// frontmatter, and returns the trimmed Markdown.
func Expand(name string, skills []LoadedSkill) (string, error) {
	var found *LoadedSkill
	for i := range skills {
		if skills[i].Name == name {
			found = &skills[i]
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("Skill not found: %s", name)
	}
	var raw []byte
	var err error
	if found.fsys != nil {
		raw, err = fs.ReadFile(found.fsys, found.BodyPath)
	} else {
		raw, err = os.ReadFile(found.BodyPath)
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", found.BodyPath, err)
	}
	body := frontmatterRE.ReplaceAllString(string(raw), "")
	return strings.TrimSpace(body), nil
}
