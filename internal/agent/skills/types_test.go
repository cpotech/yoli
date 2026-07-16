package skills

import "testing"

func TestLoadedSkill_TriggerAccessor(t *testing.T) {
	cases := []struct {
		name string
		fm   map[string]any
		want string
	}{
		{"string", map[string]any{"trigger": "when planning"}, "when planning"},
		{"missing", map[string]any{}, ""},
		{"nil frontmatter", nil, ""},
		{"non-string", map[string]any{"trigger": 7}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := LoadedSkill{Frontmatter: tc.fm}
			if got := s.Trigger(); got != tc.want {
				t.Fatalf("Trigger() = %q, want %q", got, tc.want)
			}
		})
	}
}
