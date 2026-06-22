package agent

import "testing"

func TestContainsYoliumProtocolText(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "empty string",
			content: "",
			want:    false,
		},
		{
			name:    "no marker",
			content: "Just some assistant content with no protocol mention.",
			want:    false,
		},
		{
			name:    "bare complete marker",
			content: `@@YOLIUM:{"type":"complete","summary":"done"}`,
			want:    true,
		},
		{
			name:    "marker embedded in prose",
			content: "I will now emit @@YOLIUM:{\"type\":\"progress\",\"step\":\"x\"} as text.",
			want:    true,
		},
		{
			name:    "code-fenced marker",
			content: "```\n@@YOLIUM:{\"type\":\"complete\"}\n```",
			want:    true,
		},
		{
			name:    "lowercase variant is not the marker",
			content: "@@yolium:{\"type\":\"complete\"}",
			want:    false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := containsYoliumProtocolText(c.content)
			if got != c.want {
				t.Errorf("containsYoliumProtocolText(%q) = %v; want %v", c.content, got, c.want)
			}
		})
	}
}
