package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"yoli/internal/agent/tools"
	"yoli/internal/ai"
)

// scriptedProvider replays a fixed slice of ChatResponses in order and
// records every ChatRequest it sees.
type scriptedProvider struct {
	responses []ai.ChatResponse
	i         int
	calls     []ai.ChatRequest
}

func (s *scriptedProvider) Chat(_ context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	s.calls = append(s.calls, req)
	if s.i >= len(s.responses) {
		return ai.ChatResponse{}, errors.New("scriptedProvider: out of responses")
	}
	r := s.responses[s.i]
	s.i++
	return r, nil
}

// fnTool wraps a Go closure as a tools.Tool.
type fnTool struct {
	def   ai.ToolDefinition
	calls []json.RawMessage
	run   func(ctx context.Context, args json.RawMessage) (string, error)
}

func (f *fnTool) Definition() ai.ToolDefinition { return f.def }
func (f *fnTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	f.calls = append(f.calls, args)
	return f.run(ctx, args)
}

func ptr(s string) *string { return &s }

func userMsg(text string) ai.Message {
	return ai.Message{Role: ai.RoleUser, Content: ptr(text)}
}

func TestRun_PlainTextReplyReturnsAfterOneTurn(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{Content: ptr("final answer")},
	}}
	conv, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Messages: []ai.Message{userMsg("hello")},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(prov.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(prov.calls))
	}
	last := conv[len(conv)-1]
	if last.Role != ai.RoleAssistant {
		t.Fatalf("role = %q", last.Role)
	}
	if last.Content == nil || *last.Content != "final answer" {
		t.Fatalf("content = %v", last.Content)
	}
	if last.ToolCalls != nil {
		t.Fatalf("toolCalls = %v, want nil", last.ToolCalls)
	}
}

func TestRun_PassesMaxTokensIntoProviderRequest(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{Content: ptr("final answer")},
	}}
	_, err := Run(context.Background(), RunOptions{
		Provider:  prov,
		Model:     "m",
		Messages:  []ai.Message{userMsg("hello")},
		MaxTokens: 1234,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := prov.calls[0].MaxTokens; got != 1234 {
		t.Fatalf("MaxTokens = %d, want 1234", got)
	}
}

func TestRun_DefaultsMaxTokensIntoProviderRequest(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{Content: ptr("final answer")},
	}}
	_, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Messages: []ai.Message{userMsg("hello")},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := prov.calls[0].MaxTokens; got != DefaultMaxOutputTokens {
		t.Fatalf("MaxTokens = %d, want %d", got, DefaultMaxOutputTokens)
	}
}

func TestRun_DefaultMaxOutputTokensIs8192(t *testing.T) {
	// 4096 used to be the default and was too small: a single Write
	// call to create a ~7 KB source file emits ~4 K output tokens of
	// JSON-encoded content and gets truncated mid-arguments. Pin the
	// raised default so the bump isn't accidentally reverted.
	if DefaultMaxOutputTokens != 8192 {
		t.Fatalf("DefaultMaxOutputTokens = %d, want 8192", DefaultMaxOutputTokens)
	}
}

func TestRun_SanitizesTruncatedToolCallArgumentsBeforeReplay(t *testing.T) {
	// Reproduction of the real failure: a model hits its output cap
	// mid-tool-call and the provider returns a tool_call whose
	// `arguments` field is truncated JSON. Without sanitization the
	// next provider request includes that malformed string in the
	// conversation history and downstream validators (e.g. SiliconFlow
	// on OpenRouter) reject the whole request with 400. After the fix
	// the conversation message stored for replay must contain
	// well-formed JSON and the tool result must explain the truncation.
	truncated := `{"path":"src/vault/vaultManager.ts","content":"import type { Entry, KdfParams`
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "trunc1", Name: "Write", Arguments: truncated}}},
		{Content: ptr("ok, will retry shorter")},
	}}
	wrote := 0
	write := &fnTool{
		def: ai.ToolDefinition{Name: "Write", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, _ json.RawMessage) (string, error) {
			wrote++
			return "should not run", nil
		},
	}
	conv, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Tools:    []tools.Tool{write},
		Messages: []ai.Message{userMsg("go")},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if wrote != 0 {
		t.Fatalf("Write tool ran %d times, want 0 (truncated calls must NOT execute with default {})", wrote)
	}
	// Find the assistant message that carried the truncated call.
	var assistant *ai.Message
	for i := range conv {
		if conv[i].Role == ai.RoleAssistant && len(conv[i].ToolCalls) > 0 {
			assistant = &conv[i]
			break
		}
	}
	if assistant == nil {
		t.Fatalf("expected assistant message with tool calls in conv = %+v", conv)
	}
	if got := assistant.ToolCalls[0].Arguments; !json.Valid([]byte(got)) {
		t.Fatalf("assistant.ToolCalls[0].Arguments = %q, want valid JSON for safe replay", got)
	}
	// The tool result must surface the truncation so the model knows
	// to retry with shorter arguments.
	var toolMsg *ai.Message
	for i := range conv {
		if conv[i].Role == ai.RoleTool && conv[i].ToolCallID == "trunc1" {
			toolMsg = &conv[i]
			break
		}
	}
	if toolMsg == nil || toolMsg.Content == nil {
		t.Fatalf("expected a tool result message for the truncated call")
	}
	if !strings.Contains(*toolMsg.Content, "truncat") {
		t.Fatalf("tool result = %q, want a truncation-aware error message", *toolMsg.Content)
	}
	// Second provider call must have happened (loop continued) and the
	// request it sent must contain a valid JSON Arguments string for
	// the assistant turn — that's the invariant downstream providers
	// validate.
	if len(prov.calls) < 2 {
		t.Fatalf("provider calls = %d, want at least 2", len(prov.calls))
	}
	replayed := prov.calls[1].Messages
	foundValidReplay := false
	for _, m := range replayed {
		if m.Role != ai.RoleAssistant {
			continue
		}
		for _, c := range m.ToolCalls {
			if c.ID == "trunc1" && json.Valid([]byte(c.Arguments)) {
				foundValidReplay = true
			}
		}
	}
	if !foundValidReplay {
		t.Fatalf("replay round-trip did not contain a sanitized tool_call for trunc1")
	}
}

