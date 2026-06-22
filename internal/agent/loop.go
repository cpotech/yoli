// Package agent implements the chat-with-tools loop: repeatedly call the
// provider, dispatch any tool calls, append results, and repeat until
// the model emits a plain text reply or maxIterations is exhausted.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"yoli/internal/agent/tools"
	"yoli/internal/ai"
)

// DefaultMaxIterations bounds the chat loop when RunOptions.MaxIterations
// is unset. This is the ONLY safety bound on the loop; yoli follows the
// PI / OpenCode design and does not perform stall detection or
// duplicate-tool-call analysis. Weak models that re-Read the same file
// or burn turns "narrating" without progress are allowed to do so until
// they either make forward progress or hit the iteration cap. This
// trades a worst-case waste of MaxIterations provider round-trips for
// not killing runs whose models are merely exploratory.
const DefaultMaxIterations = 64

// RunOptions configures Run.
type RunOptions struct {
	Provider            ai.Provider
	Model               string
	Tools               []tools.Tool
	Messages            []ai.Message
	MaxIterations       int
	MaxTokens           int
	ContextBudgetTokens int
	// OnMessage, if non-nil, is invoked synchronously each time a new
	// assistant or tool message is appended to the conversation.
	OnMessage func(ai.Message)
	// Stop, if non-nil, is consulted after each assistant turn (and
	// after OnMessage runs for that turn). Returning true causes Run to
	// return the current conversation cleanly with no error, as if the
	// model had produced a final text reply. Used to terminate the loop
	// when the assistant's prose contains a terminal @@YOLIUM: event
	// such as complete/error/question.
	Stop func() bool
	// YoliumMode opts the loop into Yolium-integrated behavior. When
	// true, an assistant turn that produces zero tool calls is NOT
	// treated as a terminator — the loop continues until Stop returns
	// true (set by a terminator tool like yolium_complete) or the
	// iteration cap is hit. This matches
	// Yolium's protocol where terminators are explicit tool calls, not
	// "no further tool calls". Defaults false; standalone yoli behavior
	// is unchanged.
	YoliumMode bool
}

