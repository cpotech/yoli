package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"yoli/internal/ai"
)

// ListDirTool lists the immediate entries of a directory relative to cwd.
// Directories are suffixed with "/". Output is sorted lexicographically.
type ListDirTool struct {
	cwd string
}

// NewListDirTool constructs a ListDirTool rooted at cwd.
func NewListDirTool(cwd string) *ListDirTool { return &ListDirTool{cwd: cwd} }

// Definition returns the tool schema sent to the model.
func (t *ListDirTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "LS",
		Description: "List the immediate entries of a directory relative to the working directory. " +
			"Directories are suffixed with /.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": `Directory path relative to the working directory. Defaults to ".".`,
				},
			},
		},
	}
}

type listDirArgs struct {
	Path string `json:"path"`
}

// Run unmarshals args (path optional, defaults to "."), resolves the path
// inside cwd, lists entries, and returns them joined by newlines.
func (t *ListDirTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args listDirArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("list_dir: invalid arguments: %w", err)
		}
	}
	if args.Path == "" {
		args.Path = "."
	}
	target, err := ResolveInside(t.cwd, args.Path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return "", fmt.Errorf("list_dir: %w", err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		if e.IsDir() {
			names[i] = e.Name() + "/"
		} else {
			names[i] = e.Name()
		}
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}

var _ Tool = (*ListDirTool)(nil)
