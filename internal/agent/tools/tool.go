// Package tools defines the Tool interface that the agent loop dispatches
// to, along with the built-in tools the model can call.
package tools

import (
	"context"
	"encoding/json"

	"yoli/internal/ai"
)

// Tool is something the model can call by name. Implementations are
// expected to be safe for concurrent use after construction.
type Tool interface {
	// Definition is the JSON-Schema-shaped description forwarded to the
	// model in the chat request.
	Definition() ai.ToolDefinition

	// Run executes the tool with the model-supplied arguments (raw JSON
	// from the wire) and returns the result that becomes the next
	// `role: "tool"` message.
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

// DefaultTools returns the standard tool set rooted at cwd: read_file,
// write_file, list_dir, run_bash, edit, glob, grep, web_search. Git
// operations are performed via run_bash; the host orchestrator owns
// branch, merge, push, and PR workflows.
func DefaultTools(cwd string) []Tool {
	return []Tool{
		NewReadFileTool(cwd),
		NewWriteFileTool(cwd),
		NewListDirTool(cwd),
		NewRunBashTool(cwd),
		NewEditTool(cwd),
		NewGlobTool(cwd),
		NewGrepTool(cwd),
		NewWebSearchTool(),
	}
}
