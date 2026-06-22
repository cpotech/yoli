package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFile_DefinitionSchema(t *testing.T) {
	def := NewWriteFileTool(t.TempDir()).Definition()
	if def.Name != "Write" {
		t.Fatalf("name = %q", def.Name)
	}
	props, _ := def.Parameters["properties"].(map[string]any)
	if props["path"].(map[string]any)["type"] != "string" {
		t.Fatalf("path schema")
	}
	if props["content"].(map[string]any)["type"] != "string" {
		t.Fatalf("content schema")
	}
	required, _ := def.Parameters["required"].([]string)
	if len(required) != 2 || required[0] != "path" || required[1] != "content" {
		t.Fatalf("required = %v", required)
	}
}

func TestWriteFile_WritesRelative(t *testing.T) {
	root := rootDir(t)
	tool := NewWriteFileTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"path": "out.txt", "content": "written by yoli",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(got, "out.txt") || !strings.Contains(got, "15") {
		t.Fatalf("confirmation = %q", got)
	}
	body, err := os.ReadFile(filepath.Join(root, "out.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "written by yoli" {
		t.Fatalf("body = %q", body)
	}
}

func TestWriteFile_CreatesIntermediateDirs(t *testing.T) {
	root := rootDir(t)
	tool := NewWriteFileTool(root)
	if _, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"path": filepath.Join("nested", "deep", "file.txt"), "content": "x",
	})); err != nil {
		t.Fatalf("run: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, "nested", "deep", "file.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "x" {
		t.Fatalf("body = %q", body)
	}
}

func TestWriteFile_RejectsEscape(t *testing.T) {
	tool := NewWriteFileTool(rootDir(t))
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"path": filepath.Join("..", "escape.txt"), "content": "no",
	}))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "outside") {
		t.Fatalf("err = %v", err)
	}
}
