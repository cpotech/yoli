package yolium

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// captureSink records every Emit() call for assertion.
type captureSink struct {
	events []Event
}

func (c *captureSink) Emit(e Event) error {
	c.events = append(c.events, e)
	return nil
}

func findTool(t *testing.T, name string, sink EventSink, exit *ExitSignal) *yoliumTool {
	t.Helper()
	for _, tool := range NewTools(sink, exit) {
		yt, ok := tool.(*yoliumTool)
		if !ok {
			continue
		}
		if yt.name == name {
			return yt
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func TestNewTools_RegistersExpectedNames(t *testing.T) {
	tools := NewTools(NopSink(), NewExitSignal())
	want := []string{
		ToolComplete, ToolError, ToolAskQuestion,
		ToolProgress, ToolAddComment, ToolComment,
		ToolCreateItem, ToolUpdateDescription, ToolSetTestSpecs,
		ToolAction, ToolStartAgent,
	}
	got := make(map[string]bool, len(tools))
	for _, tl := range tools {
		got[tl.Definition().Name] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing tool %q in registered set", w)
		}
	}
	if len(tools) != len(want) {
		t.Errorf("len(NewTools)=%d want %d", len(tools), len(want))
	}
}

func TestCompleteTool_SetsExitAndEmits(t *testing.T) {
	sink := &captureSink{}
	exit := NewExitSignal()
	tool := findTool(t, ToolComplete, sink, exit)

	out, err := tool.Run(context.Background(), json.RawMessage(`{"summary":"done"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty result")
	}
	if exit.Pending == nil || exit.Pending.Kind != ExitPendingComplete {
		t.Fatalf("exit.Pending=%+v want kind=complete", exit.Pending)
	}
	if exit.Pending.Summary != "done" {
		t.Fatalf("summary=%q", exit.Pending.Summary)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events=%d want 1", len(sink.events))
	}
	ce, ok := sink.events[0].(CompleteEvent)
	if !ok {
		t.Fatalf("event type=%T", sink.events[0])
	}
	if ce.Summary != "done" {
		t.Fatalf("event.Summary=%q", ce.Summary)
	}
}

func TestCompleteTool_RejectsMissingSummary(t *testing.T) {
	tool := findTool(t, ToolComplete, &captureSink{}, NewExitSignal())
	if _, err := tool.Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for missing summary")
	}
}

func TestCompleteTool_VerdictPropagates(t *testing.T) {
	sink := &captureSink{}
	tool := findTool(t, ToolComplete, sink, NewExitSignal())
	_, err := tool.Run(context.Background(), json.RawMessage(`{"summary":"ok","verdict":"approved"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	ce := sink.events[0].(CompleteEvent)
	if ce.Verdict != "approved" {
		t.Fatalf("verdict=%q", ce.Verdict)
	}
}

func TestErrorTool_SetsExitAndEmits(t *testing.T) {
	sink := &captureSink{}
	exit := NewExitSignal()
	tool := findTool(t, ToolError, sink, exit)

	if _, err := tool.Run(context.Background(), json.RawMessage(`{"message":"boom"}`)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit.Pending == nil || exit.Pending.Kind != ExitPendingError {
		t.Fatalf("exit.Pending=%+v", exit.Pending)
	}
	if exit.Pending.Message != "boom" {
		t.Fatalf("message=%q", exit.Pending.Message)
	}
	if _, ok := sink.events[0].(ErrorEvent); !ok {
		t.Fatalf("event type=%T", sink.events[0])
	}
}

func TestAskQuestionTool_SetsExitToQuestion(t *testing.T) {
	sink := &captureSink{}
	exit := NewExitSignal()
	tool := findTool(t, ToolAskQuestion, sink, exit)

	_, err := tool.Run(context.Background(), json.RawMessage(`{"text":"which?","options":["a","b"]}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit.Pending == nil || exit.Pending.Kind != ExitPendingQuestion {
		t.Fatalf("exit.Pending=%+v", exit.Pending)
	}
	q := sink.events[0].(AskQuestionEvent)
	if q.Text != "which?" || len(q.Options) != 2 {
		t.Fatalf("event=%+v", q)
	}
}

func TestProgressTool_EmitsWithoutSettingExit(t *testing.T) {
	sink := &captureSink{}
	exit := NewExitSignal()
	tool := findTool(t, ToolProgress, sink, exit)

	if _, err := tool.Run(context.Background(), json.RawMessage(`{"step":"model","detail":"openrouter/free"}`)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit.Pending != nil {
		t.Fatalf("non-terminator must not set exit; got %+v", exit.Pending)
	}
	pe := sink.events[0].(ProgressEvent)
	if pe.Step != "model" || pe.Detail != "openrouter/free" {
		t.Fatalf("event=%+v", pe)
	}
}

func TestAddCommentTool_AndAliasComment_BothWork(t *testing.T) {
	for _, name := range []string{ToolAddComment, ToolComment} {
		sink := &captureSink{}
		tool := findTool(t, name, sink, NewExitSignal())
		if _, err := tool.Run(context.Background(), json.RawMessage(`{"text":"hi"}`)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, ok := sink.events[0].(CommentEvent); !ok {
			t.Fatalf("%s: event type %T", name, sink.events[0])
		}
	}
}

func TestCreateItemTool_RoundTripsAllFields(t *testing.T) {
	sink := &captureSink{}
	tool := findTool(t, ToolCreateItem, sink, NewExitSignal())
	raw := `{"title":"T","description":"D","branch":"b","agentProvider":"yoli","order":3,"model":"openrouter/free","isFeatureParent":true,"parentId":"p","parentBranch":"pb"}`
	if _, err := tool.Run(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ev := sink.events[0].(CreateItemEvent)
	if ev.Title != "T" || ev.Description != "D" || ev.Branch != "b" || ev.AgentProvider != "yoli" ||
		ev.Order != 3 || ev.Model != "openrouter/free" || !ev.IsFeatureParent || ev.ParentID != "p" || ev.ParentBranch != "pb" {
		t.Fatalf("event=%+v", ev)
	}
}

func TestUpdateDescriptionTool_Emits(t *testing.T) {
	sink := &captureSink{}
	tool := findTool(t, ToolUpdateDescription, sink, NewExitSignal())
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"description":"x"}`)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if e, ok := sink.events[0].(UpdateDescriptionEvent); !ok || e.Description != "x" {
		t.Fatalf("event=%+v ok=%v", sink.events[0], ok)
	}
}

func TestSetTestSpecsTool_RequiresNonEmpty(t *testing.T) {
	sink := &captureSink{}
	tool := findTool(t, ToolSetTestSpecs, sink, NewExitSignal())
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"specs":[]}`)); err == nil {
		t.Fatal("expected error for empty specs")
	}
	raw := `{"specs":[{"file":"a.go","description":"x","specs":["s1","s2"]}]}`
	if _, err := tool.Run(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if e, ok := sink.events[0].(SetTestSpecsEvent); !ok || len(e.Specs) != 1 || e.Specs[0].File != "a.go" {
		t.Fatalf("event=%+v ok=%v", sink.events[0], ok)
	}
}

func TestActionTool_AcceptsArbitraryData(t *testing.T) {
	sink := &captureSink{}
	tool := findTool(t, ToolAction, sink, NewExitSignal())
	raw := `{"action":"foo","data":{"k":"v","n":7}}`
	if _, err := tool.Run(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ev := sink.events[0].(ActionEvent)
	if ev.Action != "foo" || ev.Data["k"] != "v" {
		t.Fatalf("event=%+v", ev)
	}
}

func TestStartAgentTool_RequiresIDAndName(t *testing.T) {
	sink := &captureSink{}
	tool := findTool(t, ToolStartAgent, sink, NewExitSignal())
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"itemId":"i"}`)); err == nil {
		t.Fatal("expected error for missing agentName")
	}
	raw := `{"itemId":"i","agentName":"code-agent","goal":"g","agentProvider":"yoli"}`
	if _, err := tool.Run(context.Background(), json.RawMessage(raw)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ev := sink.events[0].(StartAgentEvent)
	if ev.ItemID != "i" || ev.AgentName != "code-agent" || ev.Goal != "g" || ev.AgentProvider != "yoli" {
		t.Fatalf("event=%+v", ev)
	}
}

func TestNDJSONSink_OneLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	sink := NewNDJSONSink(&buf)
	if err := sink.Emit(ProgressEvent{Step: "s", Detail: "d"}); err != nil {
		t.Fatalf("emit progress: %v", err)
	}
	if err := sink.Emit(CompleteEvent{Summary: "ok"}); err != nil {
		t.Fatalf("emit complete: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines=%d want 2; output=%q", len(lines), buf.String())
	}
	for i, line := range lines {
		var probe map[string]any
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Fatalf("line %d not JSON: %q (%v)", i, line, err)
		}
	}
}
