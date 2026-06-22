package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"yoli/internal/agent/yolium"
	"yoli/internal/ai"
	"yoli/internal/ai/providers"
)

// recordingProvider wraps a FauxProvider but additionally captures
// every ChatRequest for assertion (in particular, the Tools list).
type recordingProvider struct {
	inner *providers.FauxProvider
	reqs  []ai.ChatRequest
}

func (r *recordingProvider) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	r.reqs = append(r.reqs, req)
	return r.inner.Chat(ctx, req)
}

func TestParseAgentFlags_YoliumModeAndEventsFD(t *testing.T) {
	f, err := parseAgentFlags([]string{"--yolium-mode", "--events-fd", "3", "--prompt", "x"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !f.YoliumMode {
		t.Fatal("YoliumMode not set")
	}
	if f.EventsFD != 3 {
		t.Fatalf("EventsFD=%d want 3", f.EventsFD)
	}
	if f.Prompt != "x" {
		t.Fatalf("Prompt=%q", f.Prompt)
	}
}

func TestParseAgentFlags_EventsFDEquals(t *testing.T) {
	f, err := parseAgentFlags([]string{"--events-fd=5"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.EventsFD != 5 {
		t.Fatalf("EventsFD=%d want 5", f.EventsFD)
	}
}

func TestParseAgentFlags_EventsFDRequiresInt(t *testing.T) {
	if _, err := parseAgentFlags([]string{"--events-fd", "abc"}); err == nil {
		t.Fatal("expected parse error for non-numeric fd")
	}
}

func TestParseAgentFlags_YoliumModeDefaultsFalse(t *testing.T) {
	f, err := parseAgentFlags([]string{"--prompt", "p"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.YoliumMode {
		t.Fatal("YoliumMode should default false")
	}
	if f.EventsFD != 0 {
		t.Fatalf("EventsFD=%d should default 0", f.EventsFD)
	}
}

// TestRunAgentLoop_YoliumMode_TerminatorToolExitsCleanly verifies that
// under --yolium-mode a `yolium_complete` tool call ends the run and
// emits the structured event via the EventSink.
func TestRunAgentLoop_YoliumMode_TerminatorToolExitsCleanly(t *testing.T) {
	var stdout, stderr, events bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "c1",
				Name:      yolium.ToolComplete,
				Arguments: `{"summary":"done via tool"}`,
			}},
		},
	})

	code := runAgentLoop(agentLoopConfig{
		provider:   prov,
		model:      "faux",
		prompt:     "go",
		repoPath:   t.TempDir(),
		yoliumMode: true,
		eventSink:  yolium.NewNDJSONSink(&events),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(events.String(), `"type":"complete"`) ||
		!strings.Contains(events.String(), "done via tool") {
		t.Fatalf("expected complete in NDJSON events: %q", events.String())
	}
}

// TestRunAgentLoop_YoliumMode_NoToolCallsContinues_ThenTerminatorWins
// verifies that an empty assistant turn under YoliumMode does NOT
// terminate the loop (in contrast to standalone behavior). The next
// turn calls yolium_complete which does terminate it.
func TestRunAgentLoop_YoliumMode_NoToolCallsContinues_ThenTerminatorWins(t *testing.T) {
	var stdout, stderr, events bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp("thinking...")}, // no tool calls — loop must continue
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "c1",
				Name:      yolium.ToolComplete,
				Arguments: `{"summary":"finished"}`,
			}},
		},
	})

	code := runAgentLoop(agentLoopConfig{
		provider:   prov,
		model:      "faux",
		prompt:     "go",
		repoPath:   t.TempDir(),
		yoliumMode: true,
		eventSink:  yolium.NewNDJSONSink(&events),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(events.String(), "finished") {
		t.Fatalf("expected complete in events: %q", events.String())
	}
}

// TestRunAgentLoop_YoliumMode_ProgressToolEmitsButDoesNotExit verifies
// non-terminator tools emit on the EventSink and continue the loop.
func TestRunAgentLoop_YoliumMode_ProgressToolEmitsButDoesNotExit(t *testing.T) {
	var stdout, stderr, events bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "p1",
				Name:      yolium.ToolProgress,
				Arguments: `{"step":"model","detail":"openrouter/free"}`,
			}},
		},
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "c1",
				Name:      yolium.ToolComplete,
				Arguments: `{"summary":"done"}`,
			}},
		},
	})

	code := runAgentLoop(agentLoopConfig{
		provider:   prov,
		model:      "faux",
		prompt:     "go",
		repoPath:   t.TempDir(),
		yoliumMode: true,
		eventSink:  yolium.NewNDJSONSink(&events),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(events.String(), `"step":"model"`) {
		t.Fatalf("missing progress event: %q", events.String())
	}
	if !strings.Contains(events.String(), `"type":"complete"`) {
		t.Fatalf("missing complete event: %q", events.String())
	}
}

// TestRunAgentLoop_StandaloneMode_NoYoliumToolsRegistered verifies that
// with yoliumMode=false the yolium_* tools are NOT exposed to the
// model, preserving standalone yoli behavior byte-for-byte.
func TestRunAgentLoop_StandaloneMode_NoYoliumToolsRegistered(t *testing.T) {
	var stdout, stderr bytes.Buffer
	prov := &recordingProvider{inner: providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp(`@@YOLIUM:{"type":"complete","summary":"standalone done"}`)},
	})}

	code := runAgentLoop(agentLoopConfig{
		provider: prov,
		model:    "faux",
		prompt:   "go",
		repoPath: t.TempDir(),
		// yoliumMode left false — this is standalone behavior.
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}

	for _, req := range prov.reqs {
		for _, td := range req.Tools {
			if strings.HasPrefix(td.Name, "yolium_") {
				t.Fatalf("standalone run leaked yolium tool: %s", td.Name)
			}
		}
	}
}

// TestRunAgentLoop_YoliumMode_RegistersYoliumTools verifies the opt-in
// path actually exposes the yolium_* tools to the provider.
func TestRunAgentLoop_YoliumMode_RegistersYoliumTools(t *testing.T) {
	var stdout, stderr, events bytes.Buffer
	prov := &recordingProvider{inner: providers.NewFauxProvider([]ai.ChatResponse{
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "c1",
				Name:      yolium.ToolComplete,
				Arguments: `{"summary":"done"}`,
			}},
		},
	})}

	code := runAgentLoop(agentLoopConfig{
		provider:   prov,
		model:      "faux",
		prompt:     "go",
		repoPath:   t.TempDir(),
		yoliumMode: true,
		eventSink:  yolium.NewNDJSONSink(&events),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}

	if len(prov.reqs) == 0 {
		t.Fatal("no ChatRequest recorded")
	}
	seen := make(map[string]bool)
	for _, td := range prov.reqs[0].Tools {
		seen[td.Name] = true
	}
	for _, want := range []string{
		yolium.ToolComplete, yolium.ToolError, yolium.ToolAskQuestion,
		yolium.ToolProgress, yolium.ToolAddComment,
	} {
		if !seen[want] {
			t.Errorf("yolium-mode missing tool: %s", want)
		}
	}
}
