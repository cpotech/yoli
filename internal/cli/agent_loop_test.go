package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoli/internal/agent"
	agentsession "yoli/internal/agent/session"
	"yoli/internal/agent/yolium"
	"yoli/internal/ai"
	"yoli/internal/ai/providers"
)

func strp(s string) *string { return &s }

func mkUsage(prompt, completion, total int, cost float64) *ai.Usage {
	return &ai.Usage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
		Cost:             cost,
	}
}

func countSubstr(s, sub string) int {
	return strings.Count(s, sub)
}

// --- dispatchAssistantEvents (text-emission protocol) ---

func TestDispatchAssistantEvents_CompleteLineSetsExitPending(t *testing.T) {
	var out bytes.Buffer
	exit := yolium.NewExitSignal()
	content := `@@YOLIUM:{"type":"progress","step":"work","detail":"doing"}` + "\n" +
		`@@YOLIUM:{"type":"complete","summary":"all done"}`

	dispatchAssistantEvents(content, &out, exit)

	if exit.Pending == nil || exit.Pending.Kind != yolium.ExitPendingComplete {
		t.Fatalf("expected complete pending, got %+v", exit.Pending)
	}
	if exit.Pending.Summary != "all done" {
		t.Fatalf("summary = %q want %q", exit.Pending.Summary, "all done")
	}
	got := out.String()
	if !strings.Contains(got, `"type":"progress"`) {
		t.Fatalf("progress not re-emitted: %q", got)
	}
	if !strings.Contains(got, `"type":"complete"`) {
		t.Fatalf("complete not re-emitted: %q", got)
	}
}

func TestDispatchAssistantEvents_ErrorLineSetsExitPending(t *testing.T) {
	var out bytes.Buffer
	exit := yolium.NewExitSignal()
	content := `@@YOLIUM:{"type":"error","message":"something broke"}`

	dispatchAssistantEvents(content, &out, exit)

	if exit.Pending == nil || exit.Pending.Kind != yolium.ExitPendingError {
		t.Fatalf("expected error pending, got %+v", exit.Pending)
	}
	if exit.Pending.Message != "something broke" {
		t.Fatalf("message = %q", exit.Pending.Message)
	}
}

func TestDispatchAssistantEvents_QuestionLineSetsExitPending(t *testing.T) {
	var out bytes.Buffer
	exit := yolium.NewExitSignal()
	content := `@@YOLIUM:{"type":"ask_question","text":"which?","options":["a","b"]}`

	dispatchAssistantEvents(content, &out, exit)

	if exit.Pending == nil || exit.Pending.Kind != yolium.ExitPendingQuestion {
		t.Fatalf("expected question pending, got %+v", exit.Pending)
	}
	if !strings.Contains(out.String(), `"type":"ask_question"`) {
		t.Fatalf("question not re-emitted: %q", out.String())
	}
}

func TestDispatchAssistantEvents_NoEventsLeavesExitUntouched(t *testing.T) {
	var out bytes.Buffer
	exit := yolium.NewExitSignal()
	dispatchAssistantEvents("just plain prose, no markers", &out, exit)
	if exit.Pending != nil {
		t.Fatalf("expected no pending: %+v", exit.Pending)
	}
	if out.String() != "" {
		t.Fatalf("expected no re-emission: %q", out.String())
	}
}

func TestDispatchAssistantEvents_FirstTerminalWins(t *testing.T) {
	var out bytes.Buffer
	exit := yolium.NewExitSignal()
	content := `@@YOLIUM:{"type":"complete","summary":"first"}` + "\n" +
		`@@YOLIUM:{"type":"error","message":"second"}`

	dispatchAssistantEvents(content, &out, exit)

	if exit.Pending == nil || exit.Pending.Kind != yolium.ExitPendingComplete {
		t.Fatalf("expected complete (first) to win, got %+v", exit.Pending)
	}
	// Both lines are still re-emitted on stdout — yolium's parser will
	// see both, but the agent-side pending state is locked to the first.
	if !strings.Contains(out.String(), `"summary":"first"`) {
		t.Fatalf("complete not re-emitted: %q", out.String())
	}
}

