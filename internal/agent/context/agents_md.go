// Package context loads ambient context — AGENTS.md instruction blocks —
// that gets prepended to the system prompt.
package context

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LoadAgentsMdOptions configures LoadAgentsMd.
type LoadAgentsMdOptions struct {
	Cwd string
}

// LoadAgentsMd walks upward from Cwd, stopping at the first directory
// that contains a .git entry (or the filesystem root), and concatenates
// the AGENTS.md content of every directory on the walk path in
// outermost-first order. Each block is right-trimmed of trailing
// whitespace and blocks are joined with a blank line. Returns "" when
// no AGENTS.md is found.
func LoadAgentsMd(opts LoadAgentsMdOptions) (string, error) {
	cwd, err := filepath.Abs(opts.Cwd)
	if err != nil {
		return "", err
	}

	var chain []string
	dir := cwd
	for {
		chain = append(chain, dir)
		if exists(filepath.Join(dir, ".git")) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Reverse for outermost-first ordering.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	var blocks []string
	for _, d := range chain {
		content, err := readIfExists(filepath.Join(d, "AGENTS.md"))
		if err != nil {
			return "", err
		}
		if content != nil {
			blocks = append(blocks, strings.TrimRight(*content, " \t\r\n\v\f"))
		}
	}
	return strings.Join(blocks, "\n\n"), nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readIfExists returns nil (no error) when the file is missing; bubbles
// up any other I/O failure so callers don't silently swallow it.
func readIfExists(path string) (*string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	s := string(data)
	return &s, nil
}
