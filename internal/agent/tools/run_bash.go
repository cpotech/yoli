package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"yoli/internal/ai"
)

// RunBashTool executes a bash command inside cwd and returns the merged
// stdout/stderr followed by the exit code.
type RunBashTool struct {
	cwd string
}

// NewRunBashTool constructs a RunBashTool rooted at cwd.
func NewRunBashTool(cwd string) *RunBashTool { return &RunBashTool{cwd: cwd} }

// Definition returns the tool schema sent to the model.
func (t *RunBashTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "Bash",
		Description: "Run a bash command inside the working directory. " +
			"Returns merged stdout/stderr followed by the exit code.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Bash command line to execute.",
				},
			},
			"required": []string{"command"},
		},
	}
}

type runBashArgs struct {
	Command string `json:"command"`
}

// Run executes args.Command via `bash -lc`, capturing merged stdout/stderr.
// Non-zero exit codes are reported in the output string rather than as Go
// errors. Spawn failures (bash not found, etc.) surface as errors.
func (t *RunBashTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args runBashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("run_bash: invalid arguments: %w", err)
	}
	if err := CheckBashCommandPolicy(args.Command); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "bash", "-lc", args.Command)
	cmd.Dir = t.cwd
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	exit := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exit = exitErr.ExitCode()
		} else {
			return "", fmt.Errorf("run_bash: %w", err)
		}
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return fmt.Sprintf("%sexit code: %d", out, exit), nil
}

var _ Tool = (*RunBashTool)(nil)