// --- usageRecordingProvider ---

func TestUsageRecordingProvider_EmitsPerTurnUsage(t *testing.T) {
	var out bytes.Buffer
	inner := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp("turn one"), Usage: mkUsage(10, 5, 15, 0.001)},
	})
	rec := newUsageRecordingProvider(inner, &out)

	if _, err := rec.Chat(context.Background(), ai.ChatRequest{}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"step":"usage"`) {
		t.Fatalf("no usage progress event: %q", got)
	}
	if !strings.Contains(got, "input:10 output:5 total:15") {
		t.Fatalf("usage detail missing tokens: %q", got)
	}
	if !strings.Contains(got, "cost:$0.001000") {
		t.Fatalf("usage detail missing cost: %q", got)
	}
}

func TestUsageRecordingProvider_AccumulatesCumulative(t *testing.T) {
	var out bytes.Buffer
	inner := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp("a"), Usage: mkUsage(10, 5, 15, 0.001)},
		{Content: strp("b"), Usage: mkUsage(20, 10, 30, 0.002)},
	})
	rec := newUsageRecordingProvider(inner, &out)

	for i := 0; i < 2; i++ {
		if _, err := rec.Chat(context.Background(), ai.ChatRequest{}); err != nil {
			t.Fatalf("Chat %d: %v", i, err)
		}
	}

	cum := rec.Cumulative()
	if cum.PromptTokens != 30 || cum.CompletionTokens != 15 || cum.TotalTokens != 45 {
		t.Fatalf("cumulative tokens = %+v", cum)
	}
	if cum.Cost != 0.003 {
		t.Fatalf("cumulative cost = %f, want 0.003", cum.Cost)
	}
}

func TestUsageRecordingProvider_NoUsageNoEvent(t *testing.T) {
	var out bytes.Buffer
	inner := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp("no usage")},
	})
	rec := newUsageRecordingProvider(inner, &out)

	if _, err := rec.Chat(context.Background(), ai.ChatRequest{}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if strings.Contains(out.String(), `"step":"usage"`) {
		t.Fatalf("unexpected usage event: %q", out.String())
	}
}

// --- runAgentLoop integration (faux provider, no network) ---

func TestRunAgentLoop_EmitsPerTurnAndCumulativeUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// Single assistant turn that emits progress + complete; the loop
	// exits after the assistant turn because there are no tool calls.
	// The usage recorder fires once for the turn, plus one cumulative
	// emission from runAgentLoop after the loop exits.
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{
			Content: strp(`@@YOLIUM:{"type":"progress","step":"work","detail":"doing"}` + "\n" +
				`@@YOLIUM:{"type":"complete","summary":"done"}`),
			Usage: mkUsage(10, 5, 15, 0.001),
		},
	})

	code := runAgentLoop(agentLoopConfig{
		provider: prov,
		model:    "faux",
		prompt:   "go",
		repoPath: t.TempDir(),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}

	out := stdout.String()
	// One per-turn usage event plus one cumulative.
	if n := countSubstr(out, `"step":"usage"`); n != 2 {
		t.Fatalf("usage events = %d, want 2: %q", n, out)
	}
	if !strings.Contains(out, "cumulative input:10 output:5 total:15") {
		t.Fatalf("missing cumulative usage line: %q", out)
	}
}

func TestRunAgentLoop_FallbackEmitsErrorWhenNoTerminalAndNoSummary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp("final answer, no protocol line")},
	})
	repo := t.TempDir()

	code := runAgentLoop(agentLoopConfig{
		provider: prov,
		model:    "faux",
		prompt:   "go",
		repoPath: repo,
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d want 1, stderr=%q", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"type":"error"`) {
		t.Fatalf("no honest error event: %q", out)
	}
	if strings.Contains(out, `"type":"complete"`) {
		t.Fatalf("must not emit fake complete: %q", out)
	}
	if !strings.Contains(out, "Agent stopped without emitting") {
		t.Fatalf("missing honest error message: %q", out)
	}
	if data, err := os.ReadFile(filepath.Join(repo, ".yolium", "summary.md")); err != nil {
		t.Fatalf("summary.md not written: %v", err)
	} else if !strings.Contains(string(data), "Error:") {
		t.Fatalf("summary.md should record the error: %q", data)
	}
}

