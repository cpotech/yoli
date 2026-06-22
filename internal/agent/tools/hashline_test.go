package tools

import (
	"strings"
	"testing"
)

func TestHashLine_DeterministicAndPositionIndependent(t *testing.T) {
	h1 := HashLine("hello world")
	h2 := HashLine("hello world")
	if h1 != h2 {
		t.Fatalf("non-deterministic: %q vs %q", h1, h2)
	}
	if len(h1) != hashlineWidth {
		t.Fatalf("hash width = %d, want %d", len(h1), hashlineWidth)
	}
	if HashLine("a") == HashLine("b") {
		t.Fatalf("different content collided trivially")
	}
}

func TestAnnotateHashlines_PreservesContentAndTrailingNewline(t *testing.T) {
	in := "alpha\nbeta\ngamma\n"
	got := AnnotateHashlines(in)
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("trailing newline lost: %q", got)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %v", len(lines), lines)
	}
	for i, want := range []string{"alpha", "beta", "gamma"} {
		prefix := lines[i]
		// format: N:HHH|content
		if !strings.Contains(prefix, "|"+want) {
			t.Fatalf("line %d %q missing content %q", i+1, prefix, want)
		}
		if !strings.HasPrefix(prefix, intStr(i+1)+":") {
			t.Fatalf("line %d %q missing 1-based number", i+1, prefix)
		}
	}
}

func TestAnnotateHashlines_NoTrailingNewline(t *testing.T) {
	got := AnnotateHashlines("alpha\nbeta")
	if strings.HasSuffix(got, "\n") {
		t.Fatalf("unwanted trailing newline: %q", got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("expected 1 internal newline, got %q", got)
	}
}

func TestAnnotateHashlines_EmptyFile(t *testing.T) {
	if got := AnnotateHashlines(""); got != "" {
		t.Fatalf("empty file annotation = %q, want \"\"", got)
	}
}

func TestAnnotateHashlines_SingleNewline(t *testing.T) {
	got := AnnotateHashlines("\n")
	// one empty line, hashed, with trailing newline preserved
	if !strings.HasPrefix(got, "1:") || !strings.Contains(got, "|") {
		t.Fatalf("single-newline file = %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("trailing newline lost: %q", got)
	}
}

func TestSplitJoinFileLines_RoundTrip(t *testing.T) {
	cases := []string{"", "\n", "a", "a\n", "a\nb", "a\nb\n", "\n\n"}
	for _, c := range cases {
		lines, trailing := splitFileLines(c)
		got := joinFileLines(lines, trailing)
		if got != c {
			t.Fatalf("roundtrip %q -> %q", c, got)
		}
	}
}

// intStr formats i as decimal without bringing strconv into the test
// file's import block.
func intStr(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
