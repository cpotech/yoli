package yolium

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitInRepo runs a git subcommand inside root and fatally fails the test
// on non-zero exit. Setup helper, mirrors the one in agent/tools.
func gitInRepo(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup: git %v: %v\n%s", args, err, out)
	}
}

// initGuardGitRepo prepares a deterministic git repo at root with one
// committed file. Identity and signing are pinned so commits succeed in
// CI environments without a global git config.
func initGuardGitRepo(t *testing.T, root string) {
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
	writeFile(t, root, "base.txt", "base\n")
	gitInRepo(t, root, "add", "base.txt")
	gitInRepo(t, root, "commit", "-q", "-m", "base")
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestDirtyPaths_EmptyAndWhitespace(t *testing.T) {
	for _, in := range []string{"", "\n", "  \n\n"} {
		if got := DirtyPaths(in); got != nil {
			t.Errorf("DirtyPaths(%q)=%v want nil", in, got)
		}
	}
}

func TestDirtyPaths_ReportsTrackedStagedAndUntracked(t *testing.T) {
	in := " M a.go\nA  b.go\n?? c.txt\n"
	got := DirtyPaths(in)
	want := []string{"a.go", "b.go", "c.txt"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestDirtyPaths_ExcludesHarnessPaths(t *testing.T) {
	in := strings.Join([]string{
		"?? .yolium/summary.md",
		" M .yolium.json",
		"?? .yolium-code-agent-instructions.md",
		"?? .yolium-plan-agent-instructions.md",
		" M AGENTS.md",
	}, "\n") + "\n"
	if got := DirtyPaths(in); len(got) != 0 {
		t.Fatalf("harness paths must be excluded; got %v", got)
	}
}

func TestDirtyPaths_AgentsMdExclusionIsRootExact(t *testing.T) {
	in := " M docs/AGENTS.md\n M AGENTS.md.bak\n"
	got := DirtyPaths(in)
	if len(got) != 2 {
		t.Fatalf("only root AGENTS.md is excluded; got %v", got)
	}
}

func TestDirtyPaths_Renames(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"R  old.go -> new.go", []string{"new.go"}},
		{"R  .yolium/a -> .yolium/b", nil},
		{"R  .yolium/a -> src/b.go", []string{"src/b.go"}},
	}
	for _, c := range cases {
		got := DirtyPaths(c.line + "\n")
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v want %v", c.line, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("%q: got[%d]=%q want %q", c.line, i, got[i], c.want[i])
			}
		}
	}
}

func TestDirtyPaths_UnquotesGitQuotedPaths(t *testing.T) {
	in := "?? \"tab\\there.txt\"\n?? \".yolium\\ttrick.md\"\n"
	got := DirtyPaths(in)
	if len(got) != 1 || got[0] != "tab\there.txt" {
		t.Fatalf("got %v want [tab\\there.txt] (quoted .yolium path excluded)", got)
	}
}

func TestFormatDirtyGuardError_CapsListedPaths(t *testing.T) {
	paths := make([]string, 25)
	for i := range paths {
		paths[i] = "f" + strings.Repeat("x", i) + ".go"
	}
	err := formatDirtyGuardError(paths)
	msg := err.Error()
	if !strings.Contains(msg, "25 uncommitted change(s)") {
		t.Errorf("message must report total count: %q", msg)
	}
	if !strings.Contains(msg, "and 5 more") {
		t.Errorf("message must note the overflow: %q", msg)
	}
	if !strings.Contains(msg, "git add") || !strings.Contains(msg, "git commit") {
		t.Errorf("message must instruct how to recover: %q", msg)
	}
	if strings.Contains(msg, paths[24]) {
		t.Errorf("paths beyond the cap must not be listed: %q", msg)
	}
}

func TestWorktreeDirtyPaths_NonGitDirFailsOpen(t *testing.T) {
	if _, ok := worktreeDirtyPaths(context.Background(), t.TempDir()); ok {
		t.Fatal("non-git dir must fail open (ok=false)")
	}
	if _, ok := worktreeDirtyPaths(context.Background(), ""); ok {
		t.Fatal("empty repoPath must fail open (ok=false)")
	}
}

func TestWorktreeDirtyPaths_ReportsRepoState(t *testing.T) {
	root := t.TempDir()
	initGuardGitRepo(t, root)

	paths, ok := worktreeDirtyPaths(context.Background(), root)
	if !ok || len(paths) != 0 {
		t.Fatalf("clean repo: ok=%v paths=%v", ok, paths)
	}

	writeFile(t, root, "work.go", "package work\n")
	writeFile(t, root, ".yolium/summary.md", "noise\n")
	paths, ok = worktreeDirtyPaths(context.Background(), root)
	if !ok || len(paths) != 1 || paths[0] != "work.go" {
		t.Fatalf("dirty repo: ok=%v paths=%v want [work.go]", ok, paths)
	}
}

func TestWorktreeBlocksComplete(t *testing.T) {
	root := t.TempDir()
	initGuardGitRepo(t, root)

	if WorktreeBlocksComplete(context.Background(), root) {
		t.Fatal("clean repo must not block")
	}
	writeFile(t, root, "work.go", "package work\n")
	if !WorktreeBlocksComplete(context.Background(), root) {
		t.Fatal("dirty repo must block")
	}
	t.Setenv("YOLI_COMPLETE_GUARD", "off")
	if WorktreeBlocksComplete(context.Background(), root) {
		t.Fatal("kill switch must disable the guard")
	}
}
