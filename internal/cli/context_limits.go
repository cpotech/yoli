package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"yoli/internal/agent"
)

// resolveContextLimits reads the YOLI_CONTEXT_WINDOW and YOLI_MAX_TOKENS
// settings from the environment (config files are folded into the
// environment earlier via ApplyEnvDefaults) and returns the total context
// window and per-turn output cap to pass to agent.Run. Unset values use
// the agent defaults; a value that fails to parse or is non-positive emits
// a one-line warning to warnings and falls back to the default.
func resolveContextLimits(warnings io.Writer) (window, maxTokens int) {
	if warnings == nil {
		warnings = os.Stderr
	}
	window = resolvePositiveInt("YOLI_CONTEXT_WINDOW", agent.DefaultContextBudget, warnings)
	maxTokens = resolvePositiveInt("YOLI_MAX_TOKENS", agent.DefaultMaxOutputTokens, warnings)
	return window, maxTokens
}

func resolvePositiveInt(key string, fallback int, warnings io.Writer) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		fmt.Fprintf(warnings, "warning: invalid %s=%q — using default %d\n", key, raw, fallback)
		return fallback
	}
	return n
}