func TestRunAgentLoop_FallbackEmitsCompleteWhenModelWroteSummaryFile(t *testing.T) {
	// Some models write a summary via the Write tool but forget to emit
	// the @@YOLIUM:{type:"complete"} line. In that case we treat the
	// run as successful (we have a real summary to surface) rather than
	// punishing the user for the model's lapse.
	//
	// We must simulate the summary being written DURING the run (not
	// pre-seeded), because runAgentLoop clears any pre-existing
	// summary.md at startup to defend against stale summaries from
	// earlier agents on the same worktree.
	var stdout, stderr bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "b1",
				Name:      "Bash",
				Arguments: `{"command":"mkdir -p .yolium && printf 'Real summary from a Write tool call\\n' > .yolium/summary.md"}`,
			}},
		},
		{Content: strp("final answer, no protocol line")},
	})
	repo := t.TempDir()

	code := runAgentLoop(agentLoopConfig{
		provider: prov,
		model:    "faux",
		prompt:   "go",
		repoPath: repo,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d want 0, stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"type":"complete"`) {
		t.Fatalf("expected complete event: %q", out)
	}
	if !strings.Contains(out, "Real summary from a Write tool call") {
		t.Fatalf("expected summary in complete event: %q", out)
	}
	if strings.Contains(out, `"type":"error"`) {
		t.Fatalf("must not emit error when summary exists: %q", out)
	}
}

func TestRunAgentLoop_RemovesStaleSummaryFromPriorAgent(t *testing.T) {
	// Regression: when plan-agent → code-agent run on the same
	// worktree, the code-agent's failure (e.g. maxIterations) used to
	// fall through to the "model wrote summary, forgot terminator"
	// fallback and emit yolium_complete with the plan-agent's stale
	// summary. The current run must clear summary.md at startup so the
	// fallback only fires for files written by THIS run.
	var stdout, stderr bytes.Buffer
	// Faux provider returns a single no-protocol response. With no
	// tool calls and !YoliumMode this is treated as a clean exit
	// (runErr == nil) — so the only thing that could trigger the
	// complete-fallback would be a pre-existing summary.md.
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp("final answer, no protocol line")},
	})
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".yolium"), 0o755)
	os.WriteFile(filepath.Join(repo, ".yolium", "summary.md"),
		[]byte("Stale summary from a prior agent run\n"), 0o644)

	code := runAgentLoop(agentLoopConfig{
		provider: prov,
		model:    "faux",
		prompt:   "go",
		repoPath: repo,
	}, &stdout, &stderr)

	out := stdout.String()
	if strings.Contains(out, "Stale summary from a prior agent run") {
		t.Fatalf("stale summary leaked into stdout: %q", out)
	}
	if strings.Contains(out, `"type":"complete"`) {
		t.Fatalf("must not emit complete based on stale summary: %q", out)
	}
	if code == 0 {
		t.Fatalf("expected non-zero exit (no terminator, no current summary); got 0, out=%q", out)
	}
}

func TestRunAgentLoop_MaxIterationsErrorDoesNotEmitStaleComplete(t *testing.T) {
	// Regression: when the loop fails (runErr != nil — e.g.
	// maxIterations exhausted), the fallback path used to call
	// readExistingSummary and emit yolium_complete with whatever was
	// in .yolium/summary.md. A failed run must always surface
	// yolium_error, never a stale complete.
	var stdout, stderr bytes.Buffer
	// Build a response slice that never emits a terminator, longer
	// than the default max iterations so the loop returns an error.
	responses := make([]ai.ChatResponse, agent.DefaultMaxIterations+1)
	for i := range responses {
		responses[i] = ai.ChatResponse{
			ToolCalls: []ai.ToolCall{{
				ID:        fmt.Sprintf("b%d", i),
				Name:      "Bash",
				Arguments: `{"command":"true"}`,
			}},
		}
	}
	prov := providers.NewFauxProvider(responses)
	repo := t.TempDir()

	code := runAgentLoop(agentLoopConfig{
		provider:   prov,
		model:      "faux",
		prompt:     "go",
		repoPath:   repo,
		yoliumMode: true,
	}, &stdout, &stderr)

	out := stdout.String()
	if code == 0 {
		t.Fatalf("expected non-zero exit on maxIterations; got 0, out=%q", out)
	}
	if strings.Contains(out, `"type":"complete"`) {
		t.Fatalf("must not emit complete on maxIterations error: %q", out)
	}
	if !strings.Contains(out, `"type":"error"`) {
		t.Fatalf("expected error event on maxIterations: %q", out)
	}
}

func TestRunAgentLoop_BashEchoCompleteTerminatesLoop(t *testing.T) {
	// Weak models sometimes use `echo '@@YOLIUM:{...}'` instead of
	// writing the protocol line directly in their text. Verify that a
	// terminal event emitted via Bash stdout cleanly stops the loop.
	var stdout, stderr bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "b1",
				Name:      "Bash",
				Arguments: `{"command":"echo '@@YOLIUM:{\"type\":\"complete\",\"summary\":\"via bash echo\"}'"}`,
			}},
		},
		// The loop should NOT reach this turn because the bash result
		// scan sets exit.Pending and Stop() returns true before the
		// next iteration starts.
		{Content: strp("should not be reached")},
	})
	repo := t.TempDir()

	code := runAgentLoop(agentLoopConfig{
		provider: prov,
		model:    "faux",
		prompt:   "go",
		repoPath: repo,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"type":"complete"`) || !strings.Contains(out, "via bash echo") {
		t.Fatalf("bash-emitted complete not surfaced: %q", out)
	}
	if data, err := os.ReadFile(filepath.Join(repo, ".yolium", "summary.md")); err != nil {
		t.Fatalf("summary.md not written: %v", err)
	} else if !strings.Contains(string(data), "via bash echo") {
		t.Fatalf("summary.md = %q", data)
	}
}

