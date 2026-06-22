package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"yoli/internal/ai"
)

// SubAgentOptions configures SubAgentTool.
type SubAgentOptions struct {
	// CLIEntry is the absolute path to a binary that, when invoked as
	// `<CLIEntry> run --role <role>` with the prompt on stdin, behaves as
	// a Yoli sub-agent.
	CLIEntry string
	// DefaultModel, if non-empty, is exported as OPENROUTER_MODEL in the
	// child's environment.
	DefaultModel string
	// MaxDepth caps recursion. Zero means the default (3).
	MaxDepth int
}

// SubAgentTool spawns another Yoli CLI process to handle a focused
// sub-task. It guards against infinite recursion via YOLI_SUBAGENT_DEPTH.
type SubAgentTool struct {
	opts SubAgentOptions
}

// NewSubAgentTool constructs a SubAgentTool with the given options.
func NewSubAgentTool(opts SubAgentOptions) *SubAgentTool {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 3
	}
	return &SubAgentTool{opts: opts}
}

// Definition returns the tool schema sent to the model.
func (t *SubAgentTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "Agent",
		Description: "Spawn a sub-agent in a separate OS subprocess running with the given role. " +
			"Use this to delegate a focused sub-task (e.g. planning or review) to another agent " +
			"with its own isolated context.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"role": map[string]any{
					"type":        "string",
					"description": "Role of the sub-agent (e.g. coder, planner, reviewer).",
				},
				"prompt": map[string]any{
					"type":        "string",
					"description": "Prompt sent to the sub-agent as its user input on stdin.",
				},
			},
			"required": []string{"role", "prompt"},
		},
	}
}

type subAgentArgs struct {
	Role   string `json:"role"`
	Prompt string `json:"prompt"`
}

// Run spawns the configured CLIEntry with `run --role <role>`, feeds the
// prompt on stdin, and returns the child's stdout. Non-zero exits surface
// as Go errors that embed the child's stderr.
func (t *SubAgentTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args subAgentArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("sub_agent: invalid arguments: %w", err)
	}

	depth, _ := strconv.Atoi(os.Getenv("YOLI_SUBAGENT_DEPTH"))
	if depth < 0 {
		depth = 0
	}
	if depth >= t.opts.MaxDepth {
		return "", fmt.Errorf(
			"sub_agent recursion limit reached: YOLI_SUBAGENT_DEPTH=%d >= maxDepth=%d",
			depth, t.opts.MaxDepth,
		)
	}

	cmd := exec.CommandContext(ctx, t.opts.CLIEntry, "run", "--role", args.Role)
	cmd.Env = childEnv(os.Environ(), depth+1, t.opts.DefaultModel)
	cmd.Stdin = strings.NewReader(args.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("sub_agent failed to spawn %s: %w", t.opts.CLIEntry, err)
	}
	if err := cmd.Wait(); err != nil {
		exit := "null"
		if ee, ok := err.(*exec.ExitError); ok {
			exit = strconv.Itoa(ee.ExitCode())
		}
		return "", fmt.Errorf(
			"sub_agent (%s) exited with code %s: %s",
			t.opts.CLIEntry, exit, stderr.String(),
		)
	}
	return stdout.String(), nil
}

// childEnv returns a copy of parent with YOLI_SUBAGENT_DEPTH set to depth
// and (if non-empty) OPENROUTER_MODEL set to model. Existing entries with
// these keys are overridden.
func childEnv(parent []string, depth int, model string) []string {
	out := make([]string, 0, len(parent)+2)
	skip := map[string]bool{"YOLI_SUBAGENT_DEPTH": true}
	if model != "" {
		skip["OPENROUTER_MODEL"] = true
	}
	for _, kv := range parent {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if skip[kv[:eq]] {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "YOLI_SUBAGENT_DEPTH="+strconv.Itoa(depth))
	if model != "" {
		out = append(out, "OPENROUTER_MODEL="+model)
	}
	return out
}

var _ Tool = (*SubAgentTool)(nil)
