package agent

import (
	"encoding/json"
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
	// DefaultContextBudget is the fallback TOTAL context window (input +
	// output) when neither RunOptions.ContextBudgetTokens nor
	// YOLI_CONTEXT_WINDOW is set. The loop reserves output headroom out of
	// this window before compacting input (see computeInputBudget), so the
	// requested input + output can never exceed it.
	DefaultContextBudget   = 180_000
	DefaultToolOutputBytes = 65_536
)

const messageOverheadTokens = 4

const (
	// minEstimateScale / maxObservedScale bound the estimate-correction
	// scale learned from provider usage reports (see observedEstimateScale).
	minEstimateScale = 1.0
	maxObservedScale = 4.0
	// maxOverflowScale bounds the harder shrink applied when the provider
	// rejects a request outright for context overflow. Overflow is direct
	// evidence the estimate is wrong, so it may push past the observed
	// clamp.
	maxOverflowScale = 8.0
	// overflowScaleStep multiplies the estimate scale on each overflow
	// retry.
	overflowScaleStep = 1.5
)

func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}

// observedEstimateScale returns the provider-reported prompt-token count
// divided by our own estimate for the same request, clamped to
// [minEstimateScale, maxObservedScale]. The bytes/4 heuristic undercounts
// dense content — hex-hash paths tokenize at ~2 bytes/token — and an
// uncorrected 2x error walks the request straight past the server's
// window. Ratios below 1 are clamped up: shrinking the input budget is
// the only safe direction, and computeInputBudget's 90% margin already
// covers mild over-estimation.
func observedEstimateScale(actual, estimated int) float64 {
	if actual <= 0 || estimated <= 0 {
		return minEstimateScale
	}
	r := float64(actual) / float64(estimated)
	if r < minEstimateScale {
		return minEstimateScale
	}
	if r > maxObservedScale {
		return maxObservedScale
	}
	return r
}

// scaleBudget shrinks an input token budget by the current estimate scale.
func scaleBudget(budget int, scale float64) int {
	if scale <= minEstimateScale {
		return budget
	}
	return int(float64(budget) / scale)
}

// estimateToolDefTokens approximates the token cost of the tool
// definitions sent as ChatRequest.Tools. That JSON is never part of the
// conversation messages, so estimateConversationTokens misses it; the
// loop must account for it when reserving input headroom or a large tool
// schema can silently push the request over the window.
func estimateToolDefTokens(defs []ai.ToolDefinition) int {
	total := 0
	for _, d := range defs {
		b, err := json.Marshal(d)
		if err != nil {
			continue
		}
		total += estimateTokens(string(b))
	}
	return total
}

// computeInputBudget derives the maximum input (prompt) token budget from
// the total context window, reserving room for the requested output and
// the tool-definition JSON. reserved output is clamped to half the window
// so tiny windows (as used by tests) keep usable input room; the result
// is scaled to 90% to absorb the bytes/4 estimation error and floored at
// window/10 so oversized tool schemas can never zero or negate it.
func computeInputBudget(window, maxTokens, toolDefTokens int) int {
	reserved := maxTokens
	if half := window / 2; reserved > half {
		reserved = half
	}
	budget := (window - reserved - toolDefTokens) * 9 / 10
	if floor := window / 10; budget < floor {
		budget = floor
	}
	return budget
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
