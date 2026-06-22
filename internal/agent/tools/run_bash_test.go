package tools

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRunBash_DefinitionSchema(t *testing.T) {
	def := NewRunBashTool(t.TempDir()).Definition()
	if def.Name != "Bash" {
		t.Fatalf("name = %q", def.Name)
	}
	props, _ := def.Parameters["properties"].(map[string]any)
	if props["command"].(map[string]any)["type"] != "string" {
		t.Fatalf("command schema")
	}
	required, _ := def.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "command" {
		t.Fatalf("required = %v", required)
	}
}

func TestRunBash_RunsInCwdAndReportsExitZero(t *testing.T) {
	root := rootDir(t)
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(""), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	tool := NewRunBashTool(root)
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]string{"command": "ls"}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(got, "a.txt") || !strings.Contains(got, "b.txt") {
		t.Fatalf("missing files in %q", got)
	}
	if !regexp.MustCompile(`exit code: 0`).MatchString(got) {
		t.Fatalf("no exit code 0 in %q", got)
	}
}

func TestRunBash_EnforcesCommandPolicy(t *testing.T) {
	tool := NewRunBashTool(rootDir(t))
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"command": "git push origin main",
	}))
	if err == nil || !strings.Contains(err.Error(), "git push") {
		t.Fatalf("expected git push to be blocked, got err=%v", err)
	}
}

func TestRunBash_ReportsNonZeroExitAndStderr(t *testing.T) {
	tool := NewRunBashTool(rootDir(t))
	got, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"command": "echo oops >&2; exit 3",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(got, "oops") {
		t.Fatalf("no stderr in %q", got)
	}
	if !regexp.MustCompile(`exit code: 3`).MatchString(got) {
		t.Fatalf("no exit code 3 in %q", got)
	}
}
