package cli

import (
	"fmt"
	"math"
)

// formatContextSize renders the human-readable context-size line value,
// e.g. "~26k tokens, 14% of 180k". Token counts under 1000 render raw to
// avoid a misleading "~0k", and a zero budget never divides by zero.
func formatContextSize(tokens, budget int) string {
	tok := fmt.Sprintf("~%d tokens", tokens)
	if tokens >= 1000 {
		tok = fmt.Sprintf("~%dk tokens", int(math.Round(float64(tokens)/1000)))
	}
	pct := 0
	if budget > 0 {
		pct = int(math.Round(float64(tokens) / float64(budget) * 100))
	}
	return fmt.Sprintf("%s, %d%% of %dk", tok, pct, budget/1000)
}
