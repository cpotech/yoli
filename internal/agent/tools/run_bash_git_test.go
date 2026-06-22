package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// gitInRepo runs a git subcommand inside root and fatally fails the
// test on non-zero exit. Used for deterministic setup that is NOT the
// subject of the test (i.e. wherever we need a working git state but
// aren't exercising run_bash itself).
func gitInRepo(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup: git %v: %v\n%s", args, err, out)
	}
}

// initBashGitRepo prepares a deterministic git repo at root for the
// run_bash git integration tests below. Identity and signing are pinned
// so commits succeed in CI environments without a global git config.
func initBashGitRepo(t *testing.T, root string) {
	t.Helper()
	for _, c := range [][]string{
		{"init", "-q"},
		{"symbolic-ref", "HEAD", "refs/heads/main"},
		{"config", "user.email", "yoli-test@example.com"},
		{"config", "user.name", "Yoli Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitInRepo(t, root, c...)
	}
}

// TestRunBash_GitStatusInCleanRepo verifies that the agent can read
// repo state via run_bash now that the dedicated git_status tool is
// gone.
func TestRunBash_GitStatusInCleanRepo(t *testing.T) {
	root := rootDir(t)
	initBashGitRepo(t, root)
	tool := NewRunBashTool(root)
	out, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"command": "git status --porcelain",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "exit code: 0") {
		t.Fatalf("want exit 0, got:\n%s", out)
	}
	// Porcelain output should be empty for a clean repo; the only
	// content in `out` is the exit-code suffix.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected only the exit-code line, got:\n%s", out)
	}
}

// TestRunBash_GitCommitFlow verifies the full add → commit → log
// workflow through run_bash, exercising the path that previously lived
// in the dedicated git_commit tool.
func TestRunBash_GitCommitFlow(t *testing.T) {
	root := rootDir(t)
	initBashGitRepo(t, root)
	writeFile(t, root, "hello.txt", "hi\n")
	tool := NewRunBashTool(root)

	commit, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"command": "git add hello.txt && git commit -q -m 'feat: add hello'",
	}))
	if err != nil {
		t.Fatalf("commit run: %v", err)
	}
	if !strings.Contains(commit, "exit code: 0") {
		t.Fatalf("commit failed:\n%s", commit)
	}

	log, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"command": "git log --oneline",
	}))
	if err != nil {
		t.Fatalf("log run: %v", err)
	}
	if !strings.Contains(log, "feat: add hello") {
		t.Fatalf("commit missing from log:\n%s", log)
	}
	if !strings.Contains(log, "exit code: 0") {
		t.Fatalf("log non-zero:\n%s", log)
	}
}

// TestRunBash_GitDiffShowsUnstagedChanges confirms read-only diff
// inspection works through run_bash.
func TestRunBash_GitDiffShowsUnstagedChanges(t *testing.T) {
	root := rootDir(t)
	initBashGitRepo(t, root)
	writeFile(t, root, "f.txt", "original\n")
	gitInRepo(t, root, "add", "f.txt")
	gitInRepo(t, root, "commit", "-q", "-m", "seed")
	writeFile(t, root, "f.txt", "changed\n")

	tool := NewRunBashTool(root)
	out, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"command": "git diff -- f.txt",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "-original") || !strings.Contains(out, "+changed") {
		t.Fatalf("diff missing expected hunks:\n%s", out)
	}
	if !strings.Contains(out, "exit code: 0") {
		t.Fatalf("non-zero exit:\n%s", out)
	}
}
