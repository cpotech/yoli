package agent

import (
	"strings"
	"testing"

	"yoli/internal/ai"
)

func TestEstimateTokens_RoughCharsPer4(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "empty", in: "", want: 0},
		{name: "short", in: "abc", want: 1},
		{name: "exact", in: "abcd", want: 1},
		{name: "round up", in: "abcde", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateTokens(tt.in); got != tt.want {
				t.Fatalf("estimateTokens(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestEstimateMessageTokens_IncludesContentAndToolCalls(t *testing.T) {
	content := strings.Repeat("a", 40)
	base := estimateMessageTokens(ai.Message{
		Role:    ai.RoleAssistant,
		Content: &content,
	})
	withTool := estimateMessageTokens(ai.Message{
		Role:    ai.RoleAssistant,
		Content: &content,
		ToolCalls: []ai.ToolCall{{
			ID:        "call_1",
			Name:      "Read",
			Arguments: strings.Repeat("b", 400),
		}},
	})
	if withTool <= base {
		t.Fatalf("withTool = %d, want > base %d", withTool, base)
	}
}

func TestEstimateContextTokens_EmptyIsZero(t *testing.T) {
	if got := EstimateContextTokens(nil); got != 0 {
		t.Fatalf("EstimateContextTokens(nil) = %d, want 0", got)
	}
	if got := EstimateContextTokens([]ai.Message{}); got != 0 {
		t.Fatalf("EstimateContextTokens([]) = %d, want 0", got)
	}
}

func TestEstimateContextTokens_MatchesInternalHelper(t *testing.T) {
	system := strings.Repeat("s", 80)
	user := strings.Repeat("u", 120)
	conv := []ai.Message{
		{Role: ai.RoleSystem, Content: &system},
		{Role: ai.RoleUser, Content: &user},
	}
	if got, want := EstimateContextTokens(conv), estimateConversationTokens(conv); got != want {
		t.Fatalf("EstimateContextTokens = %d, want %d", got, want)
	}
}

func TestEstimateContextTokens_IncreasesOnAppend(t *testing.T) {
	first := strings.Repeat("a", 40)
	conv := []ai.Message{{Role: ai.RoleUser, Content: &first}}
	before := EstimateContextTokens(conv)
	extra := strings.Repeat("b", 40)
	conv = append(conv, ai.Message{Role: ai.RoleAssistant, Content: &extra})
	if after := EstimateContextTokens(conv); after <= before {
		t.Fatalf("after = %d, want > before %d", after, before)
	}
}

func TestTruncateString_AddsMarker(t *testing.T) {
	in := strings.Repeat("x", 100)
	got := truncateString(in, 20)
	if got == in {
		t.Fatalf("truncateString returned original")
	}
	if !strings.Contains(got, "[truncated: ") || !strings.Contains(got, " bytes elided]") {
		t.Fatalf("missing marker in %q", got)
	}
	if len(got) >= len(in) {
		t.Fatalf("len(got) = %d, want < %d", len(got), len(in))
	}
}

func TestTruncateString_PassesThroughSmallInput(t *testing.T) {
	in := "small"
	if got := truncateString(in, 20); got != in {
		t.Fatalf("truncateString = %q, want %q", got, in)
	}
}

func TestComputeInputBudget_ReservesOutputHeadroom(t *testing.T) {
	// budget = (window - maxTokens - toolDefTokens) * 9 / 10 when the
	// output reservation fits in half the window.
	if got, want := computeInputBudget(1000, 400, 0), 540; got != want {
		t.Fatalf("computeInputBudget(1000, 400, 0) = %d, want %d", got, want)
	}
	if got, want := computeInputBudget(180_000, 8192, 0), 154_627; got != want {
		t.Fatalf("computeInputBudget(180000, 8192, 0) = %d, want %d", got, want)
	}
}

func TestComputeInputBudget_Regression32768(t *testing.T) {
	// The original failure: input 24577 + requested output 8192 > 32768
	// on a vLLM server capped at 32768 total tokens. With the window set
	// to 32768 the derived input budget plus the output reservation must
	// never exceed the window.
	got := computeInputBudget(32768, 8192, 0)
	if want := 22118; got != want {
		t.Fatalf("computeInputBudget(32768, 8192, 0) = %d, want %d", got, want)
	}
	if got+8192 > 32768 {
		t.Fatalf("input budget %d + output 8192 exceeds window 32768", got)
	}
	// Even with substantial tool definitions the invariant holds.
	withDefs := computeInputBudget(32768, 8192, 1500)
	if withDefs+8192+1500 > 32768 {
		t.Fatalf("input budget %d + output 8192 + tool defs 1500 exceeds window 32768", withDefs)
	}
	if withDefs >= got {
		t.Fatalf("tool-def tokens did not reduce the budget: %d >= %d", withDefs, got)
	}
}

func TestComputeInputBudget_ClampsReservationToHalfWindow(t *testing.T) {
	// Tiny windows (as used by tests) must keep at least half the window
	// for input even when maxTokens is huge.
	if got, want := computeInputBudget(100, 8192, 0), 45; got != want {
		t.Fatalf("computeInputBudget(100, 8192, 0) = %d, want %d", got, want)
	}
}

func TestComputeInputBudget_ClampsResultToTenthOfWindow(t *testing.T) {
	// Oversized tool definitions can never zero (or negate) the budget.
	if got, want := computeInputBudget(1000, 400, 100_000), 100; got != want {
		t.Fatalf("computeInputBudget(1000, 400, 100000) = %d, want %d", got, want)
	}
}

func TestEstimateToolDefTokens_EmptyIsZero(t *testing.T) {
	if got := estimateToolDefTokens(nil); got != 0 {
		t.Fatalf("estimateToolDefTokens(nil) = %d, want 0", got)
	}
	if got := estimateToolDefTokens([]ai.ToolDefinition{}); got != 0 {
		t.Fatalf("estimateToolDefTokens([]) = %d, want 0", got)
	}
}

func TestEstimateToolDefTokens_CountsDefinitionJSON(t *testing.T) {
	def := ai.ToolDefinition{
		Name:        "Read",
		Description: strings.Repeat("d", 400),
		Parameters:  map[string]any{"type": "object"},
	}
	one := estimateToolDefTokens([]ai.ToolDefinition{def})
	// The 400-byte description alone is ~100 tokens under the bytes/4
	// heuristic; the JSON envelope adds more.
	if one < 100 {
		t.Fatalf("estimateToolDefTokens = %d, want >= 100", one)
	}
	two := estimateToolDefTokens([]ai.ToolDefinition{def, def})
	if two != 2*one {
		t.Fatalf("two defs = %d, want %d", two, 2*one)
	}
}
