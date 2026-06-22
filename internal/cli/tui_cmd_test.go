package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentsession "yoli/internal/agent/session"
	"yoli/internal/agent/tools"
	"yoli/internal/ai"
	"yoli/internal/ai/providers"
)

// recordingProvider is shared with agent_yolium_mode_test.go: it wraps a
// FauxProvider and captures every ChatRequest for assertion.

func strptr(s string) *string { return &s }

// newTUITestConfig builds a tuiLoopConfig wired for in-process tests:
// in-memory sessions, no color, non-interactive, no signal handling.
func newTUITestConfig(p ai.Provider) tuiLoopConfig {
	opts := agentsession.Options{Cwd: "/tmp"}
	return tuiLoopConfig{
		provider:   p,
		model:      "test/model",
		tools:      nil,
		sess:       agentsession.InMemory(opts),
		newSession: func() (*agentsession.Session, error) { return agentsession.InMemory(opts), nil },
	}
}

// runTUITest runs runTUILoop over scripted stdin and returns exit code,
// stdout, and stderr.
func runTUITest(t *testing.T, c tuiLoopConfig, stdin string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := runTUILoop(c, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestTUI_ExitCommandReturnsZeroWithoutProviderCall(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	c := newTUITestConfig(rec)
	code, _, _ := runTUITest(t, c, "/exit\nshould never be sent\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(rec.reqs) != 0 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
}

func TestTUI_QuitCommandReturnsZero(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	c := newTUITestConfig(rec)
	code, _, _ := runTUITest(t, c, "/quit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(rec.reqs) != 0 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
}

func TestTUI_EOFOnEmptyStdinReturnsZero(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	c := newTUITestConfig(rec)
	code, _, _ := runTUITest(t, c, "")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(rec.reqs) != 0 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
}

func TestTUI_BlankLinesSkippedWithoutProviderCall(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	c := newTUITestConfig(rec)
	code, _, _ := runTUITest(t, c, "\n   \n\t\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(rec.reqs) != 0 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
}

func TestTUI_AssistantContentRenderedToStdout(t *testing.T) {
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strptr("hello from assistant")},
	})
	c := newTUITestConfig(faux)
	code, stdout, _ := runTUITest(t, c, "hi\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "hello from assistant") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestTUI_PartialFinalLineProcessedBeforeEOF(t *testing.T) {
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strptr("partial-line-reply")},
	})
	c := newTUITestConfig(faux)
	// No trailing newline: ReadString returns the line together with io.EOF.
	code, stdout, _ := runTUITest(t, c, "hi there")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "partial-line-reply") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestTUI_ToolCallAndResultRendered(t *testing.T) {
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "Glob", Arguments: `{"pattern":"*.go"}`}}},
		{Content: strptr("done")},
	})
	c := newTUITestConfig(faux)
	c.tools = tools.DefaultTools(t.TempDir())
	code, stdout, _ := runTUITest(t, c, "list go files\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "→ Glob") || !strings.Contains(stdout, `{"pattern":"*.go"}`) {
		t.Fatalf("missing tool-call line: stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "← Glob (") || !strings.Contains(stdout, "bytes)") {
		t.Fatalf("missing tool-result line: stdout = %q", stdout)
	}
}

func TestTUI_ErrorToolResultMarkedRedWhenColorEnabled(t *testing.T) {
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "Nope", Arguments: `{}`}}},
		{Content: strptr("done")},
	})
	c := newTUITestConfig(faux)
	c.color = true
	// No tools registered: the loop produces "Error: unknown tool Nope".
	code, stdout, _ := runTUITest(t, c, "do it\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "\x1b[31m") {
		t.Fatalf("no red ANSI marker for error result: stdout = %q", stdout)
	}
}

