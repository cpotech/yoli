package skills

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadOptions configures Load. Any directory left empty or missing on
// disk is silently skipped; a nil BuiltIn means no built-in skills.
type LoadOptions struct {
	ProjectDir string
	UserDir    string
	// BuiltIn is the built-in skill bundle, typically the embedded
	// filesystem compiled into the binary (yoli/skills.FS). Each skill
	// lives at <name>/SKILL.md relative to its root.
	BuiltIn fs.FS
}

var frontmatterRE = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n?`)

// Load scans the configured sources in precedence order (project > user
// > built-in) and returns the deduplicated, sorted set of skills. Skills
// whose SKILL.md is missing, has no frontmatter, has invalid YAML, or
// lacks a string `description` field are skipped.
func Load(opts LoadOptions) ([]LoadedSkill, error) {
	byName := make(map[string]LoadedSkill)
	add := func(found []LoadedSkill) {
		for _, s := range found {
			if _, ok := byName[s.Name]; !ok {
				byName[s.Name] = s
			}
		}
	}

	dirSources := []struct {
		dir    string
		origin Origin
	}{
		{opts.ProjectDir, OriginProject},
		{opts.UserDir, OriginUser},
	}
	for _, src := range dirSources {
		if src.dir == "" {
			continue
		}
		found, err := scanDir(src.dir, src.origin)
		if err != nil {
			return nil, err
		}
		add(found)
	}
	if opts.BuiltIn != nil {
		found, err := scanFS(opts.BuiltIn, OriginBuiltIn)
		if err != nil {
			return nil, err
		}
		add(found)
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

// parseFrontmatter extracts the YAML frontmatter map from a SKILL.md.
// Returns ok=false when the frontmatter is missing, invalid, or lacks a
// string description — the skill is then skipped.
func parseFrontmatter(raw []byte) (fm map[string]any, description string, ok bool) {
	match := frontmatterRE.FindSubmatch(raw)
	if match == nil {
		return nil, "", false
	}
	var parsed any
	if err := yaml.Unmarshal(match[1], &parsed); err != nil {
		return nil, "", false
	}
	fm, isMap := parsed.(map[string]any)
	if !isMap {
		return nil, "", false
	}
	description, isStr := fm["description"].(string)
	if !isStr {
		return nil, "", false
	}
	return fm, description, true
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
		fm, desc, ok := parseFrontmatter(raw)
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

// scanFS is scanDir over an fs.FS (used for the embedded built-in
// bundle). BodyPath is recorded relative to the filesystem root and the
// filesystem itself is retained on the skill for later expansion.
func scanFS(fsys fs.FS, origin Origin) ([]LoadedSkill, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []LoadedSkill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		bodyPath := path.Join(name, "SKILL.md")
		raw, err := fs.ReadFile(fsys, bodyPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		fm, desc, ok := parseFrontmatter(raw)
		if !ok {
			continue
		}
		out = append(out, LoadedSkill{
			Name:        name,
			Description: desc,
			Frontmatter: fm,
			BodyPath:    bodyPath,
			Origin:      origin,
			fsys:        fsys,
		})
	}
	return out, nil
}
