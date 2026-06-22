package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"yoli/internal/ai"
)

// EditTool replaces content inside a file. It supports two modes:
//
//   - str_replace mode: replace an exact substring (old_string → new_string).
//   - hashline mode:    replace a single line or an inclusive range of
//     lines identified by (line, hash) or (from_line, from_hash, to_line,
//     to_hash). Provides drift detection — the call fails if the file's
//     current line hash doesn't match what the caller supplied.
type EditTool struct {
	cwd string
}

// NewEditTool constructs an EditTool rooted at cwd.
func NewEditTool(cwd string) *EditTool { return &EditTool{cwd: cwd} }

// Definition returns the tool schema sent to the model.
func (t *EditTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "Edit",
		Description: "Edit a file. Two modes:\n" +
			"  • str_replace: provide old_string + new_string. old_string must occur " +
			"once unless replace_all is true.\n" +
			"  • hashline: identify the target by (line, hash) for a single line, or " +
			"(from_line, from_hash, to_line, to_hash) for an inclusive range, plus " +
			"new_text. Use the hashes printed by read_file with with_hashes=true. " +
			"The edit is rejected if the file has drifted from the supplied hashes.\n" +
			"The two modes are mutually exclusive.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path relative to the working directory.",
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "str_replace mode: exact text to find. Must match byte-for-byte.",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "str_replace mode: replacement text. Must differ from old_string.",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "str_replace mode: replace every occurrence instead of requiring uniqueness.",
				},
				"line": map[string]any{
					"type":        "integer",
					"description": "hashline mode (single line): 1-based line number to replace.",
				},
				"hash": map[string]any{
					"type":        "string",
					"description": "hashline mode (single line): expected content hash of `line`.",
				},
				"from_line": map[string]any{
					"type":        "integer",
					"description": "hashline mode (range): 1-based first line of the inclusive range.",
				},
				"from_hash": map[string]any{
					"type":        "string",
					"description": "hashline mode (range): expected content hash of `from_line`.",
				},
				"to_line": map[string]any{
					"type":        "integer",
					"description": "hashline mode (range): 1-based last line of the inclusive range.",
				},
				"to_hash": map[string]any{
					"type":        "string",
					"description": "hashline mode (range): expected content hash of `to_line`.",
				},
				"new_text": map[string]any{
					"type":        "string",
					"description": "hashline mode: replacement text. May contain newlines to grow the line count.",
				},
			},
			"required": []string{"path"},
		},
	}
}

type editArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`

	Line     *int   `json:"line"`
	Hash     string `json:"hash"`
	FromLine *int   `json:"from_line"`
	FromHash string `json:"from_hash"`
	ToLine   *int   `json:"to_line"`
	ToHash   string `json:"to_hash"`
	NewText  string `json:"new_text"`
}

// Run dispatches to the appropriate edit mode based on which arguments
// were supplied.
func (t *EditTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args editArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("edit: invalid arguments: %w", err)
	}
	target, err := ResolveInside(t.cwd, args.Path)
	if err != nil {
		return "", err
	}

	hasStr := args.OldString != "" || args.NewString != "" || args.ReplaceAll
	hasSingle := args.Line != nil
	hasRange := args.FromLine != nil || args.ToLine != nil

	switch {
	case hasSingle && hasRange:
		return "", fmt.Errorf("edit: cannot combine single-line and range hashline parameters")
	case hasStr && (hasSingle || hasRange):
		return "", fmt.Errorf("edit: cannot combine str_replace and hashline parameters")
	case hasSingle:
		return t.runHashlineSingle(target, args)
	case hasRange:
		return t.runHashlineRange(target, args)
	default:
		return t.runStrReplace(target, args)
	}
}

// runStrReplace performs the legacy substring substitution. Kept
// because — per "The Harness Problem" benchmark — no single edit
// format dominates across models.
func (t *EditTool) runStrReplace(target string, args editArgs) (string, error) {
	if args.OldString == "" {
		return "", fmt.Errorf("edit: missing edit parameters; provide old_string/new_string or line/hash/new_text")
	}
	if args.OldString == args.NewString {
		return "", fmt.Errorf("edit: old_string and new_string are identical")
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("edit: %s: %v", args.Path, err)
	}
	mode := info.Mode().Perm()
	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("edit: read %s: %v", args.Path, err)
	}
	body := string(data)
	count := strings.Count(body, args.OldString)
	if count == 0 {
		return "", fmt.Errorf("edit: old_string not found in %s", args.Path)
	}
	if count > 1 && !args.ReplaceAll {
		return "", fmt.Errorf("edit: old_string appears %d times in %s; pass replace_all=true to replace every occurrence", count, args.Path)
	}
	n := 1
	if args.ReplaceAll {
		n = -1
	}
	updated := strings.Replace(body, args.OldString, args.NewString, n)
	if err := os.WriteFile(target, []byte(updated), mode); err != nil {
		return "", fmt.Errorf("edit: write %s: %v", args.Path, err)
	}
	suffix := "occurrence"
	if count != 1 {
		suffix = "occurrences"
	}
	return fmt.Sprintf("Edited %s (replaced %d %s)", args.Path, count, suffix), nil
}

// runHashlineSingle replaces one line identified by (line, hash).
func (t *EditTool) runHashlineSingle(target string, args editArgs) (string, error) {
	if args.Hash == "" {
		return "", fmt.Errorf("edit: hashline mode requires `hash` alongside `line`")
	}
	lineNum := *args.Line
	lines, trailing, mode, err := readForEdit(target, args.Path)
	if err != nil {
		return "", err
	}
	if err := checkLineHash(args.Path, lines, lineNum, args.Hash); err != nil {
		return "", err
	}
	replacement := splitNewText(args.NewText)
	newLines := append([]string{}, lines[:lineNum-1]...)
	newLines = append(newLines, replacement...)
	newLines = append(newLines, lines[lineNum:]...)
	if err := os.WriteFile(target, []byte(joinFileLines(newLines, trailing)), mode); err != nil {
		return "", fmt.Errorf("edit: write %s: %v", args.Path, err)
	}
	return fmt.Sprintf("Edited %s (line %d → %d line(s))", args.Path, lineNum, len(replacement)), nil
}

// runHashlineRange replaces an inclusive range of lines.
func (t *EditTool) runHashlineRange(target string, args editArgs) (string, error) {
	if args.FromLine == nil || args.ToLine == nil {
		return "", fmt.Errorf("edit: range hashline mode requires both from_line and to_line")
	}
	if args.FromHash == "" || args.ToHash == "" {
		return "", fmt.Errorf("edit: range hashline mode requires both from_hash and to_hash")
	}
	from, to := *args.FromLine, *args.ToLine
	if from > to {
		return "", fmt.Errorf("edit: from_line (%d) > to_line (%d)", from, to)
	}
	lines, trailing, mode, err := readForEdit(target, args.Path)
	if err != nil {
		return "", err
	}
	if err := checkLineHash(args.Path, lines, from, args.FromHash); err != nil {
		return "", err
	}
	if err := checkLineHash(args.Path, lines, to, args.ToHash); err != nil {
		return "", err
	}
	replacement := splitNewText(args.NewText)
	newLines := append([]string{}, lines[:from-1]...)
	newLines = append(newLines, replacement...)
	newLines = append(newLines, lines[to:]...)
	if err := os.WriteFile(target, []byte(joinFileLines(newLines, trailing)), mode); err != nil {
		return "", fmt.Errorf("edit: write %s: %v", args.Path, err)
	}
	return fmt.Sprintf("Edited %s (lines %d-%d → %d line(s))", args.Path, from, to, len(replacement)), nil
}

// readForEdit loads the file and returns split lines, trailing-newline
// flag, file mode, and any wrapped error suitable for surfacing.
func readForEdit(target, displayPath string) ([]string, bool, os.FileMode, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, false, 0, fmt.Errorf("edit: %s: %v", displayPath, err)
	}
	mode := info.Mode().Perm()
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, false, 0, fmt.Errorf("edit: read %s: %v", displayPath, err)
	}
	lines, trailing := splitFileLines(string(data))
	return lines, trailing, mode, nil
}

// checkLineHash verifies that `line` (1-based) exists in `lines` and
// that its content hashes to `wantHash`.
func checkLineHash(displayPath string, lines []string, line int, wantHash string) error {
	if line < 1 || line > len(lines) {
		return fmt.Errorf("edit: %s has %d lines; line %d is out of range", displayPath, len(lines), line)
	}
	got := HashLine(lines[line-1])
	if got != wantHash {
		return fmt.Errorf("edit: hash drift at %s:%d (expected %s, got %s) — re-read the file with with_hashes=true", displayPath, line, wantHash, got)
	}
	return nil
}

// splitNewText turns a (possibly multi-line) replacement string into the
// line slice it represents. Empty new_text means one empty line; a
// trailing newline in new_text adds an extra empty line.
func splitNewText(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

var _ Tool = (*EditTool)(nil)