// Run executes the agent loop. It returns the full conversation —
// including the seed Messages, every assistant turn, and every tool
// result — once the model produces a turn with no tool calls.
//
// Returns an error when the iteration cap is hit without a final reply.
// Tool errors do NOT propagate; they are captured as the tool message
// content so the model can react to them.
func Run(ctx context.Context, opts RunOptions) ([]ai.Message, error) {
	max := opts.MaxIterations
	if max <= 0 {
		max = DefaultMaxIterations
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxOutputTokens
	}
	contextBudget := opts.ContextBudgetTokens
	if contextBudget <= 0 {
		contextBudget = DefaultContextBudget
	}

	index := make(map[string]tools.Tool, len(opts.Tools))
	defs := make([]ai.ToolDefinition, len(opts.Tools))
	for i, t := range opts.Tools {
		def := t.Definition()
		defs[i] = def
		index[def.Name] = t
	}

	conv := make([]ai.Message, len(opts.Messages))
	copy(conv, opts.Messages)

	budgetWarned := false
	for i := 0; i < max; i++ {
		// Budget warning: once the loop crosses ~80% of its iteration
		// cap, inject a one-time heads-up so the model wraps up and
		// emits a terminator BEFORE it runs out of turns. Yolium-mode
		// only — standalone yoli ends naturally on a no-tool-call turn
		// and rarely approaches the cap. Guarded by max >= 5 so tiny
		// caps used in tests don't fire a warning on turn 1.
		if opts.YoliumMode && !budgetWarned && max >= 5 && i >= (max*4)/5 {
			budgetWarned = true
			warn := fmt.Sprintf(
				"Heads-up: you are nearing your iteration budget (turn %d of %d). "+
					"Wrap up the remaining work and call a terminator tool SOON — "+
					"yolium_complete with a summary if the task is done, or yolium_error "+
					"if you are blocked. Do not start large new sub-tasks now.",
				i+1, max,
			)
			warnMsg := ai.Message{Role: ai.RoleUser, Content: &warn}
			conv = append(conv, warnMsg)
			if opts.OnMessage != nil {
				opts.OnMessage(warnMsg)
			}
		}

		req := ai.ChatRequest{
			Model:     opts.Model,
			Messages:  compactConversation(conv, contextBudget),
			MaxTokens: maxTokens,
		}
		if len(defs) > 0 {
			req.Tools = defs
		}
		resp, err := opts.Provider.Chat(ctx, req)
		if err != nil {
			return conv, err
		}

		assistant := ai.Message{
			Role:    ai.RoleAssistant,
			Content: resp.Content,
		}
		// Sanitize tool_call Arguments fields BEFORE storing the
		// assistant message. When a model hits its output cap
		// mid-tool-call the provider returns a tool_call whose
		// `arguments` string is truncated mid-JSON. Storing that as-is
		// poisons the conversation: on the next turn we replay the
		// assistant message back to the provider, and validators on
		// downstream OpenAI-compatible backends (e.g. SiliconFlow)
		// reject the whole request with HTTP 400. Replace any invalid
		// JSON with `{}` for safe replay; the matching tool_result we
		// emit below explains the truncation so the model can retry.
		truncated := make(map[string]bool, len(resp.ToolCalls))
		if len(resp.ToolCalls) > 0 {
			sanitized := make([]ai.ToolCall, len(resp.ToolCalls))
			for i, c := range resp.ToolCalls {
				sanitized[i] = c
				if len(c.Arguments) == 0 {
					continue
				}
				if !json.Valid([]byte(c.Arguments)) {
					sanitized[i].Arguments = "{}"
					truncated[c.ID] = true
				}
			}
			assistant.ToolCalls = sanitized
		}
		conv = append(conv, assistant)
		if opts.OnMessage != nil {
			opts.OnMessage(assistant)
		}

		if opts.Stop != nil && opts.Stop() {
			return conv, nil
		}

		// Yolium mode only: under `--yolium-mode` plain `@@YOLIUM:{...}`
		// lines in assistant content are IGNORED (the side-channel
		// `yolium_*` tools are the only valid event source). Weak
		// models — especially ones that saw the legacy text protocol
		// during pre-training — will sometimes regurgitate the text
		// form anyway and silently fail to terminate the loop, or
		// duplicate a tool event they already emitted. Detect the
		// pattern and inject a corrective user message so the model
		// re-emits the intended event via the proper tool on the next
		// turn. We do this before the no-tool-call branch so a hybrid
		// turn (some tool calls plus a stray @@YOLIUM: text) still
		// gets the correction.
		if opts.YoliumMode && resp.Content != nil && containsYoliumProtocolText(*resp.Content) {
			nudge := "I detected `@@YOLIUM:{...}` in your assistant content. " +
				"Under Yolium mode that text protocol is IGNORED — only the native `yolium_*` tools emit events. " +
				"If you intended to report progress, call `yolium_progress`. " +
				"If you intended to finish, call exactly one TERMINATOR: " +
				"`yolium_complete`, `yolium_error`, or `yolium_ask_question`. " +
				"Re-emit the event by calling the matching tool now — do NOT echo protocol JSON as text."
			nudgeMsg := ai.Message{Role: ai.RoleUser, Content: &nudge}
			conv = append(conv, nudgeMsg)
			if opts.OnMessage != nil {
				opts.OnMessage(nudgeMsg)
			}
			// Don't `continue` here: if the model also made tool calls
			// in the same turn, we still want to run them below before
			// the next provider round-trip.
		}

		if len(resp.ToolCalls) == 0 {
			if !opts.YoliumMode {
				return conv, nil
			}
			// Under YoliumMode, a turn with no tool calls is not a
			// terminator — keep looping. The model is expected to call
			// a terminator tool (yolium_complete / yolium_error /
			// yolium_ask_question), which sets ExitSignal and causes
			// Stop() above to return true on the next turn.
			//
			// Inject a nudge as a user message so the model gets a
			// fresh prompt rather than re-running on identical history
			// and burning another iteration on the same empty turn.
			nudge := "You produced an assistant turn with no tool calls. " +
				"Under Yolium mode, the loop only ends when you call a terminator tool " +
				"(yolium_complete, yolium_error, or yolium_ask_question). " +
				"Call the appropriate terminator tool now, or continue with productive tool calls."
			nudgeMsg := ai.Message{Role: ai.RoleUser, Content: &nudge}
			conv = append(conv, nudgeMsg)
			if opts.OnMessage != nil {
				opts.OnMessage(nudgeMsg)
			}
			continue
		}

		for _, call := range resp.ToolCalls {
			var result string
			if truncated[call.ID] {
				// Don't actually execute: the original args were
				// truncated mid-JSON and the sanitized {} would either
				// run with unintended defaults or crash inside the
				// tool. Surface the truncation so the model retries
				// with shorter arguments.
				result = fmt.Sprintf(
					"Error: arguments for tool %s were truncated mid-response (the model hit its output-token cap "+
						"before the JSON closed). The call was NOT executed. Retry with shorter arguments — for "+
						"Write, emit a smaller file or split the file into multiple Writes; for Edit, make smaller "+
						"individual edits.",
					call.Name,
				)
			} else {
				result = runToolCall(ctx, index, call)
			}
			content := result
			toolMsg := ai.Message{
				Role:       ai.RoleTool,
				ToolCallID: call.ID,
				Content:    &content,
			}
			conv = append(conv, toolMsg)
			if opts.OnMessage != nil {
				opts.OnMessage(toolMsg)
			}
		}

		// A tool may have set the exit condition (e.g., yolium_complete
		// under --yolium-mode sets ExitSignal.Pending which Stop()
		// reads). Check Stop() here so we don't waste another provider
		// round-trip after a clearly terminating tool fired.
		if opts.Stop != nil && opts.Stop() {
			return conv, nil
		}
	}

	// Iteration cap reached without a terminator. Under Yolium mode, give
	// the model ONE final wrap-up turn (tools still enabled so it can call
	// yolium_complete / yolium_error) before surfacing a hard failure to
	// the host. This recovers the common case where the model finished the
	// real work but ran out of turns before reporting it. A single bounded
	// extra round-trip: if it still doesn't terminate — or the provider
	// errors — we fall through to the original cap error.
	if opts.YoliumMode {
		if conv, ok := flushTerminator(ctx, opts, conv, index, defs, maxTokens, contextBudget, max); ok {
			return conv, nil
		}
	}

	return conv, fmt.Errorf("Agent stopped after reaching maxIterations=%d without a final response", max)
}

