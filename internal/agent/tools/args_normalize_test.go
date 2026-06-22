package tools

import (
	"encoding/json"
	"testing"
)

func TestNormalizeArgKeys_CamelToSnake(t *testing.T) {
	in := json.RawMessage(`{"oldString":"a","newString":"b","replaceAll":true}`)
	out := NormalizeArgKeys(in)

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["old_string"] != "a" {
		t.Errorf("expected old_string=a, got %v", got["old_string"])
	}
	if got["new_string"] != "b" {
		t.Errorf("expected new_string=b, got %v", got["new_string"])
	}
	if got["replace_all"] != true {
		t.Errorf("expected replace_all=true, got %v", got["replace_all"])
	}
	if _, ok := got["oldString"]; ok {
		t.Errorf("camelCase oldString should be gone from normalised payload")
	}
}

func TestNormalizeArgKeys_SnakeWinsOnCollision(t *testing.T) {
	// When both camelCase and snake_case forms appear, the snake_case
	// form (canonical schema name) wins.
	in := json.RawMessage(`{"old_string":"keep","oldString":"drop"}`)
	out := NormalizeArgKeys(in)

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["old_string"] != "keep" {
		t.Errorf("expected snake_case to win; got %v", got["old_string"])
	}
}

func TestNormalizeArgKeys_NoUppercasePassesThrough(t *testing.T) {
	in := json.RawMessage(`{"old_string":"a","new_string":"b"}`)
	out := NormalizeArgKeys(in)
	// Fast path returns the input unchanged.
	if string(out) != string(in) {
		t.Errorf("expected passthrough for all-snake input; got %q", string(out))
	}
}

func TestNormalizeArgKeys_NonObjectPassesThrough(t *testing.T) {
	// Arrays and scalars aren't objects; the normaliser leaves them alone.
	cases := []string{
		`[1,2,3]`,
		`"plain string"`,
		`42`,
		`null`,
		`not json at all`,
	}
	for _, in := range cases {
		out := NormalizeArgKeys(json.RawMessage(in))
		if string(out) != in {
			t.Errorf("expected passthrough for %q; got %q", in, string(out))
		}
	}
}

func TestNormalizeArgKeys_EmptyObject(t *testing.T) {
	in := json.RawMessage(`{}`)
	out := NormalizeArgKeys(in)
	if string(out) != string(in) {
		t.Errorf("expected passthrough for empty object; got %q", string(out))
	}
}

func TestNormalizeArgKeys_MixedKeys(t *testing.T) {
	// Snake keys preserved, camel keys translated.
	in := json.RawMessage(`{"file_path":"/x","oldString":"a","newString":"b","withHashes":true,"output_mode":"content"}`)
	out := NormalizeArgKeys(in)

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	expect := map[string]any{
		"file_path":   "/x",
		"old_string":  "a",
		"new_string":  "b",
		"with_hashes": true,
		"output_mode": "content",
	}
	for k, v := range expect {
		if got[k] != v {
			t.Errorf("expected %s=%v, got %v", k, v, got[k])
		}
	}
	for _, k := range []string{"oldString", "newString", "withHashes"} {
		if _, ok := got[k]; ok {
			t.Errorf("camelCase key %s should be gone", k)
		}
	}
}

func TestCamelToSnake(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"oldString", "old_string"},
		{"newString", "new_string"},
		{"replaceAll", "replace_all"},
		{"withHashes", "with_hashes"},
		{"outputMode", "output_mode"},
		{"headLimit", "head_limit"},
		{"already_snake", "already_snake"},
		{"a", "a"},
		{"", ""},
		// Mixed: leading underscore preserved, internal camelCase split.
		{"foo_BarBaz", "foo_bar_baz"},
	}
	for _, c := range cases {
		got := camelToSnake(c.in)
		if got != c.want {
			t.Errorf("camelToSnake(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
