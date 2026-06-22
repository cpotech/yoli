package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func TestListDir_DefinitionSchema(t *testing.T) {
	def := NewListDirTool(t.TempDir()).Definition()
	if def.Name != "LS" {
		t.Fatalf("name = %q", def.Name)
	}
	props, _ := def.Parameters["properties"].(map[string]any)
	if props["path"].(map[string]any)["type"] != "string" {
		t.Fatalf("path schema")
	}
}

func TestListDir_ListsSortedWithTypeMarker(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tool := NewListDirTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]string{"path": "."}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := []string{"a.txt", "b.txt", "sub/"}
	if !reflect.DeepEqual(splitLines(got), want) {
		t.Fatalf("got %v, want %v", splitLines(got), want)
	}
}

func TestListDir_DefaultsToCwd(t *testing.T) {
	root := rootDir(t)
	if err := os.WriteFile(filepath.Join(root, "only.txt"), []byte(""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	tool := NewListDirTool(root)
	got, err := tool.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !reflect.DeepEqual(splitLines(got), []string{"only.txt"}) {
		t.Fatalf("got %v", splitLines(got))
	}
}

func TestListDir_RejectsEscape(t *testing.T) {
	tool := NewListDirTool(rootDir(t))
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]string{"path": ".."}))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "outside") {
		t.Fatalf("err = %v", err)
	}
}
