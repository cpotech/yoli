package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

// guardGitRun runs a git subcommand inside root as deterministic test
// setup (identity pinned by initGuardWorktree; failures are fatal).
func guardGitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup: git %v: %v\n%s", args, err, out)
	}
}

// initGuardWorktree prepares a git repo at root with one committed file,
// mimicking a fresh Yolium worktree before the agent starts.
func initGuardWorktree(t *testing.T, root string) {
	t.Helper()
	for _, c := range [][]string{
		{"init", "-q"},
		{"symbolic-ref", "HEAD", "refs/heads/main"},
		{"config", "user.email", "yoli-test@example.com"},
		{"config", "user.name", "Yoli Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		guardGitRun(t, root, c...)
	}
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write base.txt: %v", err)
	}
	guardGitRun(t, root, "add", "base.txt")
	guardGitRun(t, root, "commit", "-q", "-m", "base")
}

// TestRunAgentLoop_YoliumMode_CompleteGuardForcesCommit exercises the
// dirty-worktree guard end-to-end: the first yolium_complete is rejected
// with a corrective tool result, the model commits via the real Bash
// tool, and the retried yolium_complete then succeeds.
func TestRunAgentLoop_YoliumMode_CompleteGuardForcesCommit(t *testing.T) {
	root := t.TempDir()
	initGuardWorktree(t, root)
	if err := os.WriteFile(filepath.Join(root, "work.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}

	var stdout, stderr, events bytes.Buffer
	prov := &recordingProvider{inner: providers.NewFauxProvider([]ai.ChatResponse{
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "c1",
				Name:      yolium.ToolComplete,
				Arguments: `{"summary":"done"}`,
			}},
		},
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "b1",
				Name:      "Bash",
				Arguments: `{"command":"git add -A && git commit -q -m 'feat: work'"}`,
			}},
		},
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "c2",
				Name:      yolium.ToolComplete,
				Arguments: `{"summary":"done"}`,
			}},
		},
	})}

	code := runAgentLoop(agentLoopConfig{
		provider:   prov,
		model:      "faux",
		prompt:     "go",
		repoPath:   root,
		yoliumMode: true,
		eventSink:  yolium.NewNDJSONSink(&events),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if n := countSubstr(events.String(), `"type":"complete"`); n != 1 {
		t.Fatalf("complete events=%d want 1; events=%q", n, events.String())
	}
	// The rejection must have reached the model as a corrective tool
	// result in the request following the first complete attempt.
	if len(prov.reqs) < 2 {
		t.Fatalf("reqs=%d want >=2", len(prov.reqs))
	}
	rejected := false
	for _, m := range prov.reqs[1].Messages {
		if m.Role == ai.RoleTool && m.Content != nil &&
			strings.Contains(*m.Content, "uncommitted change") &&
			strings.Contains(*m.Content, "work.txt") {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("second request must carry the dirty-worktree rejection naming work.txt")
	}
	if yolium.WorktreeBlocksComplete(context.Background(), root) {
		t.Fatal("worktree must be clean after the forced commit")
	}
}

// TestRunAgentLoop_YoliumMode_TextCompleteSuppressedWhenDirty verifies
// the text-protocol bypass is closed: a `@@YOLIUM:{"type":"complete"}`
// line in assistant prose must not end a dirty run (nor be re-emitted on
// stdout where Yolium's parser would see it). The run only completes
// after a real commit and a yolium_complete tool call.
func TestRunAgentLoop_YoliumMode_TextCompleteSuppressedWhenDirty(t *testing.T) {
	root := t.TempDir()
	initGuardWorktree(t, root)
	if err := os.WriteFile(filepath.Join(root, "work.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}

	var stdout, stderr, events bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp(`@@YOLIUM:{"type":"complete","summary":"claimed without commit"}`)},
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "b1",
				Name:      "Bash",
				Arguments: `{"command":"git add -A && git commit -q -m 'feat: work'"}`,
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
		repoPath:   root,
		yoliumMode: true,
		eventSink:  yolium.NewNDJSONSink(&events),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "claimed without commit") {
		t.Fatalf("suppressed text complete leaked to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "text complete suppressed") {
		t.Fatalf("expected suppression note on stderr: %q", stderr.String())
	}
	if n := countSubstr(events.String(), `"type":"complete"`); n != 1 {
		t.Fatalf("complete events=%d want 1; events=%q", n, events.String())
	}
	if !strings.Contains(events.String(), `"summary":"done"`) {
		t.Fatalf("final complete must be the tool call's: %q", events.String())
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
