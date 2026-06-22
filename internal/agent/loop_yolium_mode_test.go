package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"yoli/internal/agent/tools"
	"yoli/internal/ai"
)

// TestRun_YoliumModeDefault_NoToolCallsExits pins today's behavior: when
// YoliumMode is false (default), a response with zero tool calls
// terminates the loop. Standalone yoli must remain byte-for-byte
// identical.
func TestRun_YoliumModeDefault_NoToolCallsExits(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{Content: ptr("done")},
	}}
	conv, err := Run(context.Background(), RunOptions{
		Provider: prov,
		Model:    "m",
		Messages: []ai.Message{userMsg("hi")},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(prov.calls) != 1 {
		t.Fatalf("calls=%d want 1", len(prov.calls))
	}
	if conv[len(conv)-1].Role != ai.RoleAssistant {
		t.Fatalf("last role=%q", conv[len(conv)-1].Role)
	}
}

// TestRun_YoliumModeEnabled_NoToolCallsContinues verifies that under
// --yolium-mode the loop does NOT exit on a turn with no tool calls.
// Instead, it injects a nudge user message and continues. Termination
// happens via Stop() returning true (set by a terminator tool).
func TestRun_YoliumModeEnabled_NoToolCallsContinues(t *testing.T) {
	// Turn 1: empty (no tool calls). Loop should continue + nudge.
	// Turn 2: empty again — Stop() returns true after this turn.
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{Content: ptr("")},
		{Content: ptr("ack")},
	}}
	stopCalls := 0
	conv, err := Run(context.Background(), RunOptions{
		Provider:   prov,
		Model:      "m",
		Messages:   []ai.Message{userMsg("hi")},
		YoliumMode: true,
		Stop: func() bool {
			stopCalls++
			// Return false after turn 1, true after turn 2.
			return stopCalls >= 2
		},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(prov.calls) != 2 {
		t.Fatalf("calls=%d want 2 (loop should not have exited after turn 1)", len(prov.calls))
	}
	// A nudge user message should have been appended between the two
	// assistant turns.
	var sawNudge bool
	for _, m := range conv {
		if m.Role == ai.RoleUser && m.Content != nil && strings.Contains(*m.Content, "terminator tool") {
			sawNudge = true
			break
		}
	}
	if !sawNudge {
		t.Fatalf("expected nudge user message containing 'terminator tool'; conv=%+v", conv)
	}
}

// TestRun_YoliumModeEnabled_StopTerminatesCleanly verifies that Stop()
// returning true under YoliumMode still terminates with a clean (nil)
// error, matching the default-mode termination contract.
func TestRun_YoliumModeEnabled_StopTerminatesCleanly(t *testing.T) {
	prov := &scriptedProvider{responses: []ai.ChatResponse{
		{Content: ptr("simulated complete tool")},
	}}
	conv, err := Run(context.Background(), RunOptions{
		Provider:   prov,
		Model:      "m",
		Messages:   []ai.Message{userMsg("hi")},
		YoliumMode: true,
		Stop: func() bool {
			return true
		},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(conv) < 2 {
		t.Fatalf("conv too short: %d", len(conv))
	}
}

// TestRun_YoliumMode_BudgetWarningAndFlushRecovers verifies that under
// YoliumMode a run that would otherwise exhaust its iteration budget gets
// (a) a one-time budget warning near ~80% of the cap, and (b) a final
// wrap-up "flush" turn where a terminator can still fire — turning a hard
// cap failure into a clean completion.
func TestRun_YoliumMode_BudgetWarningAndFlushRecovers(t *testing.T) {
	const max = 5
	// max loop responses + 1 flush response, all non-terminating tool calls.
	responses := make([]ai.ChatResponse, max+1)
	for i := range responses {
		responses[i] = ai.ChatResponse{ToolCalls: []ai.ToolCall{{
			ID:        fmt.Sprintf("c%d", i),
			Name:      "noop",
			Arguments: "{}",
		}}}
	}
	prov := &scriptedProvider{responses: responses}
	noop := &fnTool{
		def: ai.ToolDefinition{Name: "noop", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil },
	}
	// Simulate a terminator firing only on the final flush turn: Stop()
	// returns true once the provider has been called for the flush turn.
	conv, err := Run(context.Background(), RunOptions{
		Provider:      prov,
		Model:         "m",
		Tools:         []tools.Tool{noop},
		Messages:      []ai.Message{userMsg("go")},
		MaxIterations: max,
		YoliumMode:    true,
		Stop:          func() bool { return len(prov.calls) > max },
	})
	if err != nil {
		t.Fatalf("flush turn should recover cleanly, got err: %v", err)
	}
	if len(prov.calls) != max+1 {
		t.Fatalf("provider calls = %d, want %d (cap + one flush turn)", len(prov.calls), max+1)
	}
	var sawWarning, sawFlush bool
	for _, m := range conv {
		if m.Role != ai.RoleUser || m.Content == nil {
			continue
		}
		if strings.Contains(*m.Content, "nearing your iteration budget") {
			sawWarning = true
		}
		if strings.Contains(*m.Content, "This is your FINAL turn") {
			sawFlush = true
		}
	}
	if !sawWarning {
		t.Fatalf("expected a budget-warning user message; conv=%+v", conv)
	}
	if !sawFlush {
		t.Fatalf("expected a final flush user message; conv=%+v", conv)
	}
}

// TestRun_YoliumMode_FlushStillFailsReturnsCapError verifies that when the
// final flush turn ALSO fails to emit a terminator, Run still surfaces the
// maxIterations error (after exactly one extra flush round-trip).
func TestRun_YoliumMode_FlushStillFailsReturnsCapError(t *testing.T) {
	const max = 3
	responses := make([]ai.ChatResponse, max+1)
	for i := range responses {
		responses[i] = ai.ChatResponse{ToolCalls: []ai.ToolCall{{
			ID:        fmt.Sprintf("c%d", i),
			Name:      "noop",
			Arguments: "{}",
		}}}
	}
	prov := &scriptedProvider{responses: responses}
	noop := &fnTool{
		def: ai.ToolDefinition{Name: "noop", Parameters: map[string]any{"type": "object"}},
		run: func(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil },
	}
	_, err := Run(context.Background(), RunOptions{
		Provider:      prov,
		Model:         "m",
		Tools:         []tools.Tool{noop},
		Messages:      []ai.Message{userMsg("go")},
		MaxIterations: max,
		YoliumMode:    true,
		Stop:          func() bool { return false },
	})
	if err == nil || !strings.Contains(err.Error(), "maxIterations") {
		t.Fatalf("want maxIterations error, got %v", err)
	}
	if len(prov.calls) != max+1 {
		t.Fatalf("provider calls = %d, want %d (cap + one flush turn)", len(prov.calls), max+1)
	}
}