func TestRun_ExecutesToolCallThenContinues(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "call_1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		{Content: ptr("done")},
	}}
	echo := &fnTool{
		def: ai.ToolDefinition{
			Name:        "echo",
			Description: "echo back",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
				"required":   []string{"text"},
			},
		},
		run: func(_ context.Context, args json.RawMessage) (string, error) {
			var p struct{ Text string }
			if err := json.Unmarshal(args, &p); err != nil {
				return "", err
			}
			return "echoed:" + p.Text, nil
		},
	}
	conv, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Tools:    []tools.Tool{echo},
		Messages: []ai.Message{userMsg("go")},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(echo.calls) != 1 {
		t.Fatalf("echo called %d times", len(echo.calls))
	}
	var got struct{ Text string }
	if err := json.Unmarshal(echo.calls[0], &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Text != "hi" {
		t.Fatalf("text = %q", got.Text)
	}
	if len(prov.calls) != 2 {
		t.Fatalf("provider called %d times", len(prov.calls))
	}
	last2 := prov.calls[1].Messages[len(prov.calls[1].Messages)-1]
	if last2.Role != ai.RoleTool || last2.ToolCallID != "call_1" ||
		last2.Content == nil || *last2.Content != "echoed:hi" {
		t.Fatalf("tool msg = %+v", last2)
	}
	final := conv[len(conv)-1]
	if final.Role != ai.RoleAssistant || final.Content == nil || *final.Content != "done" {
		t.Fatalf("final = %+v", final)
	}
}

func TestRun_TruncatesOldToolResultsOverBudget(t *testing.T) {
	oldTool := strings.Repeat("t", 2000)
	messages := []ai.Message{
		{Role: ai.RoleSystem, Content: ptr("system")},
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{{
				ID:        "old_call",
				Name:      "old",
				Arguments: "{}",
			}},
		},
		{Role: ai.RoleTool, ToolCallID: "old_call", Content: &oldTool},
		userMsg("recent 1"),
		userMsg("recent 2"),
		userMsg("recent 3"),
		userMsg("recent 4"),
	}
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "new_call", Name: "noop", Arguments: "{}"}}},
		{Content: ptr("done")},
	}}
	noop := &fnTool{
		def: ai.ToolDefinition{Name: "noop", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil },
	}

	_, err := Run(context.Background(), RunOptions{
		Provider:            prov,
		Model:               "m",
		Tools:               []tools.Tool{noop},
		Messages:            messages,
		ContextBudgetTokens: 100,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	second := prov.calls[1].Messages
	if second[2].Content == nil || !strings.Contains(*second[2].Content, "[truncated: ") {
		t.Fatalf("old tool was not compacted: %+v", second[2])
	}
	for i := 3; i <= 6; i++ {
		if second[i].Content == nil || *second[i].Content != *messages[i].Content {
			t.Fatalf("recent message %d changed: got %+v want %+v", i, second[i], messages[i])
		}
	}
}

func TestRun_PreservesToolCallIDPairingDuringCompaction(t *testing.T) {
	oldTool := strings.Repeat("t", 2000)
	messages := []ai.Message{
		{Role: ai.RoleSystem, Content: ptr("system")},
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{{
				ID:        "old_call",
				Name:      "old",
				Arguments: strings.Repeat("a", 400),
			}},
		},
		{Role: ai.RoleTool, ToolCallID: "old_call", Content: &oldTool},
		userMsg("recent 1"),
		userMsg("recent 2"),
		userMsg("recent 3"),
		userMsg("recent 4"),
	}
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{Content: ptr("done")},
	}}

	_, err := Run(context.Background(), RunOptions{
		Provider:            prov,
		Model:               "m",
		Messages:            messages,
		ContextBudgetTokens: 100,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	got := prov.calls[0].Messages
	if len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].ID != "old_call" {
		t.Fatalf("assistant tool calls changed: %+v", got[1].ToolCalls)
	}
	if got[2].Role != ai.RoleTool || got[2].ToolCallID != "old_call" {
		t.Fatalf("tool call ID changed: %+v", got[2])
	}
}

