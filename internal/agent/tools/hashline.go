package tools

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

// hashlineWidth is the number of hex characters kept from each line's
// SHA-1. 3 chars = 12 bits = 4096 buckets, matching the hashline
// format described in https://blog.can.ac/2026/02/12/the-harness-problem/.
// Line numbers are always supplied alongside the hash, so collisions
// degrade the integrity check rather than addressing.
const hashlineWidth = 3

// HashLine returns a short hex hash of the given line content. The
// hash is deterministic and position-independent, so identical-content
// lines always hash the same.
func HashLine(content string) string {
	sum := sha1.Sum([]byte(content))
	return hex.EncodeToString(sum[:])[:hashlineWidth]
}

// splitFileLines splits content on '\n' and reports whether a trailing
// newline was present. The returned slice never contains the artifact
// empty string that Split produces when content ends with '\n'.
func splitFileLines(content string) ([]string, bool) {
	if content == "" {
		return nil, false
	}
	trailing := strings.HasSuffix(content, "\n")
	s := content
	if trailing {
		s = s[:len(s)-1]
	}
	if s == "" {
		// content was exactly "\n" — represent as one empty line.
		return []string{""}, true
	}
	return strings.Split(s, "\n"), trailing
}

// joinFileLines is the inverse of splitFileLines.
func joinFileLines(lines []string, trailingNewline bool) string {
	s := strings.Join(lines, "\n")
	if trailingNewline {
		s += "\n"
	}
	return s
}

// AnnotateHashlines prefixes each line of content with "N:HHH|"
// where N is the 1-based line number and HHH is the truncated hash.
// The original trailing newline (if any) is preserved.
func AnnotateHashlines(content string) string {
	lines, trailing := splitFileLines(content)
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&b, "%d:%s|%s", i+1, HashLine(line), line)
		if i < len(lines)-1 || trailing {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
