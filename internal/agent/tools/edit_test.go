package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEdit_DefinitionSchema(t *testing.T) {
	def := NewEditTool(t.TempDir()).Definition()
	if def.Name != "Edit" {
		t.Fatalf("name = %q", def.Name)
	}
	if def.Parameters["type"] != "object" {
		t.Fatalf("type missing")
	}
	props, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing")
	}
	for _, k := range []string{"path", "old_string", "new_string"} {
		p, ok := props[k].(map[string]any)
		if !ok || p["type"] != "string" {
			t.Fatalf("prop %s schema: %v", k, props[k])
		}
	}
	if ra, ok := props["replace_all"].(map[string]any); !ok || ra["type"] != "boolean" {
		t.Fatalf("replace_all schema: %v", props["replace_all"])
	}
	required, _ := def.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "path" {
		t.Fatalf("required = %v (want [\"path\"])", required)
	}
	for _, k := range []string{"line", "hash", "from_line", "from_hash", "to_line", "to_hash", "new_text"} {
		if _, ok := props[k].(map[string]any); !ok {
			t.Fatalf("missing hashline prop %q in schema", k)
		}
	}
}

func TestEdit_ReplacesUniqueOccurrence(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha BETA gamma"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "old_string": "BETA", "new_string": "DELTA",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(got, "f.txt") || !strings.Contains(got, "1") {
		t.Fatalf("result = %q", got)
	}
	body, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(body) != "alpha DELTA gamma" {
		t.Fatalf("body = %q", body)
	}
}

func TestEdit_ErrorsWhenOldStringMissing(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "old_string": "world", "new_string": "earth",
	}))
	if err == nil {
		t.Fatalf("want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "f.txt") || !strings.Contains(strings.ToLower(msg), "not found") {
		t.Fatalf("err = %q", msg)
	}
}

func TestEdit_ErrorsWhenOldStringNotUnique(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("dup dup dup"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "old_string": "dup", "new_string": "DUP",
	}))
	if err == nil {
		t.Fatalf("want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "3") || !strings.Contains(msg, "replace_all") {
		t.Fatalf("err = %q", msg)
	}
}

func TestEdit_ReplaceAllRewritesEveryOccurrence(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("dup dup dup"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "old_string": "dup", "new_string": "X", "replace_all": true,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(got, "3") {
		t.Fatalf("result = %q", got)
	}
	body, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(body) != "X X X" {
		t.Fatalf("body = %q", body)
	}
}

func TestEdit_ErrorsWhenOldEqualsNew(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path": "f.txt", "old_string": "hello", "new_string": "hello",
	}))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "identical") {
		t.Fatalf("err = %v", err)
	}
}

func TestEdit_RejectsPathOutsideCwd(t *testing.T) {
	tool := NewEditTool(rootDir(t))
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path": filepath.Join("..", "escape.txt"), "old_string": "a", "new_string": "b",
	}))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "outside") {
		t.Fatalf("err = %v", err)
	}
}

func TestEdit_HashlineSingleLineReplace(t *testing.T) {
	root := rootDir(t)
	original := "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(original), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	betaHash := HashLine("beta")
	tool := NewEditTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path":     "f.txt",
		"line":     2,
		"hash":     betaHash,
		"new_text": "BETA",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(got, "line 2") {
		t.Fatalf("result missing line info: %q", got)
	}
	body, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(body) != "alpha\nBETA\ngamma\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestEdit_HashlineDetectsDrift(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path":     "f.txt",
		"line":     2,
		"hash":     "000", // intentionally wrong
		"new_text": "BETA",
	}))
	if err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("expected drift error, got %v", err)
	}
}

func TestEdit_HashlineOutOfRange(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path":     "f.txt",
		"line":     5,
		"hash":     HashLine("alpha"),
		"new_text": "X",
	}))
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected out-of-range error, got %v", err)
	}
}

func TestEdit_HashlineMultiLineNewText(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path":     "f.txt",
		"line":     2,
		"hash":     HashLine("beta"),
		"new_text": "BETA1\nBETA2",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(body) != "alpha\nBETA1\nBETA2\ngamma\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestEdit_HashlineRangeReplace(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha\nbeta\ngamma\ndelta\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path":      "f.txt",
		"from_line": 2,
		"from_hash": HashLine("beta"),
		"to_line":   3,
		"to_hash":   HashLine("gamma"),
		"new_text":  "MIDDLE",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(body) != "alpha\nMIDDLE\ndelta\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestEdit_HashlineRangeDriftAtBoundary(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	// from_hash is wrong
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path":      "f.txt",
		"from_line": 2,
		"from_hash": "zzz",
		"to_line":   3,
		"to_hash":   HashLine("c"),
		"new_text":  "X",
	}))
	if err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("expected drift error, got %v", err)
	}
}

func TestEdit_HashlineRangeBackwards(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path":      "f.txt",
		"from_line": 3,
		"from_hash": HashLine("c"),
		"to_line":   2,
		"to_hash":   HashLine("b"),
		"new_text":  "X",
	}))
	if err == nil || !strings.Contains(err.Error(), "> to_line") {
		t.Fatalf("expected ordering error, got %v", err)
	}
}

func TestEdit_RejectsMixedModes(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path":       "f.txt",
		"old_string": "alpha",
		"new_string": "beta",
		"line":       1,
		"hash":       HashLine("alpha"),
		"new_text":   "x",
	}))
	if err == nil || !strings.Contains(err.Error(), "combine str_replace and hashline") {
		t.Fatalf("expected mixed-mode error, got %v", err)
	}
}

func TestEdit_RejectsMixedSingleAndRange(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path":      "f.txt",
		"line":      1,
		"hash":      HashLine("a"),
		"from_line": 1,
		"to_line":   2,
		"new_text":  "x",
	}))
	if err == nil || !strings.Contains(err.Error(), "single-line and range") {
		t.Fatalf("expected mixed-line-mode error, got %v", err)
	}
}

func TestEdit_StrReplaceWithoutOldStringErrors(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"path": "f.txt"}))
	if err == nil || !strings.Contains(err.Error(), "missing edit parameters") {
		t.Fatalf("expected missing-params error, got %v", err)
	}
}

func TestEdit_PreservesTrailingNewlineAndUTF8(t *testing.T) {
	root := rootDir(t)
	original := "héllo αβγ\nmiddle\nend\n"
	if err := os.WriteFile(filepath.Join(root, "u.txt"), []byte(original), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewEditTool(root)
	if _, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"path": "u.txt", "old_string": "middle", "new_string": "centre",
	})); err != nil {
		t.Fatalf("run: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(root, "u.txt"))
	want := "héllo αβγ\ncentre\nend\n"
	if string(body) != want {
		t.Fatalf("body = %q want %q", body, want)
	}
}