func TestRun_TruncatesOldAssistantContentWhenToolCompactionInsufficient(t *testing.T) {
	oldAssistant := strings.Repeat("a", 2000)
	oldTool := strings.Repeat("t", 2000)
	messages := []ai.Message{
		{Role: ai.RoleSystem, Content: ptr("system")},
		{Role: ai.RoleAssistant, Content: &oldAssistant},
		{
			Role: ai.RoleAssistant,
			ToolCalls: []ai.ToolCall{{
				ID:        "old_call",
				Name:      "old",
				Arguments: "{}",
			}},
		},
		{Role: ai.RoleTool, ToolCallID: "old_call", Content: &oldTool},
		userMsg("recent 1"),
		userMsg("recent 2"),
		userMsg("recent 3"),
		userMsg("recent 4"),
	}
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{Content: ptr("done")},
	}}

	_, err := Run(context.Background(), RunOptions{
		Provider:            prov,
		Model:               "m",
		Messages:            messages,
		ContextBudgetTokens: 100,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	got := prov.calls[0].Messages
	if got[1].Content == nil || !strings.Contains(*got[1].Content, "[truncated: ") {
		t.Fatalf("old assistant content was not compacted: %+v", got[1])
	}
	if got[3].Content == nil || !strings.Contains(*got[3].Content, "[truncated: ") {
		t.Fatalf("old tool content was not compacted: %+v", got[3])
	}
}

func TestRun_UnknownToolProducesErrorMessage(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "call_x", Name: "no_such_tool", Arguments: "{}"}}},
		{Content: ptr("sorry")},
	}}
	_, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Messages: []ai.Message{userMsg("go")},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	last := prov.calls[1].Messages[len(prov.calls[1].Messages)-1]
	if last.Role != ai.RoleTool || last.ToolCallID != "call_x" {
		t.Fatalf("tool msg = %+v", last)
	}
	if last.Content == nil || !strings.Contains(*last.Content, "no_such_tool") {
		t.Fatalf("content = %v", last.Content)
	}
}

func TestRun_CapturesToolErrorAsContent(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "call_e", Name: "boom", Arguments: "{}"}}},
		{Content: ptr("k")},
	}}
	boom := &fnTool{
		def: ai.ToolDefinition{Name: "boom", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, _ json.RawMessage) (string, error) {
			return "", fmt.Errorf("explosion")
		},
	}
	_, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Tools:    []tools.Tool{boom},
		Messages: []ai.Message{userMsg("go")},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	last := prov.calls[1].Messages[len(prov.calls[1].Messages)-1]
	if last.Content == nil || !strings.Contains(*last.Content, "explosion") {
		t.Fatalf("content = %v", last.Content)
	}
}

func TestRun_StopsAtMaxIterations(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "a", Name: "noop", Arguments: "{}"}}},
		{ToolCalls: []ai.ToolCall{{ID: "b", Name: "noop", Arguments: "{}"}}},
		{ToolCalls: []ai.ToolCall{{ID: "c", Name: "noop", Arguments: "{}"}}},
	}}
	noop := &fnTool{
		def: ai.ToolDefinition{Name: "noop", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil },
	}
	_, err := Run(context.Background(), RunOptions{
		Provider:      prov,
		Model:         "m",
		Tools:         []tools.Tool{noop},
		Messages:      []ai.Message{userMsg("go")},
		MaxIterations: 2,
	})
	if err == nil {
		t.Fatalf("want error")
	}
	if !regexp.MustCompile(`maxIterations`).MatchString(err.Error()) {
		t.Fatalf("err = %v", err)
	}
	if len(prov.calls) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(prov.calls))
	}
}

