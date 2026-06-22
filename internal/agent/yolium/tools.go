package yolium

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"yoli/internal/agent/tools"
	"yoli/internal/ai"
)

// Tool names. Keep these in sync with the JSON `type` discriminators in
// src/agents/_protocol.md on the Yolium side — these are the same
// protocol, just routed through tool calls instead of stdout text under
// --yolium-mode.
const (
	ToolComplete          = "yolium_complete"
	ToolError             = "yolium_error"
	ToolAskQuestion       = "yolium_ask_question"
	ToolProgress          = "yolium_progress"
	ToolAddComment        = "yolium_add_comment"
	ToolComment           = "yolium_comment" // alias of yolium_add_comment
	ToolCreateItem        = "yolium_create_item"
	ToolUpdateDescription = "yolium_update_description"
	ToolSetTestSpecs      = "yolium_set_test_specs"
	ToolAction            = "yolium_action"
	ToolStartAgent        = "yolium_start_agent"
)

// yoliumTool is the shared base for all yolium_* tools: it owns the
// EventSink (where the tool emits its event) and, for terminators, the
// ExitSignal that the agent loop checks via Stop().
type yoliumTool struct {
	name        string
	description string
	parameters  map[string]any
	// run is invoked by Run() after JSON arg validation. It must return
	// the human-readable tool result (which becomes the next tool
	// message) or an error.
	run func(ctx context.Context, raw json.RawMessage) (string, error)
}

func (t *yoliumTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        t.name,
		Description: t.description,
		Parameters:  t.parameters,
	}
}

func (t *yoliumTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	return t.run(ctx, raw)
}

var _ tools.Tool = (*yoliumTool)(nil)

// NewTools returns the yolium_* tool set bound to sink and exit. The
// caller (CLI) registers these alongside the regular tool set when
// --yolium-mode is enabled. Standalone yoli runs do NOT register these.
func NewTools(sink EventSink, exit *ExitSignal) []tools.Tool {
	if sink == nil {
		sink = NopSink()
	}
	if exit == nil {
		exit = NewExitSignal()
	}
	return []tools.Tool{
		newCompleteTool(sink, exit),
		newErrorTool(sink, exit),
		newAskQuestionTool(sink, exit),
		newProgressTool(sink),
		newAddCommentTool(sink, ToolAddComment),
		newAddCommentTool(sink, ToolComment),
		newCreateItemTool(sink),
		newUpdateDescriptionTool(sink),
		newSetTestSpecsTool(sink),
		newActionTool(sink),
		newStartAgentTool(sink),
	}
}

// ---------- Terminator tools ----------

