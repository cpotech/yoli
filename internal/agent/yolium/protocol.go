// Package yolium defines the Yoli/yolium stdio protocol: structured
// "@@YOLIUM:" event lines emitted by the agent and single-line answers
// read back from stdin. It also exposes the per-event Go types and a
// small shared ExitSignal for tools that want to stop the agent loop.
package yolium

import (
	"encoding/json"
	"fmt"
	"io"
)

// Prefix is the literal byte sequence every protocol line begins with.
const Prefix = "@@YOLIUM:"

// TestSpecGroup is a per-file list of test specifications associated
// with a planned work item.
type TestSpecGroup struct {
	File  string   `json:"file"`
	Specs []string `json:"specs"`
}

// Event is implemented by every concrete event variant. The eventTag
// method is unexported so external packages can't add new variants.
type Event interface {
	eventTag()
}

// ProgressEvent reports incremental progress on a step without pausing.
type ProgressEvent struct {
	Type        string `json:"type"`
	Step        string `json:"step"`
	Detail      string `json:"detail"`
	Attempt     *int   `json:"attempt,omitempty"`
	MaxAttempts *int   `json:"maxAttempts,omitempty"`
}

func (ProgressEvent) eventTag() {}

// CommentEvent posts a comment to the work-item thread.
type CommentEvent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (CommentEvent) eventTag() {}

// UpdateDescriptionEvent replaces the work-item description body.
type UpdateDescriptionEvent struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

func (UpdateDescriptionEvent) eventTag() {}

// SetTestSpecsEvent attaches the test specifications for downstream
// agents.
type SetTestSpecsEvent struct {
	Type  string          `json:"type"`
	Specs []TestSpecGroup `json:"specs"`
}

func (SetTestSpecsEvent) eventTag() {}

// AskQuestionEvent pauses the agent and asks the user a question.
type AskQuestionEvent struct {
	Type    string   `json:"type"`
	Text    string   `json:"text"`
	Options []string `json:"options,omitempty"`
}

func (AskQuestionEvent) eventTag() {}

// CompleteEvent signals that the work item is done. Verdict is
// populated only by verify-agent runs (values: "approved", "rejected",
// "needs_revision"); other agents omit it.
type CompleteEvent struct {
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Verdict string `json:"verdict,omitempty"`
}

func (CompleteEvent) eventTag() {}

// CreateItemEvent inserts a new work item on the kanban board. Most
// fields are optional and default at the consumer (Yolium) side.
type CreateItemEvent struct {
	Type            string `json:"type"`
	Title           string `json:"title"`
	Description     string `json:"description,omitempty"`
	Branch          string `json:"branch,omitempty"`
	AgentProvider   string `json:"agentProvider,omitempty"`
	Order           int    `json:"order,omitempty"`
	Model           string `json:"model,omitempty"`
	IsFeatureParent bool   `json:"isFeatureParent,omitempty"`
	ParentID        string `json:"parentId,omitempty"`
	ParentBranch    string `json:"parentBranch,omitempty"`
}

func (CreateItemEvent) eventTag() {}

