package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"yoli/internal/ai"
)

// ReadFileTool reads UTF-8 text files relative to cwd. It may also read
// from extraRoots — explicitly allowlisted read-only directories outside
// the working tree (see YOLI_READ_ALLOW / ResolveReadable).
type ReadFileTool struct {
	cwd        string
	extraRoots []string
}

// NewReadFileTool constructs a ReadFileTool rooted at cwd. Extra read-only
// roots are sourced from the YOLI_READ_ALLOW environment variable.
func NewReadFileTool(cwd string) *ReadFileTool {
	return &ReadFileTool{cwd: cwd, extraRoots: ReadAllowRoots()}
}

// Definition returns the tool schema sent to the model.
func (t *ReadFileTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "Read",
		Description: "Read a UTF-8 text file relative to the working directory. " +
			"Set with_hashes=true to prefix each line with `N:HHH|` (1-based line " +
			"number and a short content hash); pair those values with edit's " +
			"hashline mode to make changes without reproducing the line text.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path relative to the working directory.",
				},
				"with_hashes": map[string]any{
					"type":        "boolean",
					"description": "When true, return each line prefixed with \"N:HHH|\" where N is the 1-based line number and HHH is a short content hash for use with edit's hashline mode.",
				},
			},
			"required": []string{"path"},
		},
	}
}

type readFileArgs struct {
	Path       string `json:"path"`
	WithHashes bool   `json:"with_hashes"`
}

// Run unmarshals args, resolves the path inside cwd, and returns the
// file's contents (optionally hashline-annotated).
func (t *ReadFileTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args readFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("read_file: invalid arguments: %w", err)
	}
	target, err := ResolveReadable(t.cwd, t.extraRoots, args.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("Failed to read %s: %v", args.Path, err)
	}
	if args.WithHashes {
		return AnnotateHashlines(string(data)), nil
	}
	return string(data), nil
}

var _ Tool = (*ReadFileTool)(nil)
