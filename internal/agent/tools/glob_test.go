package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlob_DefinitionSchema(t *testing.T) {
	def := NewGlobTool(t.TempDir()).Definition()
	if def.Name != "Glob" {
		t.Fatalf("name = %q", def.Name)
	}
	props, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing")
	}
	if p, ok := props["pattern"].(map[string]any); !ok || p["type"] != "string" {
		t.Fatalf("pattern schema: %v", props["pattern"])
	}
	if p, ok := props["path"].(map[string]any); !ok || p["type"] != "string" {
		t.Fatalf("path schema: %v", props["path"])
	}
	required, _ := def.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "pattern" {
		t.Fatalf("required = %v", required)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestGlob_MatchesSimpleSuffix(t *testing.T) {
	root := rootDir(t)
	writeFile(t, root, "a.go", "")
	writeFile(t, root, "b.go", "")
	writeFile(t, root, "c.txt", "")
	tool := NewGlobTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"pattern": "*.go"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, ".go") {
			t.Fatalf("non-go line: %q", l)
		}
	}
}

func TestGlob_DoublestarMatchesNestedDirs(t *testing.T) {
	root := rootDir(t)
	writeFile(t, root, "top.txt", "")
	writeFile(t, root, "a/x.txt", "")
	writeFile(t, root, "a/b/y.txt", "")
	writeFile(t, root, "a/b/c/z.txt", "")
	writeFile(t, root, "a/skip.md", "")
	tool := NewGlobTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"pattern": "**/*.txt"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %v", lines)
	}
}

func TestGlob_PathScopesSearch(t *testing.T) {
	root := rootDir(t)
	writeFile(t, root, "out.go", "")
	writeFile(t, root, "sub/in.go", "")
	writeFile(t, root, "sub/nested/deep.go", "")
	tool := NewGlobTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "**/*.go", "path": "sub",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "sub/") {
			t.Fatalf("scope leak: %q", l)
		}
	}
}

func TestGlob_ReturnsRelativePathsSortedByMtimeDesc(t *testing.T) {
	root := rootDir(t)
	writeFile(t, root, "old.go", "")
	writeFile(t, root, "new.go", "")
	old := time.Now().Add(-2 * time.Hour)
	newer := time.Now()
	if err := os.Chtimes(filepath.Join(root, "old.go"), old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Chtimes(filepath.Join(root, "new.go"), newer, newer); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	tool := NewGlobTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"pattern": "*.go"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 || lines[0] != "new.go" || lines[1] != "old.go" {
		t.Fatalf("order = %v", lines)
	}
}

func TestGlob_SkipsDotGitAndNodeModules(t *testing.T) {
	root := rootDir(t)
	writeFile(t, root, "a.go", "")
	writeFile(t, root, ".git/objects/x.go", "")
	writeFile(t, root, "node_modules/pkg/y.go", "")
	writeFile(t, root, "vendor/lib/z.go", "")
	writeFile(t, root, ".yolium/cache/q.go", "")
	tool := NewGlobTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"pattern": "**/*.go"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 1 || lines[0] != "a.go" {
		t.Fatalf("got %v", lines)
	}
}

func TestGlob_BraceAlternationMatchesEachOption(t *testing.T) {
	root := rootDir(t)
	writeFile(t, root, "a.ts", "")
	writeFile(t, root, "b.tsx", "")
	writeFile(t, root, "c.js", "")
	tool := NewGlobTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"pattern": "*.{ts,tsx}"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, ".ts") && !strings.HasSuffix(l, ".tsx") {
			t.Fatalf("unexpected match: %q", l)
		}
	}
}

func TestGlob_BraceWithDoublestarAndNames(t *testing.T) {
	root := rootDir(t)
	writeFile(t, root, "AGENTS.md", "")
	writeFile(t, root, "CLAUDE.md", "")
	writeFile(t, root, "README.md", "")
	writeFile(t, root, "sub/AGENTS.md", "")
	writeFile(t, root, "notes.txt", "")
	tool := NewGlobTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "**/{AGENTS.md,CLAUDE.md,README*}",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %v", lines)
	}
	for _, l := range lines {
		if strings.HasSuffix(l, "notes.txt") {
			t.Fatalf("matched excluded file: %q", l)
		}
	}
}

func TestGlob_LiteralBraceWithoutCommaIsNotAlternation(t *testing.T) {
	// "{a,b}/{c,d}" cartesian product; ensures multiple groups combine.
	root := rootDir(t)
	writeFile(t, root, "src/x.ts", "")
	writeFile(t, root, "test/x.ts", "")
	writeFile(t, root, "other/x.ts", "")
	tool := NewGlobTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "{src,test}/*.ts",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "other/") {
			t.Fatalf("matched excluded dir: %q", l)
		}
	}
}

func TestExpandBraces(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"*.go", []string{"*.go"}},
		{"*.{ts,tsx}", []string{"*.ts", "*.tsx"}},
		{"{a,b}/{c,d}", []string{"a/c", "a/d", "b/c", "b/d"}},
		{"x{a,{b,c}}y", []string{"xay", "xby", "xcy"}},
		{"{abc}", []string{"{abc}"}},   // no comma → literal
		{"a{b", []string{"a{b"}},       // unbalanced → literal
		{"**/{AGENTS.md,CLAUDE.md}", []string{"**/AGENTS.md", "**/CLAUDE.md"}},
	}
	for _, c := range cases {
		got := expandBraces(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Fatalf("expandBraces(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestGlob_RejectsPathOutsideCwd(t *testing.T) {
	tool := NewGlobTool(rootDir(t))
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]any{
		"pattern": "*.go", "path": filepath.Join("..", "escape"),
	}))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "outside") {
		t.Fatalf("err = %v", err)
	}
}

func TestGlob_EmptyResultReturnsEmptyString(t *testing.T) {
	root := rootDir(t)
	writeFile(t, root, "a.go", "")
	tool := NewGlobTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]any{"pattern": "*.xyz"}))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "" {
		t.Fatalf("got = %q", got)
	}
}
