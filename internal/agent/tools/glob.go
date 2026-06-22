package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"yoli/internal/ai"
)

// GlobTool walks the filesystem and returns paths matching a shell-style
// pattern. Supports `**` for segment-spanning matches and `{a,b}` brace
// alternation.
type GlobTool struct {
	cwd string
}

// NewGlobTool constructs a GlobTool rooted at cwd.
func NewGlobTool(cwd string) *GlobTool { return &GlobTool{cwd: cwd} }

// Definition returns the tool schema sent to the model.
func (t *GlobTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "Glob",
		Description: "Find files matching a shell-style pattern. Supports ** for nested matches " +
			"and {a,b} brace alternation (e.g. \"**/*.{ts,tsx}\"). " +
			"Returns repo-relative paths sorted by mtime (newest first). " +
			"Skips .git, node_modules, vendor, .yolium.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Glob pattern, e.g. \"**/*.go\" or \"src/*.ts\".",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional subdirectory to scope the search (default: working directory).",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

type globArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

var defaultSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".yolium":      true,
}

// Run walks the filesystem under (cwd/path) and returns paths matching the
// pattern, newest mtime first, joined by newlines.
func (t *GlobTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args globArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("glob: invalid arguments: %w", err)
	}
	scopeArg := args.Path
	if scopeArg == "" {
		scopeArg = "."
	}
	scopeAbs, err := ResolveInside(t.cwd, scopeArg)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(t.cwd)
	if err != nil {
		return "", fmt.Errorf("glob: resolve cwd: %w", err)
	}
	// Expand {a,b} brace alternation into concrete patterns first; Go's
	// filepath.Match (used per-segment by matchGlob) has no brace support,
	// so "**/*.{ts,tsx}" would otherwise match nothing.
	patSegsList := make([][]string, 0, 2)
	for _, expanded := range expandBraces(args.Pattern) {
		patSegsList = append(patSegsList, splitPattern(expanded))
	}

	type hit struct {
		rel   string
		mtime time.Time
	}
	var hits []hit

	walkErr := filepath.WalkDir(scopeAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != scopeAbs && defaultSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		relToScope, relErr := filepath.Rel(scopeAbs, p)
		if relErr != nil {
			return nil
		}
		nameSegs := splitPath(relToScope)
		matched := false
		for _, patSegs := range patSegsList {
			if matchGlob(patSegs, nameSegs) {
				matched = true
				break
			}
		}
		if !matched {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		relToRoot, relErr := filepath.Rel(rootAbs, p)
		if relErr != nil {
			return nil
		}
		hits = append(hits, hit{rel: filepath.ToSlash(relToRoot), mtime: info.ModTime()})
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("glob: %w", walkErr)
	}
	sort.Slice(hits, func(i, j int) bool {
		if !hits[i].mtime.Equal(hits[j].mtime) {
			return hits[i].mtime.After(hits[j].mtime)
		}
		return hits[i].rel < hits[j].rel
	})
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.rel
	}
	return strings.Join(out, "\n"), nil
}

// expandBraces expands shell-style brace alternation in a glob pattern,
// returning every concrete pattern it denotes. Patterns without a real
// alternation are returned unchanged. Nesting and multiple groups are
// supported:
//
//	"*.{ts,tsx}"   -> ["*.ts", "*.tsx"]
//	"{a,b}/{c,d}"  -> ["a/c", "a/d", "b/c", "b/d"]
//	"x{a,{b,c}}y"  -> ["xay", "xby", "xcy"]
//
// A group with no top-level comma (e.g. "{abc}") or an unbalanced brace is
// treated as literal text, matching common shell behaviour.
func expandBraces(pattern string) []string {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return []string{pattern}
	}
	depth := 0
	closeIdx := -1
	for i := open; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closeIdx = i
			}
		}
		if closeIdx >= 0 {
			break
		}
	}
	if closeIdx < 0 {
		return []string{pattern} // unbalanced — treat literally
	}
	options := splitTopLevel(pattern[open+1 : closeIdx])
	if len(options) < 2 {
		// Not an alternation; keep the braces literal and scan the rest.
		rest := expandBraces(pattern[closeIdx+1:])
		out := make([]string, len(rest))
		for i, r := range rest {
			out[i] = pattern[:closeIdx+1] + r
		}
		return out
	}
	prefix := pattern[:open]
	suffix := pattern[closeIdx+1:]
	var out []string
	for _, opt := range options {
		// Recurse on the recombined string so nested groups in opt and any
		// further groups in suffix are expanded too. The leftmost remaining
		// brace always sits past the (brace-free) prefix, so this terminates.
		out = append(out, expandBraces(prefix+opt+suffix)...)
	}
	return out
}

// splitTopLevel splits s on commas that are not nested inside braces.
func splitTopLevel(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

func splitPattern(p string) []string {
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "./")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func splitPath(p string) []string {
	p = filepath.ToSlash(p)
	if p == "" || p == "." {
		return nil
	}
	return strings.Split(p, "/")
}

func matchGlob(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchGlob(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, err := filepath.Match(pat[0], name[0])
	if err != nil || !ok {
		return false
	}
	return matchGlob(pat[1:], name[1:])
}

var _ Tool = (*GlobTool)(nil)
