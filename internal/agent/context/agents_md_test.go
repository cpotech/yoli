package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rootDir returns t.TempDir() with symlinks evaluated so that paths
// returned by LoadAgentsMd (which calls filepath.Abs) compare equal on
// platforms like macOS where /var is symlinked to /private/var.
func rootDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("evalsymlinks: %v", err)
	}
	return root
}

func mkGit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkgit: %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLoadAgentsMd_EmptyWhenNoneOnPath(t *testing.T) {
	root := rootDir(t)
	mkGit(t, root)
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := LoadAgentsMd(LoadAgentsMdOptions{Cwd: sub})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestLoadAgentsMd_LoadsSingleFromCwd(t *testing.T) {
	root := rootDir(t)
	mkGit(t, root)
	writeFile(t, filepath.Join(root, "AGENTS.md"), "root rules")
	got, err := LoadAgentsMd(LoadAgentsMdOptions{Cwd: root})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "root rules" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadAgentsMd_ConcatenatesRootFirst(t *testing.T) {
	root := rootDir(t)
	mkGit(t, root)
	sub := filepath.Join(root, "a", "b")
	writeFile(t, filepath.Join(root, "AGENTS.md"), "ROOT")
	writeFile(t, filepath.Join(root, "a", "AGENTS.md"), "MIDDLE")
	writeFile(t, filepath.Join(sub, "AGENTS.md"), "LEAF")
	got, err := LoadAgentsMd(LoadAgentsMdOptions{Cwd: sub})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	idxRoot := strings.Index(got, "ROOT")
	idxMid := strings.Index(got, "MIDDLE")
	idxLeaf := strings.Index(got, "LEAF")
	if idxRoot < 0 || idxMid <= idxRoot || idxLeaf <= idxMid {
		t.Fatalf("ordering wrong: %q", got)
	}
}

func TestLoadAgentsMd_StopsAtGitDirectory(t *testing.T) {
	root := rootDir(t)
	project := filepath.Join(root, "project")
	mkGit(t, project)
	writeFile(t, filepath.Join(root, "AGENTS.md"), "ABOVE_GIT")
	writeFile(t, filepath.Join(project, "AGENTS.md"), "AT_GIT")
	got, err := LoadAgentsMd(LoadAgentsMdOptions{Cwd: project})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, "AT_GIT") {
		t.Fatalf("missing AT_GIT: %q", got)
	}
	if strings.Contains(got, "ABOVE_GIT") {
		t.Fatalf("should not include ABOVE_GIT: %q", got)
	}
}

func TestLoadAgentsMd_StopsAtFilesystemRoot(t *testing.T) {
	root := rootDir(t)
	sub := filepath.Join(root, "a", "b")
	writeFile(t, filepath.Join(sub, "AGENTS.md"), "LEAF")
	got, err := LoadAgentsMd(LoadAgentsMdOptions{Cwd: sub})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, "LEAF") {
		t.Fatalf("missing LEAF: %q", got)
	}
}

func TestLoadAgentsMd_AtExactGitRoot(t *testing.T) {
	root := rootDir(t)
	mkGit(t, root)
	writeFile(t, filepath.Join(root, "AGENTS.md"), "GIT_ROOT_AGENTS")
	sub := filepath.Join(root, "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := LoadAgentsMd(LoadAgentsMdOptions{Cwd: sub})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(got, "GIT_ROOT_AGENTS") {
		t.Fatalf("missing content: %q", got)
	}
}

func TestLoadAgentsMd_SeparatesBlocksWithBlankLine(t *testing.T) {
	root := rootDir(t)
	mkGit(t, root)
	sub := filepath.Join(root, "a")
	writeFile(t, filepath.Join(root, "AGENTS.md"), "first")
	writeFile(t, filepath.Join(sub, "AGENTS.md"), "second")
	got, err := LoadAgentsMd(LoadAgentsMdOptions{Cwd: sub})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "first\n\nsecond" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadAgentsMd_TrimsTrailingWhitespace(t *testing.T) {
	root := rootDir(t)
	mkGit(t, root)
	sub := filepath.Join(root, "a")
	writeFile(t, filepath.Join(root, "AGENTS.md"), "first   \n\n\n")
	writeFile(t, filepath.Join(sub, "AGENTS.md"), "second\n\n")
	got, err := LoadAgentsMd(LoadAgentsMdOptions{Cwd: sub})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "first\n\nsecond" {
		t.Fatalf("got %q", got)
	}
}
