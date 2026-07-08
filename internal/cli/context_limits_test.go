package cli

import (
	"bytes"
	"strings"
	"testing"

	"yoli/internal/agent"
)

func TestResolveContextLimits_DefaultsWhenUnset(t *testing.T) {
	t.Setenv("YOLI_CONTEXT_WINDOW", "")
	t.Setenv("YOLI_MAX_TOKENS", "")
	var warn bytes.Buffer
	window, maxTokens := resolveContextLimits(&warn)
	if window != agent.DefaultContextBudget {
		t.Fatalf("window = %d, want %d", window, agent.DefaultContextBudget)
	}
	if maxTokens != agent.DefaultMaxOutputTokens {
		t.Fatalf("maxTokens = %d, want %d", maxTokens, agent.DefaultMaxOutputTokens)
	}
	if warn.Len() != 0 {
		t.Fatalf("unexpected warning: %q", warn.String())
	}
}

func TestResolveContextLimits_ReadsEnv(t *testing.T) {
	t.Setenv("YOLI_CONTEXT_WINDOW", "32768")
	t.Setenv("YOLI_MAX_TOKENS", "4096")
	var warn bytes.Buffer
	window, maxTokens := resolveContextLimits(&warn)
	if window != 32768 {
		t.Fatalf("window = %d, want 32768", window)
	}
	if maxTokens != 4096 {
		t.Fatalf("maxTokens = %d, want 4096", maxTokens)
	}
	if warn.Len() != 0 {
		t.Fatalf("unexpected warning: %q", warn.String())
	}
}

func TestResolveContextLimits_InvalidWindowWarnsAndFallsBack(t *testing.T) {
	t.Setenv("YOLI_CONTEXT_WINDOW", "notanumber")
	t.Setenv("YOLI_MAX_TOKENS", "")
	var warn bytes.Buffer
	window, _ := resolveContextLimits(&warn)
	if window != agent.DefaultContextBudget {
		t.Fatalf("window = %d, want default %d", window, agent.DefaultContextBudget)
	}
	if !strings.Contains(warn.String(), "YOLI_CONTEXT_WINDOW") {
		t.Fatalf("warning did not mention YOLI_CONTEXT_WINDOW: %q", warn.String())
	}
}

func TestResolveContextLimits_NonPositiveWarnsAndFallsBack(t *testing.T) {
	t.Setenv("YOLI_CONTEXT_WINDOW", "0")
	t.Setenv("YOLI_MAX_TOKENS", "-1")
	var warn bytes.Buffer
	window, maxTokens := resolveContextLimits(&warn)
	if window != agent.DefaultContextBudget {
		t.Fatalf("window = %d, want default %d", window, agent.DefaultContextBudget)
	}
	if maxTokens != agent.DefaultMaxOutputTokens {
		t.Fatalf("maxTokens = %d, want default %d", maxTokens, agent.DefaultMaxOutputTokens)
	}
	out := warn.String()
	if !strings.Contains(out, "YOLI_CONTEXT_WINDOW") || !strings.Contains(out, "YOLI_MAX_TOKENS") {
		t.Fatalf("warnings missing keys: %q", out)
	}
}
