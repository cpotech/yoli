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
