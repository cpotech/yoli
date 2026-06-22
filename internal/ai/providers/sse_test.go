package providers

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/iotest"

	"yoli/internal/ai"
)

// --- MergeToolCallDeltas ---

func TestMergeToolCallDeltas_InitialDeltaCreatesEntry(t *testing.T) {
	acc := map[int]*ToolCallAccumulator{}
	chunk := MergeToolCallDeltas(acc, WireToolCallDelta{
		Index:    0,
		ID:       "call_1",
		Type:     "function",
		Function: &WireToolCallFn{Name: "Read", Arguments: ""},
	})

	if len(acc) != 1 {
		t.Fatalf("acc size = %d, want 1", len(acc))
	}
	got := acc[0]
	if got.ID != "call_1" || got.Name != "Read" || got.Arguments != "" {
		t.Fatalf("acc[0] = %+v, want id=call_1 name=Read args=\"\"", got)
	}
	want := ai.ChatStreamChunk{
		Type:           ai.ChunkToolCall,
		Index:          0,
		ID:             "call_1",
		Name:           "Read",
		ArgumentsDelta: "",
	}
	if chunk != want {
		t.Fatalf("chunk = %+v, want %+v", chunk, want)
	}
}

func TestMergeToolCallDeltas_AppendsArgumentsWithoutClobberingIdName(t *testing.T) {
	acc := map[int]*ToolCallAccumulator{}
	MergeToolCallDeltas(acc, WireToolCallDelta{
		Index:    0,
		ID:       "call_1",
		Function: &WireToolCallFn{Name: "Read", Arguments: `{"pa`},
	})
	MergeToolCallDeltas(acc, WireToolCallDelta{
		Index:    0,
		Function: &WireToolCallFn{Arguments: `th":"a"}`},
	})
	got := acc[0]
	if got.ID != "call_1" || got.Name != "Read" || got.Arguments != `{"path":"a"}` {
		t.Fatalf("acc[0] = %+v, want id=call_1 name=Read args=`{\"path\":\"a\"}`", got)
	}
}

func TestMergeToolCallDeltas_KeepsIndicesIndependent(t *testing.T) {
	acc := map[int]*ToolCallAccumulator{}
	MergeToolCallDeltas(acc, WireToolCallDelta{
		Index:    0,
		ID:       "call_a",
		Function: &WireToolCallFn{Name: "foo", Arguments: "{}"},
	})
	MergeToolCallDeltas(acc, WireToolCallDelta{
		Index:    1,
		ID:       "call_b",
		Function: &WireToolCallFn{Name: "bar", Arguments: `{"x":1}`},
	})
	if len(acc) != 2 {
		t.Fatalf("acc size = %d, want 2", len(acc))
	}
	if a := acc[0]; a.ID != "call_a" || a.Name != "foo" || a.Arguments != "{}" {
		t.Fatalf("acc[0] = %+v", a)
	}
	if b := acc[1]; b.ID != "call_b" || b.Name != "bar" || b.Arguments != `{"x":1}` {
		t.Fatalf("acc[1] = %+v", b)
	}
}

func TestMergeToolCallDeltas_ToleratesAbsentIdName(t *testing.T) {
	acc := map[int]*ToolCallAccumulator{}
	MergeToolCallDeltas(acc, WireToolCallDelta{
		Index:    0,
		ID:       "call_1",
		Function: &WireToolCallFn{Name: "foo", Arguments: ""},
	})
	MergeToolCallDeltas(acc, WireToolCallDelta{
		Index:    0,
		Function: &WireToolCallFn{Arguments: "{}"},
	})
	got := acc[0]
	if got.ID != "call_1" || got.Name != "foo" || got.Arguments != "{}" {
		t.Fatalf("acc[0] = %+v", got)
	}
}

// --- IterSSE ---

func collectSSE(t *testing.T, body string) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	for raw, err := range IterSSE(strings.NewReader(body)) {
		if err != nil {
			t.Fatalf("IterSSE error: %v", err)
		}
		out = append(out, raw)
	}
	return out
}

func TestIterSSE_YieldsJSONPayloads(t *testing.T) {
	body := "data: {\"a\":1}\n\n" +
		"data: {\"b\":2}\n\n" +
		"data: [DONE]\n\n"
	events := collectSSE(t, body)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	var first, second map[string]int
	if err := json.Unmarshal(events[0], &first); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}
	if err := json.Unmarshal(events[1], &second); err != nil {
		t.Fatalf("unmarshal second: %v", err)
	}
	if first["a"] != 1 || second["b"] != 2 {
		t.Fatalf("events = %v %v", first, second)
	}
}

func TestIterSSE_SkipsCommentKeepalives(t *testing.T) {
	body := ": keep-alive\n\n" +
		": another comment\n\n" +
		"data: {\"ok\":true}\n\n" +
		"data: [DONE]\n\n"
	events := collectSSE(t, body)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	var got map[string]bool
	if err := json.Unmarshal(events[0], &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got["ok"] {
		t.Fatalf("event = %v, want ok:true", got)
	}
}

func TestIterSSE_TerminatesOnDoneSentinel(t *testing.T) {
	body := "data: {\"first\":1}\n\n" +
		"data: [DONE]\n\n" +
		"data: {\"shouldBeIgnored\":true}\n\n"
	events := collectSSE(t, body)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func TestIterSSE_BuffersPartialFramesAcrossReads(t *testing.T) {
	body := "data: {\"hello\":\"world\"}\n\n" + "data: [DONE]\n\n"
	// OneByteReader forces the scanner to refill one byte at a time,
	// exercising the buffered split path.
	r := iotest.OneByteReader(strings.NewReader(body))
	var events []json.RawMessage
	for raw, err := range IterSSE(r) {
		if err != nil {
			t.Fatalf("IterSSE error: %v", err)
		}
		events = append(events, raw)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	var got map[string]string
	if err := json.Unmarshal(events[0], &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["hello"] != "world" {
		t.Fatalf("event = %v, want hello:world", got)
	}
}

func TestIterSSE_HandlesCRLFBoundaries(t *testing.T) {
	body := "data: {\"x\":1}\r\n\r\n" + "data: [DONE]\r\n\r\n"
	events := collectSSE(t, body)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}