// flushTerminator runs a single final wrap-up turn after the iteration cap
// is hit. It injects an explicit "this is your last turn, call a terminator
// now" instruction, makes one provider call with tools still available, and
// dispatches any resulting tool calls. It returns the (possibly extended)
// conversation and true when a terminator fired (Stop() became true);
// otherwise it returns false so the caller surfaces the cap error.
func flushTerminator(
	ctx context.Context,
	opts RunOptions,
	conv []ai.Message,
	index map[string]tools.Tool,
	defs []ai.ToolDefinition,
	maxTokens, contextBudget, max int,
) ([]ai.Message, bool) {
	flush := fmt.Sprintf(
		"You have reached the iteration cap (%d turns). This is your FINAL turn. "+
			"Call exactly ONE terminator tool NOW: yolium_complete with a concise summary "+
			"of what you accomplished (and anything left unfinished), or yolium_error if you "+
			"made no usable progress. Do not start any other work.",
		max,
	)
	flushMsg := ai.Message{Role: ai.RoleUser, Content: &flush}
	conv = append(conv, flushMsg)
	if opts.OnMessage != nil {
		opts.OnMessage(flushMsg)
	}

	req := ai.ChatRequest{
		Model:     opts.Model,
		Messages:  compactConversation(conv, contextBudget),
		MaxTokens: maxTokens,
	}
	if len(defs) > 0 {
		req.Tools = defs
	}
	resp, err := opts.Provider.Chat(ctx, req)
	if err != nil {
		return conv, false
	}

	assistant := ai.Message{Role: ai.RoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls}
	conv = append(conv, assistant)
	if opts.OnMessage != nil {
		opts.OnMessage(assistant)
	}
	if opts.Stop != nil && opts.Stop() {
		return conv, true
	}

	for _, call := range resp.ToolCalls {
		result := runToolCall(ctx, index, call)
		content := result
		toolMsg := ai.Message{Role: ai.RoleTool, ToolCallID: call.ID, Content: &content}
		conv = append(conv, toolMsg)
		if opts.OnMessage != nil {
			opts.OnMessage(toolMsg)
		}
	}
	if opts.Stop != nil && opts.Stop() {
		return conv, true
	}
	return conv, false
}

