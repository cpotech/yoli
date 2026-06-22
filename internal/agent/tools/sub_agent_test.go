package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestMain implements the "self-as-helper" pattern: when YOLI_TEST_HELPER
// is set, the binary acts as a fake CLI for sub_agent tests instead of
// running the test suite. The exact behaviour is selected by the value.
func TestMain(m *testing.M) {
	switch os.Getenv("YOLI_TEST_HELPER") {
	case "dumper":
		runDumperHelper()
		return
	case "hello":
		_, _ = io.Copy(io.Discard, os.Stdin)
		fmt.Fprint(os.Stdout, "hello from child\n")
		return
	case "stderr-exit-7":
		_, _ = io.Copy(io.Discard, os.Stdin)
		fmt.Fprint(os.Stderr, "something broke")
		os.Exit(7)
	case "silent-zero":
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	os.Exit(m.Run())
}

func runDumperHelper() {
	buf, _ := io.ReadAll(os.Stdin)
	dump := map[string]any{
		"scriptPath": os.Args[0],
		"args":       os.Args[1:],
		"env": map[string]any{
			"YOLI_SUBAGENT_DEPTH": envOrNil("YOLI_SUBAGENT_DEPTH"),
			"OPENROUTER_API_KEY":  envOrNil("OPENROUTER_API_KEY"),
			"OPENROUTER_MODEL":    envOrNil("OPENROUTER_MODEL"),
		},
		"stdin": string(buf),
	}
	_ = json.NewEncoder(os.Stdout).Encode(dump)
}

func envOrNil(key string) any {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	return v
}

// helperBinary returns the path to the test binary so tests can spawn it
// as a fake CLI.
func helperBinary(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return path
}

type dump struct {
	ScriptPath string         `json:"scriptPath"`
	Args       []string       `json:"args"`
	Env        map[string]any `json:"env"`
	Stdin      string         `json:"stdin"`
}

func runDumper(t *testing.T, opts SubAgentOptions, role, prompt string) dump {
	t.Helper()
	t.Setenv("YOLI_TEST_HELPER", "dumper")
	opts.CLIEntry = helperBinary(t)
	tool := NewSubAgentTool(opts)
	out, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"role": role, "prompt": prompt,
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var d dump
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("unmarshal dump: %v\nraw: %s", err, out)
	}
	return d
}

func TestSubAgent_DefinitionSchema(t *testing.T) {
	def := NewSubAgentTool(SubAgentOptions{CLIEntry: "/nonexistent"}).Definition()
	if def.Name != "Agent" {
		t.Fatalf("name = %q", def.Name)
	}
	props, _ := def.Parameters["properties"].(map[string]any)
	if props["role"].(map[string]any)["type"] != "string" {
		t.Fatalf("role schema")
	}
	if props["prompt"].(map[string]any)["type"] != "string" {
		t.Fatalf("prompt schema")
	}
	required, _ := def.Parameters["required"].([]string)
	if len(required) != 2 || required[0] != "role" || required[1] != "prompt" {
		t.Fatalf("required = %v", required)
	}
}

func TestSubAgent_PassesRunAndRoleArgs(t *testing.T) {
	t.Setenv("YOLI_SUBAGENT_DEPTH", "")
	d := runDumper(t, SubAgentOptions{}, "planner", "hi")
	if d.ScriptPath != helperBinary(t) {
		t.Fatalf("scriptPath = %q", d.ScriptPath)
	}
	want := []string{"run", "--role", "planner"}
	if !equalStrings(d.Args, want) {
		t.Fatalf("args = %v, want %v", d.Args, want)
	}
}

func TestSubAgent_WritesPromptToStdin(t *testing.T) {
	t.Setenv("YOLI_SUBAGENT_DEPTH", "")
	d := runDumper(t, SubAgentOptions{}, "coder", "please refactor X")
	if d.Stdin != "please refactor X" {
		t.Fatalf("stdin = %q", d.Stdin)
	}
}

