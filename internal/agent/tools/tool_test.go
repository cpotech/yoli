package tools

import "testing"

func TestDefaultTools_RegistersEditGlobGrep(t *testing.T) {
	got := DefaultTools(t.TempDir())
	names := map[string]bool{}
	for _, tl := range got {
		names[tl.Definition().Name] = true
	}
	want := []string{
		"Read", "Write", "LS", "Bash", "Edit", "Glob", "Grep", "WebSearch",
	}
	for _, w := range want {
		if !names[w] {
			t.Fatalf("missing tool %q in %v", w, names)
		}
	}
}