func newCompleteTool(sink EventSink, exit *ExitSignal) *yoliumTool {
	return &yoliumTool{
		name: ToolComplete,
		description: "Signal that the current work item is fully done. " +
			"Calling this tool emits a `complete` event and stops the agent loop. " +
			"Provide a one-line summary of what was accomplished. " +
			"verify-agent only: pass `verdict` (approved|rejected|needs_revision).",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]any{
					"type":        "string",
					"description": "Concise one-line summary of completed work.",
				},
				"verdict": map[string]any{
					"type":        "string",
					"description": "Verify-agent only: approved | rejected | needs_revision.",
					"enum":        []string{"approved", "rejected", "needs_revision"},
				},
			},
			"required": []string{"summary"},
		},
		run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Summary string `json:"summary"`
				Verdict string `json:"verdict"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("%s: invalid arguments: %w", ToolComplete, err)
			}
			if args.Summary == "" {
				return "", errors.New(ToolComplete + ": summary is required")
			}
			evt := CompleteEvent{Summary: args.Summary, Verdict: args.Verdict}
			if err := sink.Emit(evt); err != nil {
				return "", fmt.Errorf("%s: emit: %w", ToolComplete, err)
			}
			exit.Pending = &ExitPending{Kind: ExitPendingComplete, Summary: args.Summary}
			return "Complete event emitted; agent loop will exit.", nil
		},
	}
}

func newErrorTool(sink EventSink, exit *ExitSignal) *yoliumTool {
	return &yoliumTool{
		name: ToolError,
		description: "Signal an unrecoverable failure. Calling this tool emits an `error` " +
			"event and stops the agent loop. Provide a clear message describing what went wrong.",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{
					"type":        "string",
					"description": "Human-readable failure description.",
				},
			},
			"required": []string{"message"},
		},
		run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("%s: invalid arguments: %w", ToolError, err)
			}
			if args.Message == "" {
				return "", errors.New(ToolError + ": message is required")
			}
			if err := sink.Emit(ErrorEvent{Message: args.Message}); err != nil {
				return "", fmt.Errorf("%s: emit: %w", ToolError, err)
			}
			exit.Pending = &ExitPending{Kind: ExitPendingError, Message: args.Message}
			return "Error event emitted; agent loop will exit.", nil
		},
	}
}

func newAskQuestionTool(sink EventSink, exit *ExitSignal) *yoliumTool {
	return &yoliumTool{
		name: ToolAskQuestion,
		description: "Pause the agent and ask the user a question. Calling this tool emits " +
			"an `ask_question` event and stops the agent loop; Yolium surfaces the question " +
			"to the user and later resumes the agent with the answer.",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "The question to ask the user.",
				},
				"options": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional list of multiple-choice options.",
				},
			},
			"required": []string{"text"},
		},
		run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Text    string   `json:"text"`
				Options []string `json:"options"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("%s: invalid arguments: %w", ToolAskQuestion, err)
			}
			if args.Text == "" {
				return "", errors.New(ToolAskQuestion + ": text is required")
			}
			if err := sink.Emit(AskQuestionEvent{Text: args.Text, Options: args.Options}); err != nil {
				return "", fmt.Errorf("%s: emit: %w", ToolAskQuestion, err)
			}
			exit.Pending = &ExitPending{Kind: ExitPendingQuestion}
			return "Question emitted; agent loop will pause for user answer.", nil
		},
	}
}

// ---------- Non-terminator tools ----------

func newProgressTool(sink EventSink) *yoliumTool {
	return &yoliumTool{
		name: ToolProgress,
		description: "Report incremental progress on a step without pausing. " +
			"Use `step` for a short stable label (e.g., \"clarify\", \"implement\", \"model\") and " +
			"`detail` for a one-line specific update. attempt/maxAttempts are optional retry counters.",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"step":        map[string]any{"type": "string"},
				"detail":      map[string]any{"type": "string"},
				"attempt":     map[string]any{"type": "integer"},
				"maxAttempts": map[string]any{"type": "integer"},
			},
			"required": []string{"step", "detail"},
		},
		run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Step        string `json:"step"`
				Detail      string `json:"detail"`
				Attempt     *int   `json:"attempt"`
				MaxAttempts *int   `json:"maxAttempts"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("%s: invalid arguments: %w", ToolProgress, err)
			}
			if args.Step == "" || args.Detail == "" {
				return "", errors.New(ToolProgress + ": step and detail are required")
			}
			if err := sink.Emit(ProgressEvent{
				Step: args.Step, Detail: args.Detail,
				Attempt: args.Attempt, MaxAttempts: args.MaxAttempts,
			}); err != nil {
				return "", fmt.Errorf("%s: emit: %w", ToolProgress, err)
			}
			return "Progress event emitted.", nil
		},
	}
}

func newAddCommentTool(sink EventSink, name string) *yoliumTool {
	return &yoliumTool{
		name: name,
		description: "Append a comment to the work-item thread. Use this for human-facing " +
			"notes (e.g., what you tried, where the bug was, follow-up suggestions). " +
			"For incremental status updates, prefer yolium_progress.",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []string{"text"},
		},
		run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("%s: invalid arguments: %w", name, err)
			}
			if args.Text == "" {
				return "", errors.New(name + ": text is required")
			}
			if err := sink.Emit(CommentEvent{Text: args.Text}); err != nil {
				return "", fmt.Errorf("%s: emit: %w", name, err)
			}
			return "Comment emitted.", nil
		},
	}
}

func newCreateItemTool(sink EventSink) *yoliumTool {
	return &yoliumTool{
		name: ToolCreateItem,
		description: "Create a new kanban work item. Provide at least a title; description, " +
			"branch, agent assignment, ordering, parent linkage, and feature-parent flag are optional.",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":           map[string]any{"type": "string"},
				"description":     map[string]any{"type": "string"},
				"branch":          map[string]any{"type": "string"},
				"agentProvider":   map[string]any{"type": "string"},
				"order":           map[string]any{"type": "integer"},
				"model":           map[string]any{"type": "string"},
				"isFeatureParent": map[string]any{"type": "boolean"},
				"parentId":        map[string]any{"type": "string"},
				"parentBranch":    map[string]any{"type": "string"},
			},
			"required": []string{"title"},
		},
		run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args CreateItemEvent
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("%s: invalid arguments: %w", ToolCreateItem, err)
			}
			if args.Title == "" {
				return "", errors.New(ToolCreateItem + ": title is required")
			}
			if err := sink.Emit(args); err != nil {
				return "", fmt.Errorf("%s: emit: %w", ToolCreateItem, err)
			}
			return "Create-item event emitted.", nil
		},
	}
}

func newUpdateDescriptionTool(sink EventSink) *yoliumTool {
	return &yoliumTool{
		name:        ToolUpdateDescription,
		description: "Replace the current work-item description body.",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{"type": "string"},
			},
			"required": []string{"description"},
		},
		run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Description string `json:"description"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("%s: invalid arguments: %w", ToolUpdateDescription, err)
			}
			if args.Description == "" {
				return "", errors.New(ToolUpdateDescription + ": description is required")
			}
			if err := sink.Emit(UpdateDescriptionEvent{Description: args.Description}); err != nil {
				return "", fmt.Errorf("%s: emit: %w", ToolUpdateDescription, err)
			}
			return "Description updated.", nil
		},
	}
}