func TestRun_RepeatedIdenticalToolCallsAreAllowedUntilMaxIterations(t *testing.T) {
	// Yoli follows the PI / OpenCode design: there is no stall
	// detection. A weak model that re-issues the same tool call every
	// turn is allowed to do so until it either makes progress or hits
	// the iteration cap. This test pins that behavior.
	stuckArgs := `{"command":"echo stuck"}`
	responses := make([]ai.ChatResponse, DefaultMaxIterations)
	for i := range responses {
		responses[i] = ai.ChatResponse{
			ToolCalls: []ai.ToolCall{{
				ID:        fmt.Sprintf("c%d", i),
				Name:      "Bash",
				Arguments: stuckArgs,
			}},
		}
	}
	prov := &scriptedProvider{responses: responses}
	bash := &fnTool{
		def: ai.ToolDefinition{Name: "Bash", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil },
	}
	_, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Tools:    []tools.Tool{bash},
		Messages: []ai.Message{userMsg("go")},
	})
	if err == nil {
		t.Fatalf("want maxIterations error")
	}
	if !strings.Contains(err.Error(), "maxIterations") {
		t.Fatalf("err = %v, want maxIterations error (NOT a stall error)", err)
	}
	if len(prov.calls) != DefaultMaxIterations {
		t.Fatalf("provider calls = %d, want %d (the full iteration budget — no early stall abort)", len(prov.calls), DefaultMaxIterations)
	}
}

func TestRun_UsesRaisedDefaultMaxIterations(t *testing.T) {
	responses := make([]ai.ChatResponse, DefaultMaxIterations)
	for i := range responses {
		responses[i] = ai.ChatResponse{
			ToolCalls: []ai.ToolCall{{
				ID:        fmt.Sprintf("c%d", i),
				Name:      "noop",
				Arguments: fmt.Sprintf(`{"iter":%d}`, i),
			}},
		}
	}
	prov := &scriptedProvider{responses: responses}
	noop := &fnTool{
		def: ai.ToolDefinition{Name: "noop", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil },
	}
	_, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Tools:    []tools.Tool{noop},
		Messages: []ai.Message{userMsg("go")},
	})
	if err == nil {
		t.Fatalf("want maxIterations error")
	}
	if !regexp.MustCompile(`maxIterations`).MatchString(err.Error()) {
		t.Fatalf("err = %v", err)
	}
	if len(prov.calls) != DefaultMaxIterations {
		t.Fatalf("provider calls = %d, want %d", len(prov.calls), DefaultMaxIterations)
	}
	if DefaultMaxIterations != 64 {
		t.Fatalf("DefaultMaxIterations = %d, want 64", DefaultMaxIterations)
	}
}

func TestRun_OnMessageCallbackFiresForEveryAppend(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "echo", Arguments: "{}"}}},
		{Content: ptr("done")},
	}}
	echo := &fnTool{
		def: ai.ToolDefinition{Name: "echo", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, _ json.RawMessage) (string, error) { return "hi", nil },
	}
	var seen []ai.Role
	_, err := Run(context.Background(), RunOptions{
		Provider:  prov,
		Model:     "m",
		Tools:     []tools.Tool{echo},
		Messages:  []ai.Message{userMsg("go")},
		OnMessage: func(m ai.Message) { seen = append(seen, m.Role) },
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []ai.Role{ai.RoleAssistant, ai.RoleTool, ai.RoleAssistant}
	if len(seen) != len(want) {
		t.Fatalf("seen = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("seen[%d] = %q, want %q", i, seen[i], want[i])
		}
	}
}

func TestRun_InvalidJSONArgumentsProduceErrorMessage(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c", Name: "echo", Arguments: "{not json"}}},
		{Content: ptr("ok")},
	}}
	echo := &fnTool{
		def: ai.ToolDefinition{Name: "echo", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, _ json.RawMessage) (string, error) {
			t.Fatalf("tool should not have run")
			return "", nil
		},
	}
	_, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Tools:    []tools.Tool{echo},
		Messages: []ai.Message{userMsg("go")},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	last := prov.calls[1].Messages[len(prov.calls[1].Messages)-1]
	if last.Content == nil || !strings.Contains(*last.Content, "truncat") {
		t.Fatalf("content = %v, want a truncation-aware error", last.Content)
	}
}

func TestRun_EmptyArgumentsTreatedAsEmptyObject(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c", Name: "noop", Arguments: ""}}},
		{Content: ptr("ok")},
	}}
	noop := &fnTool{
		def: ai.ToolDefinition{Name: "noop", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, args json.RawMessage) (string, error) {
			if string(args) != "{}" {
				t.Fatalf("args = %q, want {}", args)
			}
			return "ran", nil
		},
	}
	if _, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Tools:    []tools.Tool{noop},
		Messages: []ai.Message{userMsg("go")},
	}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(noop.calls) != 1 {
		t.Fatalf("noop called %d times", len(noop.calls))
	}
}
