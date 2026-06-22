package skills

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadOptions configures Load. Any directory left empty or missing on
// disk is silently skipped.
type LoadOptions struct {
	ProjectDir string
	UserDir    string
	BuiltInDir string
}

var frontmatterRE = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?`)

// Load scans the three configured directories in precedence order
// (project > user > built-in) and returns the deduplicated, sorted set
// of skills. Skills whose SKILL.md is missing, has no frontmatter, has
// invalid YAML, or lacks a string `description` field are skipped.
func Load(opts LoadOptions) ([]LoadedSkill, error) {
	sources := []struct {
		dir    string
		origin Origin
	}{
		{opts.ProjectDir, OriginProject},
		{opts.UserDir, OriginUser},
		{opts.BuiltInDir, OriginBuiltIn},
	}

	byName := make(map[string]LoadedSkill)
	for _, src := range sources {
		if src.dir == "" {
			continue
		}
		found, err := scanDir(src.dir, src.origin)
		if err != nil {
			return nil, err
		}
		for _, s := range found {
			if _, ok := byName[s.Name]; !ok {
				byName[s.Name] = s
			}
		}
	}

	out := make([]LoadedSkill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func scanDir(dir string, origin Origin) ([]LoadedSkill, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []LoadedSkill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		bodyPath := filepath.Join(dir, name, "SKILL.md")
		raw, err := os.ReadFile(bodyPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		match := frontmatterRE.FindSubmatch(raw)
		if match == nil {
			continue
		}
		var parsed any
		if err := yaml.Unmarshal(match[1], &parsed); err != nil {
			continue
		}
		fm, ok := parsed.(map[string]any)
		if !ok {
			continue
		}
		desc, ok := fm["description"].(string)
		if !ok {
			continue
		}
		out = append(out, LoadedSkill{
			Name:        name,
			Description: desc,
			Frontmatter: fm,
			BodyPath:    bodyPath,
			Origin:      origin,
		})
	}
	return out, nil
}
