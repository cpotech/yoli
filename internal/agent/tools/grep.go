package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"yoli/internal/ai"
)

// GrepTool searches files using Go's regexp engine.
type GrepTool struct {
	cwd string
}

// NewGrepTool constructs a GrepTool rooted at cwd.
func NewGrepTool(cwd string) *GrepTool { return &GrepTool{cwd: cwd} }

// Definition returns the tool schema sent to the model.
func (t *GrepTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        "Grep",
		Description: "Search file contents with a Go regexp. Output modes: files_with_matches (default), content, count.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Regular expression (Go RE2 syntax).",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional subdirectory to scope the search.",
				},
				"glob": map[string]any{
					"type":        "string",
					"description": "Optional file glob filter (e.g. \"*.go\" or \"**/*.ts\").",
				},
				"type": map[string]any{
					"type":        "string",
					"description": "Optional file-type filter: go, md, json, yaml, yml, sh, ts, tsx, js, py.",
				},
				"output_mode": map[string]any{
					"type":        "string",
					"description": "files_with_matches | content | count. Default files_with_matches.",
				},
				"-i": map[string]any{
					"type":        "boolean",
					"description": "Case-insensitive match.",
				},
				"-n": map[string]any{
					"type":        "boolean",
					"description": "Include line numbers (content mode).",
				},
				"head_limit": map[string]any{
					"type":        "integer",
					"description": "Cap the number of output lines (default 250, 0 = unlimited).",
				},
				"multiline": map[string]any{
					"type":        "boolean",
					"description": "Enable (?s) — dot matches newlines and matches span lines.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

type grepArgs struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	Glob       string `json:"glob"`
	Type       string `json:"type"`
	OutputMode string `json:"output_mode"`
	I          bool   `json:"-i"`
	N          bool   `json:"-n"`
	HeadLimit  *int   `json:"head_limit"`
	Multiline  bool   `json:"multiline"`
}

var typeExts = map[string][]string{
	"go":   {".go"},
	"md":   {".md", ".markdown"},
	"json": {".json"},
	"yaml": {".yaml", ".yml"},
	"yml":  {".yaml", ".yml"},
	"sh":   {".sh", ".bash"},
	"ts":   {".ts"},
	"tsx":  {".tsx"},
	"js":   {".js", ".mjs", ".cjs"},
	"py":   {".py"},
}

// Run compiles the pattern, walks the scope, and emits results per
// output_mode.
func (t *GrepTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args grepArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("grep: invalid arguments: %w", err)
	}
	mode := args.OutputMode
	if mode == "" {
		mode = "files_with_matches"
	}
	switch mode {
	case "files_with_matches", "content", "count":
	default:
		return "", fmt.Errorf("grep: invalid output_mode %q", mode)
	}

	expr := args.Pattern
	var prefix string
	if args.I {
		prefix += "(?i)"
	}
	if args.Multiline {
		prefix += "(?s)"
	}
	re, err := regexp.Compile(prefix + expr)
	if err != nil {
		return "", fmt.Errorf("grep: %v", err)
	}

	var allowedExts map[string]bool
	if args.Type != "" {
		exts, ok := typeExts[args.Type]
		if !ok {
			return "", fmt.Errorf("grep: unknown type %q", args.Type)
		}
		allowedExts = map[string]bool{}
		for _, e := range exts {
			allowedExts[e] = true
		}
	}

	var globSegs []string
	if args.Glob != "" {
		globSegs = splitPattern(args.Glob)
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
		return "", fmt.Errorf("grep: resolve cwd: %w", err)
	}

	limit := 250
	if args.HeadLimit != nil {
		limit = *args.HeadLimit
	}

	type fileResult struct {
		rel     string
		count   int
		content []string
	}
	var results []fileResult

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
		relToRoot, relErr := filepath.Rel(rootAbs, p)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(relToRoot)
		base := filepath.Base(relSlash)

		if allowedExts != nil {
			if !allowedExts[strings.ToLower(filepath.Ext(base))] {
				return nil
			}
		}
		if globSegs != nil {
			var nameSegs []string
			if strings.Contains(args.Glob, "/") {
				nameSegs = splitPath(relSlash)
			} else {
				nameSegs = []string{base}
			}
			if !matchGlob(globSegs, nameSegs) {
				return nil
			}
		}

		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		if isBinary(data) {
			return nil
		}

		res := fileResult{rel: relSlash}
		if args.Multiline {
			matches := re.FindAllIndex(data, -1)
			if len(matches) == 0 {
				return nil
			}
			res.count = len(matches)
			if mode == "content" {
				for _, m := range matches {
					line := string(data[m[0]:m[1]])
					if args.N {
						lineNum := 1 + bytes.Count(data[:m[0]], []byte{'\n'})
						res.content = append(res.content, fmt.Sprintf("%s:%d:%s", relSlash, lineNum, line))
					} else {
						res.content = append(res.content, fmt.Sprintf("%s:%s", relSlash, line))
					}
				}
			}
		} else {
			lines := strings.Split(string(data), "\n")
			for i, line := range lines {
				hits := re.FindAllStringIndex(line, -1)
				if len(hits) == 0 {
					continue
				}
				res.count += len(hits)
				if mode == "content" {
					if args.N {
						res.content = append(res.content, fmt.Sprintf("%s:%d:%s", relSlash, i+1, line))
					} else {
						res.content = append(res.content, fmt.Sprintf("%s:%s", relSlash, line))
					}
				}
			}
			if res.count == 0 {
				return nil
			}
		}

		results = append(results, res)
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("grep: %w", walkErr)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].rel < results[j].rel })

	var lines []string
	switch mode {
	case "files_with_matches":
		for _, r := range results {
			lines = append(lines, r.rel)
		}
	case "count":
		for _, r := range results {
			lines = append(lines, fmt.Sprintf("%s:%d", r.rel, r.count))
		}
	case "content":
		for _, r := range results {
			lines = append(lines, r.content...)
		}
	}

	if limit > 0 && len(lines) > limit {
		lines = lines[:limit]
	}
	return strings.Join(lines, "\n"), nil
}

func isBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

var _ Tool = (*GrepTool)(nil)
