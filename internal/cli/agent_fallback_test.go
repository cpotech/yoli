package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadExistingSummary_ReturnsFirstLineWhenPresent(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".yolium"), 0o755)
	os.WriteFile(filepath.Join(dir, ".yolium", "summary.md"), []byte("Implemented the feature\nMore details here"), 0o644)
	got, ok := readExistingSummary(dir)
	if !ok {
		t.Fatal("expected ok=true when summary exists")
	}
	if got != "Implemented the feature" {
		t.Fatalf("got %q, want %q", got, "Implemented the feature")
	}
}

func TestReadExistingSummary_ReturnsFalseWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	got, ok := readExistingSummary(dir)
	if ok {
		t.Fatalf("expected ok=false for missing summary, got %q", got)
	}
}

func TestReadExistingSummary_SkipsLeadingBlankLines(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".yolium"), 0o755)
	os.WriteFile(filepath.Join(dir, ".yolium", "summary.md"), []byte("\n\n\nActual content"), 0o644)
	got, ok := readExistingSummary(dir)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "Actual content" {
		t.Fatalf("got %q, want %q", got, "Actual content")
	}
}

func TestReadExistingSummary_ReturnsFalseOnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".yolium"), 0o755)
	os.WriteFile(filepath.Join(dir, ".yolium", "summary.md"), []byte(""), 0o644)
	if _, ok := readExistingSummary(dir); ok {
		t.Fatal("expected ok=false for empty file")
	}
}

func TestReadExistingSummary_ReturnsFalseOnWhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".yolium"), 0o755)
	os.WriteFile(filepath.Join(dir, ".yolium", "summary.md"), []byte("   \n  \n\t\n"), 0o644)
	if _, ok := readExistingSummary(dir); ok {
		t.Fatal("expected ok=false for whitespace-only file")
	}
}

func TestReadExistingSummary_LeadingWhitespaceBeforeContent(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".yolium"), 0o755)
	os.WriteFile(filepath.Join(dir, ".yolium", "summary.md"), []byte("   \n  \ncontent"), 0o644)
	got, ok := readExistingSummary(dir)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "content" {
		t.Fatalf("got %q, want %q", got, "content")
	}
}

func TestRemoveStaleSummaryFile_RemovesExisting(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".yolium"), 0o755)
	path := filepath.Join(dir, ".yolium", "summary.md")
	os.WriteFile(path, []byte("Stale summary from prior agent"), 0o644)

	removeStaleSummaryFile(dir)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected summary.md to be removed; stat err = %v", err)
	}
}

func TestRemoveStaleSummaryFile_NoopWhenAbsent(t *testing.T) {
	// Should not panic or error when the file or .yolium dir doesn't
	// exist (fresh worktree, first run).
	dir := t.TempDir()
	removeStaleSummaryFile(dir) // .yolium doesn't exist
	os.MkdirAll(filepath.Join(dir, ".yolium"), 0o755)
	removeStaleSummaryFile(dir) // .yolium exists, summary.md doesn't
}
