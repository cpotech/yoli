// Package providers contains concrete ai.Provider implementations.
package providers

import (
	"context"
	"fmt"

	"yoli/internal/ai"
)

// ScriptExhaustedError is returned when FauxProvider.Chat is called more
// times than the script has turns.
type ScriptExhaustedError struct {
	ScriptLength int
}

func (e *ScriptExhaustedError) Error() string {
	return fmt.Sprintf("FauxProvider script exhausted after %d turn(s)", e.ScriptLength)
}

// FauxProvider replays a fixed list of ai.ChatResponse values in order.
// Useful for tests that need deterministic provider behaviour.
type FauxProvider struct {
	script []ai.ChatResponse
	cursor int
}

// NewFauxProvider returns a FauxProvider that will replay the given script
// across successive Chat calls.
func NewFauxProvider(script []ai.ChatResponse) *FauxProvider {
	return &FauxProvider{script: script}
}

// Chat returns the next scripted response, ignoring the request entirely.
// Returns *ScriptExhaustedError once the script is exhausted.
func (p *FauxProvider) Chat(_ context.Context, _ ai.ChatRequest) (ai.ChatResponse, error) {
	if p.cursor >= len(p.script) {
		return ai.ChatResponse{}, &ScriptExhaustedError{ScriptLength: len(p.script)}
	}
	turn := p.script[p.cursor]
	p.cursor++
	return turn, nil
}

// Compile-time check that FauxProvider satisfies ai.Provider.
var _ ai.Provider = (*FauxProvider)(nil)
