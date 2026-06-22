package cli

import "testing"

func TestFormatContextSize(t *testing.T) {
	tests := []struct {
		name   string
		tokens int
		budget int
		want   string
	}{
		{name: "typical", tokens: 26000, budget: 180000, want: "~26k tokens, 14% of 180k"},
		{name: "sub-1000 raw", tokens: 512, budget: 180000, want: "~512 tokens, 0% of 180k"},
		{name: "percent rounds to nearest", tokens: 27000, budget: 180000, want: "~27k tokens, 15% of 180k"},
		{name: "zero tokens no panic", tokens: 0, budget: 180000, want: "~0 tokens, 0% of 180k"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatContextSize(tt.tokens, tt.budget); got != tt.want {
				t.Fatalf("formatContextSize(%d, %d) = %q, want %q", tt.tokens, tt.budget, got, tt.want)
			}
		})
	}
}

func TestFormatContextSize_ZeroBudgetNoPanic(t *testing.T) {
	// budget 0 must not divide-by-zero; percentage falls back to 0.
	got := formatContextSize(26000, 0)
	if got != "~26k tokens, 0% of 0k" {
		t.Fatalf("formatContextSize(26000, 0) = %q", got)
	}
}
