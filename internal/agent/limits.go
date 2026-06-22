package agent

import (
	"fmt"

	"yoli/internal/ai"
)

const (
	// DefaultMaxOutputTokens caps the per-turn output from the provider
	// when RunOptions.MaxTokens is unset. 4096 turned out to be too
	// tight for tool-call workflows: a model writing a 7 KB source
	// file via the Write tool needs to emit JSON-encoded content that
	// easily clears 4 K output tokens (escaped newlines and quotes
	// inflate the byte count). When the cap was hit mid-tool-call, the
	// arguments field arrived truncated, every downstream provider
	// that validated `arguments` as JSON (e.g. SiliconFlow on
	// OpenRouter) returned 400 on the next round-trip, and the run
	// died. 8192 matches Anthropic's Sonnet default and is supported
	// by every OpenAI-compatible provider we route through.
	DefaultMaxOutputTokens = 8192
	DefaultContextBudget   = 180_000
	DefaultToolOutputBytes = 65_536
)

const messageOverheadTokens = 4

func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}

func estimateMessageTokens(m ai.Message) int {
	total := messageOverheadTokens
	total += estimateTokens(string(m.Role))
	if m.Content != nil {
		total += estimateTokens(*m.Content)
	}
	total += estimateTokens(m.ToolCallID)
	for _, call := range m.ToolCalls {
		total += estimateTokens(call.ID)
		total += estimateTokens(call.Name)
		total += estimateTokens(call.Arguments)
	}
	return total
}

// EstimateContextTokens returns the rough token count of a conversation,
// using the same heuristic the loop applies when deciding to compact.
func EstimateContextTokens(conv []ai.Message) int { return estimateConversationTokens(conv) }

func estimateConversationTokens(conv []ai.Message) int {
	total := 0
	for _, m := range conv {
		total += estimateMessageTokens(m)
	}
	return total
}

func truncateString(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	if maxBytes <= 0 {
		return fmt.Sprintf("[truncated: %d bytes elided]", len(s))
	}
	prefixLen := maxBytes
	for {
		if prefixLen < 0 {
			prefixLen = 0
		}
		elided := len(s) - prefixLen
		marker := fmt.Sprintf("\n[truncated: %d bytes elided]", elided)
		if prefixLen == 0 && len(marker) > maxBytes {
			return marker[1:]
		}
		if prefixLen+len(marker) <= maxBytes {
			return s[:prefixLen] + marker
		}
		prefixLen--
	}
}

func truncationMarker(originalBytes int) string {
	return fmt.Sprintf("[truncated: %d bytes elided]", originalBytes)
}