// yoliumProtocolMarker is the prefix used by the legacy text-based
// `@@YOLIUM:{...}` protocol. Under `--yolium-mode` plain-text emissions
// of this form are ignored by Yolium (the side-channel native tools
// are the only valid event source), so finding the marker in assistant
// content is a strong signal that the model regressed to the legacy
// protocol and the loop should inject a corrective nudge.
const yoliumProtocolMarker = "@@YOLIUM:"

// containsYoliumProtocolText reports whether the assistant content
// contains a literal `@@YOLIUM:` marker. The check is intentionally
// loose (substring match rather than full JSON parse): the goal is to
// catch the regression, not to validate the embedded JSON. A false
// positive — e.g. an agent quoting the marker in pure prose — is
// acceptable because the corrective nudge is still accurate guidance
// in that situation.
func containsYoliumProtocolText(content string) bool {
	return strings.Contains(content, yoliumProtocolMarker)
}

func compactConversation(conv []ai.Message, budget int) []ai.Message {
	out := make([]ai.Message, len(conv))
	copy(out, conv)
	if budget <= 0 || estimateConversationTokens(out) <= budget {
		return out
	}

	lastProtected := len(out) - 4
	if lastProtected < 0 {
		lastProtected = 0
	}
	for i := range out {
		if isProtectedMessage(out, i, lastProtected) {
			continue
		}
		if out[i].Role != ai.RoleTool || out[i].Content == nil || *out[i].Content == "" {
			continue
		}
		original := *out[i].Content
		marker := truncationMarker(len(original))
		out[i].Content = &marker
		if estimateConversationTokens(out) <= budget {
			return out
		}
	}

	for i := range out {
		if isProtectedMessage(out, i, lastProtected) {
			continue
		}
		if out[i].Role != ai.RoleAssistant || out[i].Content == nil || *out[i].Content == "" {
			continue
		}
		original := *out[i].Content
		marker := truncationMarker(len(original))
		out[i].Content = &marker
		if estimateConversationTokens(out) <= budget {
			return out
		}
	}

	return out
}

func isProtectedMessage(conv []ai.Message, i, lastProtected int) bool {
	if i == 0 && len(conv) > 0 && conv[i].Role == ai.RoleSystem {
		return true
	}
	return i >= lastProtected
}

// runToolCall dispatches a single tool call and converts any error into
// a string that becomes the tool message content.
func runToolCall(ctx context.Context, index map[string]tools.Tool, call ai.ToolCall) string {
	tool, ok := index[call.Name]
	if !ok {
		return "Error: unknown tool " + call.Name
	}
	args := json.RawMessage(call.Arguments)
	if len(args) == 0 {
		args = json.RawMessage("{}")
	} else {
		var probe any
		if err := json.Unmarshal(args, &probe); err != nil {
			return fmt.Sprintf("Error: invalid JSON arguments for %s: %s", call.Name, err.Error())
		}
	}
	// Normalise camelCase keys (e.g. oldString) to snake_case (old_string)
	// before dispatch. See tools.NormalizeArgKeys for rationale.
	args = tools.NormalizeArgKeys(args)
	out, err := tool.Run(ctx, args)
	if err != nil {
		return fmt.Sprintf("Error running %s: %s", call.Name, err.Error())
	}
	return out
}
