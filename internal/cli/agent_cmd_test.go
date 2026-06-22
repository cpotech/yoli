package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoli/internal/agent/tools"
	"yoli/internal/ai"
)

func TestAgent_ModelPassthrough(t *testing.T) {
	slug := "anthropic/claude-sonnet-4-5-20250929"
	t.Setenv("AGENT_MODEL", "")
	if got := resolveModel(slug); got != slug {
		t.Fatalf("resolveModel(%q) = %q, want unchanged passthrough", slug, got)
	}
}

func TestAgent_ModelFromEnvPassthrough(t *testing.T) {
	slug := "qwen/qwen3-coder"
	t.Setenv("AGENT_MODEL", slug)
	if got := resolveModel(""); got != slug {
		t.Fatalf("resolveModel(\"\") with AGENT_MODEL=%q = %q", slug, got)
	}
}

func TestAgent_ModelEmptyDefaultsToOpenRouterFree(t *testing.T) {
	t.Setenv("AGENT_MODEL", "")
	if got := resolveModel(""); got != "openrouter/free" {
		t.Fatalf("resolveModel(\"\") = %q, want openrouter/free", got)
	}
}

func TestAgent_NoPromptSourcePrintsUsage(t *testing.T) {
	r := runCli(t, []string{"agent"}, runOpts{
		extraEnv: map[string]string{"OPENROUTER_API_KEY": "k"},
	})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(strings.ToLower(r.stderr), "usage") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestAgent_MissingAPIKeyErrors(t *testing.T) {
	r := runCli(t, []string{"agent", "--prompt", "hi"}, runOpts{})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(r.stderr, "OPENROUTER_API_KEY") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestAgent_PromptFromAGENT_PROMPT_FILE(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(pf, []byte("hello agent"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_PROMPT_FILE", pf)
	t.Setenv("AGENT_PROMPT", "")
	got, err := readPrompt(agentFlags{})
	if err != nil {
		t.Fatalf("readPrompt: %v", err)
	}
	if got != "hello agent" {
		t.Fatalf("readPrompt = %q, want %q", got, "hello agent")
	}
}

func TestAgent_PromptFromAGENT_PROMPTBase64(t *testing.T) {
	t.Setenv("AGENT_PROMPT_FILE", "")
	t.Setenv("AGENT_PROMPT", "aGVsbG8gYmFzZTY0") // "hello base64"
	got, err := readPrompt(agentFlags{})
	if err != nil {
		t.Fatalf("readPrompt: %v", err)
	}
	if got != "hello base64" {
		t.Fatalf("readPrompt = %q, want %q", got, "hello base64")
	}
}

func TestAgent_UsageListsAgentSubcommand(t *testing.T) {
	r := runCli(t, nil, runOpts{})
	if !strings.Contains(r.stderr, "agent") {
		t.Fatalf("usage missing agent: %q", r.stderr)
	}
}

// --- filterAgentTools ---

func TestFilterAgentTools_NilWhitelistReturnsAll(t *testing.T) {
	ts := tools.DefaultTools("/tmp")
	got := filterAgentTools(ts, nil)
	if len(got) != len(ts) {
		t.Fatalf("len = %d, want %d", len(got), len(ts))
	}
}

func TestFilterAgentTools_EmptyWhitelistReturnsAll(t *testing.T) {
	ts := tools.DefaultTools("/tmp")
	got := filterAgentTools(ts, []string{})
	if len(got) != len(ts) {
		t.Fatalf("len = %d, want %d", len(got), len(ts))
	}
}

func TestFilterAgentTools_KeepsWhitelistedOnly(t *testing.T) {
	ts := tools.DefaultTools("/tmp")
	got := filterAgentTools(ts, []string{"Read", "Bash"})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	for _, tl := range got {
		n := tl.Definition().Name
		if n != "Read" && n != "Bash" {
			t.Fatalf("unexpected tool: %s", n)
		}
	}
}

func TestFilterAgentTools_IgnoresUnknownNames(t *testing.T) {
	ts := tools.DefaultTools("/tmp")
	got := filterAgentTools(ts, []string{"Read", "nonexistent_tool"})
	if len(got) != 1 || got[0].Definition().Name != "Read" {
		t.Fatalf("got %d tools, want 1 (Read)", len(got))
	}
}

func TestResolveRepoPath_ReturnsCwd(t *testing.T) {
	// Yoli's working directory is whatever it was launched in. Env vars
	// from the embedding host (e.g., PROJECT_DIR) must not influence this
	// — the embedder is responsible for setting cwd correctly.
	dir := t.TempDir()
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	// Resolve the symlink the same way os.Getwd() does after chdir
	// (macOS/Linux differ on /tmp -> /private/tmp etc.).
	wantCwd, _ := os.Getwd()
	t.Setenv("PROJECT_DIR", "/should/be/ignored")
	t.Setenv("WORKTREE_REPO_PATH", "/also/ignored")
	if got := resolveRepoPath(); got != wantCwd {
		t.Fatalf("resolveRepoPath() = %q, want cwd %q", got, wantCwd)
	}
}

func TestFilterAgentTools_NeverReturnsAskQuestion(t *testing.T) {
	// Create a minimal tool that mimics ask_question
	askQ := &fakeTool{name: "ask_question"}
	other := &fakeTool{name: "Read"}
	got := filterAgentTools([]tools.Tool{askQ, other}, nil)
	for _, tl := range got {
		if tl.Definition().Name == "ask_question" {
			t.Fatalf("ask_question should never be returned")
		}
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

type fakeTool struct {
	name   string
	result string
	err    error
}

func (f *fakeTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{Name: f.name, Parameters: map[string]any{"type": "object"}}
}

func (f *fakeTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return f.result, f.err
}
