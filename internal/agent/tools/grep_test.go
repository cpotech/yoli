package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrep_DefinitionSchema(t *testing.T) {
	def := NewGrepTool(t.TempDir()).Definition()
	if def.Name != "Grep" {
		t.Fatalf("name = %q", def.Name)
	}
	props, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing")
	}
	for _, k := range []string{"pattern", "path", "glob", "type", "output_mode", "multiline"} {
		if _, ok := props[k].(map[string]any); !ok {
			t.Fatalf("prop %s missing", k)
		}
	}
	for _, k := range []string{"-i", "-n"} {
		if p, ok := props[k].(map[string]any); !ok || p["type"] != "boolean" {
			t.Fatalf("prop %s schema: %v", k, props[k])
		}
	}
	if p, ok := props["head_limit"].(map[string]any); !ok {
		t.Fatalf("prop head_limit missing")
	} else if p["type"] != "integer" && p["type"] != "number" {
		t.Fatalf("prop head_limit type: %v", p["type"])
	}
	required, _ := def.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "pattern" {
		t.Fatalf("required = %v", required)
	}
}

func gWriteFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestGrep_FilesWithMatchesDefault(t *testing.T) {
	root := rootDir(t)
	gWriteFile(t, root, "a.go", "package x\nfunc Foo() {}\n")
	gWriteFile(t, root, "b.go", "package x\nfunc Bar() {}\n")
	gWriteFile(t, root, "c.go", "package x\nfunc Foo() {}\n")
	tool := NewGrepTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"pattern": "Foo"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	if lines[0] != "a.go" || lines[1] != "c.go" {
		t.Fatalf("order = %v", lines)
	}
}

func TestGrep_ContentModeIncludesLineNumbersWhenNRequested(t *testing.T) {
	root := rootDir(t)
	gWriteFile(t, root, "a.go", "alpha\nbeta target\ngamma\n")
	tool := NewGrepTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "target", "output_mode": "content", "-n": true,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(got, "a.go:2:") || !strings.Contains(got, "beta target") {
		t.Fatalf("got = %q", got)
	}
}

func TestGrep_CountModeReturnsPerFileCounts(t *testing.T) {
	root := rootDir(t)
	gWriteFile(t, root, "a.go", "x x x\n")
	gWriteFile(t, root, "b.go", "x\n")
	tool := NewGrepTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "x", "output_mode": "count",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(got, "a.go:3") || !strings.Contains(got, "b.go:1") {
		t.Fatalf("got = %q", got)
	}
}

func TestGrep_CaseInsensitiveFlagMatches(t *testing.T) {
	root := rootDir(t)
	gWriteFile(t, root, "a.go", "HELLO\n")
	tool := NewGrepTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "hello", "-i": true,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(got) != "a.go" {
		t.Fatalf("got = %q", got)
	}
}

func TestGrep_GlobFilterRestrictsFiles(t *testing.T) {
	root := rootDir(t)
	gWriteFile(t, root, "a.go", "needle\n")
	gWriteFile(t, root, "a.md", "needle\n")
	tool := NewGrepTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "needle", "glob": "*.go",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(got) != "a.go" {
		t.Fatalf("got = %q", got)
	}
}

func TestGrep_MultilineDotMatchesNewlines(t *testing.T) {
	root := rootDir(t)
	gWriteFile(t, root, "a.go", "func Foo() {\n  return 1\n}\n")
	tool := NewGrepTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "func Foo.*return", "multiline": true,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(got) != "a.go" {
		t.Fatalf("got = %q", got)
	}
}

func TestGrep_HeadLimitTruncatesOutput(t *testing.T) {
	root := rootDir(t)
	for i := 0; i < 10; i++ {
		gWriteFile(t, root, filepath.Join("d", string(rune('a'+i))+".txt"), "hit\n")
	}
	tool := NewGrepTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "hit", "head_limit": 3,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %v", lines)
	}
}

func TestGrep_InvalidRegexReturnsError(t *testing.T) {
	tool := NewGrepTool(rootDir(t))
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"pattern": "(unclosed"}))
	if err == nil {
		t.Fatalf("want error")
	}
}

func TestGrep_RejectsPathOutsideCwd(t *testing.T) {
	tool := NewGrepTool(rootDir(t))
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "x", "path": filepath.Join("..", "escape"),
	}))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "outside") {
		t.Fatalf("err = %v", err)
	}
}

func TestGrep_SkipsBinaryFiles(t *testing.T) {
	root := rootDir(t)
	gWriteFile(t, root, "text.go", "needle\n")
	bin := append([]byte("needle"), 0x00, 0x01, 0x02)
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), bin, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewGrepTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"pattern": "needle"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(got) != "text.go" {
		t.Fatalf("got = %q", got)
	}
}