func TestRunAgentLoop_AppendsPromptAssistantAndToolMessagesToSession(t *testing.T) {
	var stdout, stderr bytes.Buffer
	repo := t.TempDir()
	root := t.TempDir()
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{
			ToolCalls: []ai.ToolCall{{
				ID:        "b1",
				Name:      "Bash",
				Arguments: `{"command":"printf ok"}`,
			}},
		},
		{Content: strp(`@@YOLIUM:{"type":"complete","summary":"done"}`)},
	})

	code := runAgentLoop(agentLoopConfig{
		provider:    prov,
		model:       "faux",
		prompt:      "go",
		repoPath:    repo,
		sessionRoot: root,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	sessions, err := agentsession.List(agentsession.Options{RootDir: root, Cwd: repo})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions=%d", len(sessions))
	}
	msgs := sessions[0].BuildMessages()
	var roles []ai.Role
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	want := []ai.Role{ai.RoleUser, ai.RoleAssistant, ai.RoleTool, ai.RoleAssistant}
	if len(roles) != len(want) {
		t.Fatalf("roles=%v", roles)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("roles=%v", roles)
		}
	}
}

func TestRunAgentLoop_UsesExistingSessionBranchAsContext(t *testing.T) {
	repo := t.TempDir()
	root := t.TempDir()
	existing, err := agentsession.Create(agentsession.Options{RootDir: root, Cwd: repo})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	old := "old context"
	if _, err := existing.AppendMessage(ai.Message{Role: ai.RoleUser, Content: &old}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	rec := &recordingTestProvider{response: ai.ChatResponse{Content: strp(`@@YOLIUM:{"type":"complete","summary":"done"}`)}}
	var stdout, stderr bytes.Buffer
	code := runAgentLoop(agentLoopConfig{
		provider:      rec,
		model:         "faux",
		prompt:        "new prompt",
		repoPath:      repo,
		sessionRoot:   root,
		sessionTarget: existing.GetSessionID(),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if len(rec.messages) < 3 || rec.messages[1].Content == nil || *rec.messages[1].Content != "old context" {
		t.Fatalf("messages = %+v", rec.messages)
	}
}

func TestRunAgentLoop_NoSessionDoesNotCreateSessionFile(t *testing.T) {
	repo := t.TempDir()
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp(`@@YOLIUM:{"type":"complete","summary":"done"}`)},
	})
	code := runAgentLoop(agentLoopConfig{
		provider:    prov,
		model:       "faux",
		prompt:      "go",
		repoPath:    repo,
		noSession:   true,
		sessionRoot: root,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	sessions, err := agentsession.ListAll(agentsession.Options{RootDir: root})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions=%d", len(sessions))
	}
}

func TestRunAgentLoop_AskQuestionPersistsPartialConversation(t *testing.T) {
	repo := t.TempDir()
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp(`@@YOLIUM:{"type":"ask_question","text":"which?"}`)},
	})
	code := runAgentLoop(agentLoopConfig{
		provider:    prov,
		model:       "faux",
		prompt:      "go",
		repoPath:    repo,
		sessionRoot: root,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	sessions, err := agentsession.List(agentsession.Options{RootDir: root, Cwd: repo})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 || len(sessions[0].BuildMessages()) != 2 {
		t.Fatalf("sessions=%d messages=%d", len(sessions), len(sessions[0].BuildMessages()))
	}
}

type recordingTestProvider struct {
	response ai.ChatResponse
	messages []ai.Message
}

func (p *recordingTestProvider) Chat(_ context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	p.messages = append([]ai.Message(nil), req.Messages...)
	return p.response, nil
}

func TestRunAgentLoop_CompleteLineWritesSummaryNoFallback(t *testing.T) {
	var stdout, stderr bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp(`Here is the result.` + "\n" +
			`@@YOLIUM:{"type":"complete","summary":"explicit summary"}`)},
	})
	repo := t.TempDir()

	code := runAgentLoop(agentLoopConfig{
		provider: prov,
		model:    "faux",
		prompt:   "go",
		repoPath: repo,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, `"type":"complete"`) || !strings.Contains(out, "explicit summary") {
		t.Fatalf("complete event missing/incorrect: %q", out)
	}
	// Fallback string must NOT appear when the model emitted a complete line.
	if strings.Contains(out, "Agent finished without an explicit completion signal") {
		t.Fatalf("unexpected fallback summary: %q", out)
	}
	if data, err := os.ReadFile(filepath.Join(repo, ".yolium", "summary.md")); err != nil {
		t.Fatalf("summary.md not written: %v", err)
	} else if !strings.Contains(string(data), "explicit summary") {
		t.Fatalf("summary.md = %q", data)
	}
}