func TestSubAgent_ResolvesWithChildStdoutOnZeroExit(t *testing.T) {
	t.Setenv("YOLI_TEST_HELPER", "hello")
	t.Setenv("YOLI_SUBAGENT_DEPTH", "")
	tool := NewSubAgentTool(SubAgentOptions{CLIEntry: helperBinary(t)})
	out, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"role": "coder", "prompt": "x",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "hello from child\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestSubAgent_IncludesStderrOnNonZeroExit(t *testing.T) {
	t.Setenv("YOLI_TEST_HELPER", "stderr-exit-7")
	t.Setenv("YOLI_SUBAGENT_DEPTH", "")
	tool := NewSubAgentTool(SubAgentOptions{CLIEntry: helperBinary(t)})
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"role": "coder", "prompt": "x",
	}))
	if err == nil {
		t.Fatalf("want error")
	}
	if !strings.Contains(err.Error(), "something broke") {
		t.Fatalf("err = %v", err)
	}
}

func TestSubAgent_InheritsOpenRouterAPIKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-test-abc")
	t.Setenv("YOLI_SUBAGENT_DEPTH", "")
	d := runDumper(t, SubAgentOptions{}, "coder", "x")
	if d.Env["OPENROUTER_API_KEY"] != "sk-test-abc" {
		t.Fatalf("api key = %v", d.Env["OPENROUTER_API_KEY"])
	}
}

func TestSubAgent_SetsDepthToOneWhenUnset(t *testing.T) {
	// Empty value is treated identically to unset by strconv.Atoi.
	t.Setenv("YOLI_SUBAGENT_DEPTH", "")
	d := runDumper(t, SubAgentOptions{}, "coder", "x")
	if d.Env["YOLI_SUBAGENT_DEPTH"] != "1" {
		t.Fatalf("depth = %v", d.Env["YOLI_SUBAGENT_DEPTH"])
	}
}

func TestSubAgent_IncrementsExistingDepth(t *testing.T) {
	t.Setenv("YOLI_SUBAGENT_DEPTH", "1")
	d := runDumper(t, SubAgentOptions{}, "coder", "x")
	if d.Env["YOLI_SUBAGENT_DEPTH"] != "2" {
		t.Fatalf("depth = %v", d.Env["YOLI_SUBAGENT_DEPTH"])
	}
}

func TestSubAgent_ThrowsAtMaxDepth(t *testing.T) {
	t.Setenv("YOLI_SUBAGENT_DEPTH", "3")
	tool := NewSubAgentTool(SubAgentOptions{CLIEntry: "/nonexistent/path/to/yoli"})
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"role": "coder", "prompt": "x",
	}))
	if err == nil {
		t.Fatalf("want error")
	}
	if !regexp.MustCompile(`(?i)recursion`).MatchString(err.Error()) {
		t.Fatalf("err = %v", err)
	}
}

func TestSubAgent_DefaultModelExportsOpenRouterModel(t *testing.T) {
	t.Setenv("YOLI_SUBAGENT_DEPTH", "")
	d := runDumper(t, SubAgentOptions{DefaultModel: "anthropic/claude-3.5-sonnet"}, "coder", "x")
	if d.Env["OPENROUTER_MODEL"] != "anthropic/claude-3.5-sonnet" {
		t.Fatalf("model = %v", d.Env["OPENROUTER_MODEL"])
	}
}

func TestSubAgent_EmptyStdoutOnZeroExit(t *testing.T) {
	t.Setenv("YOLI_TEST_HELPER", "silent-zero")
	t.Setenv("YOLI_SUBAGENT_DEPTH", "")
	tool := NewSubAgentTool(SubAgentOptions{CLIEntry: helperBinary(t)})
	out, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"role": "coder", "prompt": "x",
	}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "" {
		t.Fatalf("out = %q", out)
	}
}

func TestSubAgent_MissingCLIEntryMentionsPath(t *testing.T) {
	t.Setenv("YOLI_SUBAGENT_DEPTH", "")
	missing := filepath.Join(rootDir(t), "does-not-exist")
	tool := NewSubAgentTool(SubAgentOptions{CLIEntry: missing})
	_, err := tool.Run(context.Background(), mustJSON(t, map[string]string{
		"role": "coder", "prompt": "x",
	}))
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("err = %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