// ActionEvent records a generic action with arbitrary payload data.
type ActionEvent struct {
	Type      string         `json:"type"`
	Action    string         `json:"action"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
}

func (ActionEvent) eventTag() {}

// StartAgentEvent requests that Yolium spawn an additional headless
// agent against an existing work item.
type StartAgentEvent struct {
	Type          string `json:"type"`
	ItemID        string `json:"itemId"`
	AgentName     string `json:"agentName"`
	Goal          string `json:"goal,omitempty"`
	AgentProvider string `json:"agentProvider,omitempty"`
}

func (StartAgentEvent) eventTag() {}

// ErrorEvent signals an unrecoverable failure.
type ErrorEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (ErrorEvent) eventTag() {}

// AssistantTurnEvent carries the raw assistant text for one turn. It is
// emitted only on the structured EventSink (NDJSON / fd-3) under
// --yolium-mode so that Yolium can populate its agentMessageTexts list
// without re-parsing yoli stderr. Plain `yoli agent` runs never emit
// this event.
type AssistantTurnEvent struct {
	Type string `json:"type"`
	Turn int    `json:"turn"`
	Text string `json:"text"`
}

func (AssistantTurnEvent) eventTag() {}

// Emit serialises event as compact JSON and writes a single
// "@@YOLIUM:<json>\n" line to out. The Type field on event is set
// automatically based on the variant; any prior value is ignored.
func Emit(out io.Writer, event Event) error {
	payload, err := marshalEvent(event)
	if err != nil {
		return err
	}
	if _, err := out.Write([]byte(Prefix)); err != nil {
		return err
	}
	if _, err := out.Write(payload); err != nil {
		return err
	}
	if _, err := out.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}

func marshalEvent(event Event) ([]byte, error) {
	switch e := event.(type) {
	case ProgressEvent:
		e.Type = "progress"
		return json.Marshal(e)
	case CommentEvent:
		e.Type = "comment"
		return json.Marshal(e)
	case UpdateDescriptionEvent:
		e.Type = "update_description"
		return json.Marshal(e)
	case SetTestSpecsEvent:
		e.Type = "set_test_specs"
		if e.Specs == nil {
			e.Specs = []TestSpecGroup{}
		}
		return json.Marshal(e)
	case AskQuestionEvent:
		e.Type = "ask_question"
		return json.Marshal(e)
	case CompleteEvent:
		e.Type = "complete"
		return json.Marshal(e)
	case ErrorEvent:
		e.Type = "error"
		return json.Marshal(e)
	case AssistantTurnEvent:
		e.Type = "assistant_turn"
		return json.Marshal(e)
	case CreateItemEvent:
		e.Type = "create_item"
		return json.Marshal(e)
	case ActionEvent:
		e.Type = "action"
		return json.Marshal(e)
	case StartAgentEvent:
		e.Type = "start_agent"
		return json.Marshal(e)
	default:
		return nil, fmt.Errorf("yolium: unknown event type %T", event)
	}
}

// ExitPendingKind discriminates an ExitPending.
type ExitPendingKind string

const (
	ExitPendingComplete ExitPendingKind = "complete"
	ExitPendingError    ExitPendingKind = "error"
	// ExitPendingQuestion means the model emitted an @@YOLIUM:{type:
	// "ask_question",...} line in its prose. The agent loop should stop;
	// yolium will surface the question to the user and resume the agent
	// later with the answer as a fresh user prompt.
	ExitPendingQuestion ExitPendingKind = "question"
)

// ExitPending captures a deferred "stop the loop" intent emitted by the
// complete/error tools. The agent runner checks ExitSignal.Pending after
// each tool call and exits accordingly.
type ExitPending struct {
	Kind    ExitPendingKind
	Summary string // populated when Kind == ExitPendingComplete
	Message string // populated when Kind == ExitPendingError
}

// ExitSignal is the shared "stop" handle wired between the yolium tools
// and the agent runner.
type ExitSignal struct {
	Pending *ExitPending
}

// NewExitSignal returns a signal with Pending == nil.
func NewExitSignal() *ExitSignal { return &ExitSignal{} }

// EventSink consumes structured Yolium events emitted by the yolium_*
// tools when --yolium-mode is enabled. Implementations are typically
// either a no-op (standalone yoli) or an NDJSON writer to a side
// channel (--events-fd N).
//
// Emit must be safe to call from the goroutine running the agent loop;
// no other goroutine writes events concurrently.
type EventSink interface {
	Emit(Event) error
}

// nopSink discards events.
type nopSink struct{}

func (nopSink) Emit(Event) error { return nil }

// NopSink returns an EventSink that silently discards events. Used when
// --yolium-mode is on but no --events-fd was provided.
func NopSink() EventSink { return nopSink{} }

// NDJSONSink writes one compact JSON line per event to out. Each line
// ends with '\n'. The sink takes ownership of out (callers should close
// it themselves when done).
type NDJSONSink struct {
	Out io.Writer
}

// NewNDJSONSink returns an NDJSONSink wrapping out.
func NewNDJSONSink(out io.Writer) *NDJSONSink {
	return &NDJSONSink{Out: out}
}

// Emit writes evt as a single NDJSON line.
func (s *NDJSONSink) Emit(evt Event) error {
	if s == nil || s.Out == nil {
		return nil
	}
	payload, err := marshalEvent(evt)
	if err != nil {
		return err
	}
	if _, err := s.Out.Write(payload); err != nil {
		return err
	}
	_, err = s.Out.Write([]byte("\n"))
	return err
}