func TestRunAgentLoop_ErrorLineExitsNonZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp(`@@YOLIUM:{"type":"error","message":"could not build"}`)},
	})
	repo := t.TempDir()

	code := runAgentLoop(agentLoopConfig{
		provider: prov,
		model:    "faux",
		prompt:   "go",
		repoPath: repo,
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d want 1; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"type":"error"`) {
		t.Fatalf("no error event in stdout: %q", stdout.String())
	}
}

func TestRunAgentLoop_QuestionLineExitsCleanlyWithoutFallback(t *testing.T) {
	var stdout, stderr bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp(`@@YOLIUM:{"type":"ask_question","text":"which library?"}`)},
	})
	repo := t.TempDir()

	code := runAgentLoop(agentLoopConfig{
		provider: prov,
		model:    "faux",
		prompt:   "go",
		repoPath: repo,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"type":"ask_question"`) {
		t.Fatalf("no question event: %q", out)
	}
	// No fallback complete should be emitted — yolium will resume the
	// agent after the user answers, and a complete now would mislead it.
	if strings.Contains(out, `"type":"complete"`) {
		t.Fatalf("unexpected complete after question: %q", out)
	}
	if _, err := os.Stat(filepath.Join(repo, ".yolium", "summary.md")); err == nil {
		t.Fatalf("summary.md should not be written for a pending question")
	}
}
