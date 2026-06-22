package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestReadFile_DefinitionSchema(t *testing.T) {
	tool := NewReadFileTool(t.TempDir())
	def := tool.Definition()
	if def.Name != "Read" {
		t.Fatalf("name = %q", def.Name)
	}
	if def.Parameters["type"] != "object" {
		t.Fatalf("type missing")
	}
	props, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing")
	}
	path, ok := props["path"].(map[string]any)
	if !ok || path["type"] != "string" {
		t.Fatalf("path schema wrong: %v", props)
	}
	required, _ := def.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "path" {
		t.Fatalf("required = %v", required)
	}
}

func TestReadFile_ReadsRelative(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello yoli"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewReadFileTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]string{"path": "hello.txt"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "hello yoli" {
		t.Fatalf("got %q", got)
	}
}

func TestReadFile_RejectsAbsoluteOutside(t *testing.T) {
	root := rootDir(t)
	outside := rootDir(t)
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewReadFileTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"path": filepath.Join(outside, "secret.txt"),
	}))
	if err == nil {
		t.Fatalf("want error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "outside") {
		t.Fatalf("error %q", err.Error())
	}
}

func TestReadFile_RejectsRelativeTraversal(t *testing.T) {
	tool := NewReadFileTool(rootDir(t))
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"path": filepath.Join("..", "escape.txt"),
	}))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "outside") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadFile_WithHashesAnnotatesLines(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "h.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewReadFileTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path": "h.txt", "with_hashes": true,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	wantAlpha := "1:" + HashLine("alpha") + "|alpha"
	wantBeta := "2:" + HashLine("beta") + "|beta"
	if !strings.Contains(got, wantAlpha) || !strings.Contains(got, wantBeta) {
		t.Fatalf("annotation = %q", got)
	}
}

func TestReadFile_WithHashesFalseReturnsRaw(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "h.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewReadFileTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"path": "h.txt"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "alpha\n" {
		t.Fatalf("expected raw output, got %q", got)
	}
}

func TestReadFile_MissingFileMentionsPath(t *testing.T) {
	tool := NewReadFileTool(rootDir(t))
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]string{"path": "missing.txt"}))
	if err == nil || !strings.Contains(err.Error(), "missing.txt") {
		t.Fatalf("err = %v", err)
	}
}

// TestReadFile_AllowRootPermitsOutsideRead verifies that a path inside an
// explicitly allowlisted read-only root (YOLI_READ_ALLOW) is readable even
// though it lies outside the working directory. This is how Yolium exposes
// bundled skill directories (/opt/<agent>-skills) to the Read tool.
func TestReadFile_AllowRootPermitsOutsideRead(t *testing.T) {
	root := rootDir(t)
	allowed := rootDir(t)
	if err := os.WriteFile(filepath.Join(allowed, "SKILL.md"), []byte("# skill"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("YOLI_READ_ALLOW", allowed)
	tool := NewReadFileTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"path": filepath.Join(allowed, "SKILL.md"),
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != "# skill" {
		t.Fatalf("got %q", got)
	}
}

// TestReadFile_AllowRootStillRejectsUnlisted verifies that paths outside both
// the working directory and every allowlisted root remain rejected.
func TestReadFile_AllowRootStillRejectsUnlisted(t *testing.T) {
	root := rootDir(t)
	allowed := rootDir(t)
	forbidden := rootDir(t)
	if err := os.WriteFile(filepath.Join(forbidden, "secret.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("YOLI_READ_ALLOW", allowed)
	tool := NewReadFileTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"path": filepath.Join(forbidden, "secret.txt"),
	}))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "outside") {
		t.Fatalf("err = %v", err)
	}
}