func newSetTestSpecsTool(sink EventSink) *yoliumTool {
	return &yoliumTool{
		name: ToolSetTestSpecs,
		description: "Attach a per-file list of test specifications to the work item. " +
			"Each entry in `specs` has {file, description, specs}.",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"specs": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"file":        map[string]any{"type": "string"},
							"description": map[string]any{"type": "string"},
							"specs": map[string]any{
								"type":  "array",
								"items": map[string]any{"type": "string"},
							},
						},
						"required": []string{"file", "specs"},
					},
				},
			},
			"required": []string{"specs"},
		},
		run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args SetTestSpecsEvent
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("%s: invalid arguments: %w", ToolSetTestSpecs, err)
			}
			if len(args.Specs) == 0 {
				return "", errors.New(ToolSetTestSpecs + ": at least one spec group is required")
			}
			if err := sink.Emit(args); err != nil {
				return "", fmt.Errorf("%s: emit: %w", ToolSetTestSpecs, err)
			}
			return "Test specs emitted.", nil
		},
	}
}

func newActionTool(sink EventSink) *yoliumTool {
	return &yoliumTool{
		name: ToolAction,
		description: "Emit a generic action event with an arbitrary `data` payload. Used by " +
			"specialized agents (e.g., scout, marketing) for events that don't fit the other types.",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":    map[string]any{"type": "string"},
				"data":      map[string]any{"type": "object"},
				"timestamp": map[string]any{"type": "string"},
			},
			"required": []string{"action"},
		},
		run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args ActionEvent
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("%s: invalid arguments: %w", ToolAction, err)
			}
			if args.Action == "" {
				return "", errors.New(ToolAction + ": action is required")
			}
			if err := sink.Emit(args); err != nil {
				return "", fmt.Errorf("%s: emit: %w", ToolAction, err)
			}
			return "Action emitted.", nil
		},
	}
}

func newStartAgentTool(sink EventSink) *yoliumTool {
	return &yoliumTool{
		name: ToolStartAgent,
		description: "Request that Yolium spawn an additional headless agent against an " +
			"existing work item. Used by orchestrator-style agents.",
		parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"itemId":        map[string]any{"type": "string"},
				"agentName":     map[string]any{"type": "string"},
				"goal":          map[string]any{"type": "string"},
				"agentProvider": map[string]any{"type": "string"},
			},
			"required": []string{"itemId", "agentName"},
		},
		run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args StartAgentEvent
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("%s: invalid arguments: %w", ToolStartAgent, err)
			}
			if args.ItemID == "" || args.AgentName == "" {
				return "", errors.New(ToolStartAgent + ": itemId and agentName are required")
			}
			if err := sink.Emit(args); err != nil {
				return "", fmt.Errorf("%s: emit: %w", ToolStartAgent, err)
			}
			return "Start-agent event emitted.", nil
		},
	}
}
