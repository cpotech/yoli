package yolium

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestEmit_PrefixAndTrailingNewline(t *testing.T) {
	var out bytes.Buffer
	if err := Emit(&out, ProgressEvent{Step: "a", Detail: "b"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	data := out.String()
	if !strings.HasPrefix(data, "@@YOLIUM:") {
		t.Fatalf("missing prefix: %q", data)
	}
	if !strings.HasSuffix(data, "\n") {
		t.Fatalf("missing newline: %q", data)
	}
	if strings.Count(data, "\n") != 1 {
		t.Fatalf("want exactly one newline: %q", data)
	}
	var payload struct {
		Type   string `json:"type"`
		Step   string `json:"step"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(data[len("@@YOLIUM:"):len(data)-1]), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload != (struct {
		Type   string `json:"type"`
		Step   string `json:"step"`
		Detail string `json:"detail"`
	}{Type: "progress", Step: "a", Detail: "b"}) {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestEmit_CompactJSON(t *testing.T) {
	var out bytes.Buffer
	if err := Emit(&out, ProgressEvent{Step: "s", Detail: "d"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	want := `@@YOLIUM:{"type":"progress","step":"s","detail":"d"}` + "\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestEmit_NoControlCharsBeyondTrailingNewline(t *testing.T) {
	var out bytes.Buffer
	if err := Emit(&out, CommentEvent{Text: "hi"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	data := out.String()
	ctrl := regexp.MustCompile(`[\x00-\x08\x0b-\x1f\x7f]`)
	if ctrl.MatchString(data) {
		t.Fatalf("found control chars: %q", data)
	}
	want := `@@YOLIUM:{"type":"comment","text":"hi"}` + "\n"
	if data != want {
		t.Fatalf("got %q, want %q", data, want)
	}
}

func intPtr(i int) *int { return &i }

func TestEmit_AllSevenEventKinds(t *testing.T) {
	type tc struct {
		event Event
		want  string
	}
	cases := []tc{
		{ProgressEvent{Step: "analyze", Detail: "d"},
			`{"type":"progress","step":"analyze","detail":"d"}`},
		{ProgressEvent{Step: "analyze", Detail: "d", Attempt: intPtr(1), MaxAttempts: intPtr(3)},
			`{"type":"progress","step":"analyze","detail":"d","attempt":1,"maxAttempts":3}`},
		{CommentEvent{Text: "c"},
			`{"type":"comment","text":"c"}`},
		{UpdateDescriptionEvent{Description: "body"},
			`{"type":"update_description","description":"body"}`},
		{SetTestSpecsEvent{Specs: []TestSpecGroup{{File: "a", Specs: []string{"x"}}}},
			`{"type":"set_test_specs","specs":[{"file":"a","specs":["x"]}]}`},
		{AskQuestionEvent{Text: "q?"},
			`{"type":"ask_question","text":"q?"}`},
		{AskQuestionEvent{Text: "q?", Options: []string{"a", "b"}},
			`{"type":"ask_question","text":"q?","options":["a","b"]}`},
		{CompleteEvent{Summary: "s"},
			`{"type":"complete","summary":"s"}`},
		{ErrorEvent{Message: "m"},
			`{"type":"error","message":"m"}`},
		{AssistantTurnEvent{Turn: 2, Text: "thinking..."},
			`{"type":"assistant_turn","turn":2,"text":"thinking..."}`},
	}
	for _, c := range cases {
		var out bytes.Buffer
		if err := Emit(&out, c.event); err != nil {
			t.Fatalf("emit %T: %v", c.event, err)
		}
		want := "@@YOLIUM:" + c.want + "\n"
		if out.String() != want {
			t.Fatalf("%T:\n got  %q\n want %q", c.event, out.String(), want)
		}
	}
}

func TestNewExitSignal_StartsNil(t *testing.T) {
	if s := NewExitSignal(); s.Pending != nil {
		t.Fatalf("pending = %+v, want nil", s.Pending)
	}
}