func TestTUI_NoANSIWhenColorDisabled(t *testing.T) {
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "Nope", Arguments: `{}`}}},
		{Content: strptr("plain reply")},
	})
	c := newTUITestConfig(faux)
	code, stdout, stderr := runTUITest(t, c, "go\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(stdout, "\x1b[") || strings.Contains(stderr, "\x1b[") {
		t.Fatalf("ANSI escapes in non-color output: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestTUI_NoSpinnerOrPromptWhenNotInteractive(t *testing.T) {
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strptr("reply")},
	})
	c := newTUITestConfig(faux)
	code, _, stderr := runTUITest(t, c, "hi\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(stderr, "thinking") || strings.Contains(stderr, "> ") {
		t.Fatalf("interactive chrome leaked to stderr: %q", stderr)
	}
}

func TestTUI_SessionPersistsUserAssistantAndToolMessages(t *testing.T) {
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "Nope", Arguments: `{}`}}},
		{Content: strptr("all done")},
	})
	c := newTUITestConfig(faux)
	code, _, _ := runTUITest(t, c, "do work\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	msgs := c.sess.BuildMessages()
	var haveUser, haveAssistant, haveTool bool
	for _, m := range msgs {
		switch m.Role {
		case ai.RoleUser:
			if m.Content != nil && *m.Content == "do work" {
				haveUser = true
			}
		case ai.RoleAssistant:
			haveAssistant = true
		case ai.RoleTool:
			haveTool = true
		}
	}
	if !haveUser || !haveAssistant || !haveTool {
		t.Fatalf("session missing roles: user=%v assistant=%v tool=%v (%d msgs)", haveUser, haveAssistant, haveTool, len(msgs))
	}
}

func TestTUI_HelpListsCommandsWithoutProviderCall(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	c := newTUITestConfig(rec)
	code, stdout, _ := runTUITest(t, c, "/help\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, cmd := range []string{"/help", "/model", "/context", "/clear", "/exit", "/quit"} {
		if !strings.Contains(stdout, cmd) {
			t.Fatalf("help output missing %s: %q", cmd, stdout)
		}
	}
	if len(rec.reqs) != 0 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
}

func TestTUI_ModelCommandSwitchesModelForNextTurn(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strptr("ok")},
	})}
	c := newTUITestConfig(rec)
	code, _, _ := runTUITest(t, c, "/model new/model-slug\nhi\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(rec.reqs) != 1 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
	if rec.reqs[0].Model != "new/model-slug" {
		t.Fatalf("request model = %q", rec.reqs[0].Model)
	}
}

func TestTUI_ModelCommandWithoutArgPrintsCurrent(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	c := newTUITestConfig(rec)
	code, stdout, _ := runTUITest(t, c, "/model\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "test/model") {
		t.Fatalf("current model not printed: %q", stdout)
	}
	if len(rec.reqs) != 0 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
}

func TestTUI_ClearStartsNewSessionDroppingPriorTurns(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strptr("first reply")},
		{Content: strptr("second reply")},
	})}
	c := newTUITestConfig(rec)
	code, _, _ := runTUITest(t, c, "remember the codeword\n/clear\nwhat now\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(rec.reqs) != 2 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
	for _, m := range rec.reqs[1].Messages {
		if m.Content != nil && strings.Contains(*m.Content, "remember the codeword") {
			t.Fatalf("prior turn leaked into post-/clear request")
		}
	}
}

func TestTUI_ContextCommandPrintsEstimateWithoutProviderCall(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	c := newTUITestConfig(rec)
	code, stdout, _ := runTUITest(t, c, "/context\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "tokens") {
		t.Fatalf("context estimate not printed: %q", stdout)
	}
	if len(rec.reqs) != 0 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
}

func TestTUI_UnknownSlashCommandPrintsHintWithoutProviderCall(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	c := newTUITestConfig(rec)
	code, stdout, _ := runTUITest(t, c, "/wat\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "/wat") || !strings.Contains(stdout, "/help") {
		t.Fatalf("unknown-command hint missing: %q", stdout)
	}
	if len(rec.reqs) != 0 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
}

func TestTUI_ProviderErrorKeepsREPLAlive(t *testing.T) {
	// Empty script: the first turn returns *ScriptExhaustedError. The REPL
	// must print it and keep reading; the /exit afterwards must still work.
	faux := providers.NewFauxProvider(nil)
	c := newTUITestConfig(faux)
	code, _, stderr := runTUITest(t, c, "boom\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d (REPL died on provider error)", code)
	}
	if !strings.Contains(stderr, "script exhausted") {
		t.Fatalf("provider error not reported: %q", stderr)
	}
}

