package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"yoli/internal/ai"
)

// WriteFileTool writes UTF-8 text files inside cwd, creating parent
// directories as needed.
type WriteFileTool struct {
	cwd string
}

// NewWriteFileTool constructs a WriteFileTool rooted at cwd.
func NewWriteFileTool(cwd string) *WriteFileTool { return &WriteFileTool{cwd: cwd} }

// Definition returns the tool schema sent to the model.
func (t *WriteFileTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "Write",
		Description: "Write a UTF-8 text file relative to the working directory. " +
			"Creates parent directories as needed and overwrites existing files.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path relative to the working directory.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "File contents.",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Run unmarshals args, resolves the path inside cwd, creates intermediate
// directories, writes the file, and returns a confirmation string.
func (t *WriteFileTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args writeFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("write_file: invalid arguments: %w", err)
	}
	target, err := ResolveInside(t.cwd, args.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("write_file: mkdir: %w", err)
	}
	if err := os.WriteFile(target, []byte(args.Content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	return fmt.Sprintf("Wrote %s (%d bytes)", args.Path, len(args.Content)), nil
}

var _ Tool = (*WriteFileTool)(nil)
