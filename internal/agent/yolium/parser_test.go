package yolium

import "testing"

func TestScanText_NoEvents(t *testing.T) {
	if got := ScanText(""); got != nil {
		t.Fatalf("empty input: got %v want nil", got)
	}
	if got := ScanText("hello world\nno markers here\n"); got != nil {
		t.Fatalf("plain text: got %v want nil", got)
	}
}

func TestScanText_SingleProgress(t *testing.T) {
	in := `@@YOLIUM:{"type":"progress","step":"clarify","detail":"reading files"}`
	got := ScanText(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	p, ok := got[0].(ProgressEvent)
	if !ok {
		t.Fatalf("expected ProgressEvent, got %T", got[0])
	}
	if p.Step != "clarify" || p.Detail != "reading files" {
		t.Fatalf("unexpected fields: %+v", p)
	}
}

func TestScanText_MultipleEvents(t *testing.T) {
	in := "thinking...\n" +
		`@@YOLIUM:{"type":"progress","step":"plan","detail":"a"}` + "\n" +
		"more prose\n" +
		`@@YOLIUM:{"type":"comment","text":"hi"}` + "\n" +
		`@@YOLIUM:{"type":"complete","summary":"done"}` + "\n"
	got := ScanText(in)
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d (%v)", len(got), got)
	}
	if _, ok := got[0].(ProgressEvent); !ok {
		t.Fatalf("event 0: want ProgressEvent, got %T", got[0])
	}
	if _, ok := got[1].(CommentEvent); !ok {
		t.Fatalf("event 1: want CommentEvent, got %T", got[1])
	}
	if c, ok := got[2].(CompleteEvent); !ok || c.Summary != "done" {
		t.Fatalf("event 2: want CompleteEvent{Summary:done}, got %T %+v", got[2], got[2])
	}
}

func TestScanText_SkipsMalformedJSON(t *testing.T) {
	in := `@@YOLIUM:{"type":"progress","step":"a"` + "\n" + // missing brace
		`@@YOLIUM:not-json` + "\n" +
		`@@YOLIUM:{"type":"complete","summary":"ok"}` + "\n"
	got := ScanText(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 event (only complete), got %d (%v)", len(got), got)
	}
	if _, ok := got[0].(CompleteEvent); !ok {
		t.Fatalf("want CompleteEvent, got %T", got[0])
	}
}

func TestScanText_SkipsUnknownType(t *testing.T) {
	in := `@@YOLIUM:{"type":"mystery","foo":"bar"}` + "\n"
	if got := ScanText(in); len(got) != 0 {
		t.Fatalf("expected 0 events, got %d (%v)", len(got), got)
	}
}

func TestScanText_QuestionTypeAliases(t *testing.T) {
	in := `@@YOLIUM:{"type":"ask_question","text":"why?"}` + "\n" +
		`@@YOLIUM:{"type":"question","text":"how?","options":["a","b"]}` + "\n"
	got := ScanText(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	for i, e := range got {
		q, ok := e.(AskQuestionEvent)
		if !ok {
			t.Fatalf("event %d: want AskQuestionEvent, got %T", i, e)
		}
		if q.Type != "ask_question" {
			t.Fatalf("event %d: Type normalised expected ask_question, got %q", i, q.Type)
		}
	}
}

func TestScanText_TrailingWhitespaceAndCR(t *testing.T) {
	in := "@@YOLIUM:{\"type\":\"progress\",\"step\":\"a\",\"detail\":\"b\"}   \r\n"
	got := ScanText(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
}

func TestScanText_LineMustStartWithPrefix(t *testing.T) {
	// A prefix that appears mid-line should NOT be parsed (it's narration).
	in := "I will emit @@YOLIUM:{\"type\":\"complete\",\"summary\":\"x\"}\n"
	if got := ScanText(in); len(got) != 0 {
		t.Fatalf("expected 0 events for mid-line marker, got %d (%v)", len(got), got)
	}
}

func TestTerminalEvent(t *testing.T) {
	tests := []struct {
		name     string
		evt      Event
		wantKind string
		wantOK   bool
	}{
		{"progress is non-terminal", ProgressEvent{Step: "a"}, "", false},
		{"comment is non-terminal", CommentEvent{Text: "hi"}, "", false},
		{"complete is terminal", CompleteEvent{Summary: "done"}, "complete", true},
		{"error is terminal", ErrorEvent{Message: "boom"}, "error", true},
		{"question is terminal", AskQuestionEvent{Text: "?"}, "question", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, _, _, ok := TerminalEvent(tc.evt)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v", ok, tc.wantOK)
			}
			if kind != tc.wantKind {
				t.Fatalf("kind=%q want %q", kind, tc.wantKind)
			}
		})
	}
}