func TestTUI_ColorDisabledByNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	f, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if tuiColorEnabled(f) {
		t.Fatal("tuiColorEnabled = true with NO_COLOR set")
	}
}

func TestTUI_ColorDisabledOnNonTerminalFile(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	f, err := os.Create(filepath.Join(t.TempDir(), "regular-file"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if tuiColorEnabled(f) {
		t.Fatal("tuiColorEnabled = true for a regular file")
	}
}

// TestTuiLineEditor tests the line editor functionality
func TestTuiLineEditor(t *testing.T) {
	t.Run("addToHistory deduplicates consecutive", func(t *testing.T) {
		e := &tuiLineEditor{
			history: make([]string, 0),
			histIdx: -1,
		}
		e.addToHistory("test")
		e.addToHistory("test") // Should not be added
		if len(e.history) != 1 {
			t.Errorf("expected 1 history entry, got %d", len(e.history))
		}
	})

	t.Run("addToHistory allows different entries", func(t *testing.T) {
		e := &tuiLineEditor{
			history: make([]string, 0),
			histIdx: -1,
		}
		e.addToHistory("cmd1")
		e.addToHistory("cmd2")
		if len(e.history) != 2 {
			t.Errorf("expected 2 history entries, got %d", len(e.history))
		}
	})

	t.Run("addToHistory bounds size", func(t *testing.T) {
		e := &tuiLineEditor{
			history: make([]string, 0),
			histIdx: -1,
		}
		for i := 0; i < 1001; i++ {
			e.addToHistory(fmt.Sprintf("cmd%d", i))
		}
		if len(e.history) != 1000 {
			t.Errorf("expected 1000 history entries, got %d", len(e.history))
		}
		// First entry should be cmd1 (cmd0 was evicted)
		if e.history[0] != "cmd1" {
			t.Errorf("expected oldest entry to be cmd1, got %s", e.history[0])
		}
	})

	t.Run("historyUp navigates correctly", func(t *testing.T) {
		e := &tuiLineEditor{
			stdout:  bufio.NewWriter(io.Discard),
			history: []string{"cmd1", "cmd2", "cmd3"},
			histIdx: -1,
		}
		e.historyUp()
		if e.histIdx != 2 {
			t.Errorf("expected histIdx=2, got %d", e.histIdx)
		}
		if e.prompt != "cmd3" {
			t.Errorf("expected prompt=cmd3, got %s", e.prompt)
		}
		if e.cursor != 4 {
			t.Errorf("expected cursor=4, got %d", e.cursor)
		}

		e.historyUp()
		if e.histIdx != 1 {
			t.Errorf("expected histIdx=1, got %d", e.histIdx)
		}
		if e.prompt != "cmd2" {
			t.Errorf("expected prompt=cmd2, got %s", e.prompt)
		}
	})

	t.Run("historyDown navigates correctly", func(t *testing.T) {
		e := &tuiLineEditor{
			stdout:  bufio.NewWriter(io.Discard),
			history: []string{"cmd1", "cmd2", "cmd3"},
			histIdx: 0,
		}
		e.historyDown()
		if e.histIdx != 1 {
			t.Errorf("expected histIdx=1, got %d", e.histIdx)
		}
		if e.prompt != "cmd2" {
			t.Errorf("expected prompt=cmd2, got %s", e.prompt)
		}

		e.historyDown()
		if e.histIdx != 2 {
			t.Errorf("expected histIdx=2, got %d", e.histIdx)
		}
		if e.prompt != "cmd3" {
			t.Errorf("expected prompt=cmd3, got %s", e.prompt)
		}

		// Should go back to empty new prompt
		e.historyDown()
		if e.histIdx != -1 {
			t.Errorf("expected histIdx=-1, got %d", e.histIdx)
		}
		if e.prompt != "" {
			t.Errorf("expected empty prompt, got %s", e.prompt)
		}
	})
}
