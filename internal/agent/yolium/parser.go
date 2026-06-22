package yolium

import (
	"encoding/json"
	"strings"
)

// ScanText finds every "@@YOLIUM:<json>" line in text and returns the
// parsed events in source order. Lines whose JSON is malformed, whose
// type field is missing, or whose type is not recognised are skipped
// silently so a flaky model can't crash the loop.
//
// The scanner is intentionally tolerant:
//   - A line that does not start with Prefix is ignored.
//   - Trailing carriage returns and whitespace around the JSON payload
//     are trimmed.
//   - An invalid JSON payload is skipped (not returned, not errored).
//   - An unknown type discriminator is skipped.
func ScanText(text string) []Event {
	if text == "" {
		return nil
	}
	var out []Event
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r\t ")
		if !strings.HasPrefix(line, Prefix) {
			continue
		}
		payload := strings.TrimSpace(line[len(Prefix):])
		if payload == "" {
			continue
		}
		evt, ok := parseEvent([]byte(payload))
		if !ok {
			continue
		}
		out = append(out, evt)
	}
	return out
}

// parseEvent decodes a single @@YOLIUM JSON payload into a typed Event.
// Returns (nil, false) for malformed input or unknown discriminators.
func parseEvent(payload []byte) (Event, bool) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &head); err != nil {
		return nil, false
	}
	switch head.Type {
	case "progress":
		var e ProgressEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, false
		}
		return e, true
	case "comment", "add_comment":
		var e CommentEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, false
		}
		e.Type = "comment"
		return e, true
	case "create_item":
		var e CreateItemEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, false
		}
		return e, true
	case "action":
		var e ActionEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, false
		}
		return e, true
	case "start_agent":
		var e StartAgentEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, false
		}
		return e, true
	case "update_description":
		var e UpdateDescriptionEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, false
		}
		return e, true
	case "set_test_specs":
		var e SetTestSpecsEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, false
		}
		return e, true
	case "ask_question", "question":
		var e AskQuestionEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, false
		}
		e.Type = "ask_question"
		return e, true
	case "complete":
		var e CompleteEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, false
		}
		return e, true
	case "error":
		var e ErrorEvent
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, false
		}
		return e, true
	default:
		return nil, false
	}
}

// TerminalEvent classifies an Event as one that should stop the agent
// loop. Returns the ExitPending payload (for complete/error) or signals
// a pending question (Kind="" + ok=true) for ask_question events.
//
// Returns ok=false for non-terminal events (progress, comment, etc.).
func TerminalEvent(evt Event) (kind string, summary string, message string, ok bool) {
	switch e := evt.(type) {
	case CompleteEvent:
		return "complete", e.Summary, "", true
	case ErrorEvent:
		return "error", "", e.Message, true
	case AskQuestionEvent:
		return "question", "", "", true
	default:
		return "", "", "", false
	}
}
