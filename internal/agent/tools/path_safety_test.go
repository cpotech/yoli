package tools

import (
	"path/filepath"
	"strings"
	"testing"
)

// rootDir returns t.TempDir() with symlinks evaluated, matching the TS
// test's realpathSync call (macOS resolves /var → /private/var).
func rootDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("evalsymlinks: %v", err)
	}
	return root
}

func TestResolveInside_RelativePathReturnsAbsolute(t *testing.T) {
	root := rootDir(t)
	got, err := ResolveInside(root, "file.txt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(root, "file.txt")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveInside_NestedRelativePath(t *testing.T) {
	root := rootDir(t)
	got, err := ResolveInside(root, filepath.Join("a", "b", "c.txt"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(root, "a", "b", "c.txt")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveInside_RejectsParentEscape(t *testing.T) {
	root := rootDir(t)
	_, err := ResolveInside(root, filepath.Join("..", "outside.txt"))
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "outside") {
		t.Fatalf("error %q should mention 'outside'", err.Error())
	}
}

func TestResolveInside_RejectsAbsoluteOutside(t *testing.T) {
	root := rootDir(t)
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if _, err := ResolveInside(root, outside); err == nil {
		t.Fatalf("want error, got nil")
	}
}

func TestResolveInside_AcceptsAbsoluteInside(t *testing.T) {
	root := rootDir(t)
	inside := filepath.Join(root, "nested", "file.txt")
	got, err := ResolveInside(root, inside)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != inside {
		t.Fatalf("got %q, want %q", got, inside)
	}
}

func TestResolveInside_NormalisesRedundantSegments(t *testing.T) {
	root := rootDir(t)
	sep := string(filepath.Separator)
	got, err := ResolveInside(root, "."+sep+"a"+sep+"."+sep+"b"+sep)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := filepath.Join(root, "a", "b")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
