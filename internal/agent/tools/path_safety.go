package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveInside resolves requested against root and rejects paths that
// escape it. If requested is absolute it is canonicalised as-is; otherwise
// it is joined with root. The returned path is always absolute and lies
// within root (or equals root).
func ResolveInside(root, requested string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	var target string
	if filepath.IsAbs(requested) {
		target, err = filepath.Abs(requested)
	} else {
		target, err = filepath.Abs(filepath.Join(absRoot, requested))
	}
	if err != nil {
		return "", fmt.Errorf("resolve target: %w", err)
	}
	rel, err := filepath.Rel(absRoot, target)
	if err != nil {
		return "", fmt.Errorf("Path %s resolves outside the working directory", requested)
	}
	sep := string(filepath.Separator)
	if rel == ".." || strings.HasPrefix(rel, ".."+sep) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("Path %s resolves outside the working directory", requested)
	}
	return target, nil
}

// ResolveReadable resolves requested for READ access. It first applies the
// working-directory sandbox (ResolveInside). If that rejects the path, it
// permits it only when the path resolves inside one of the explicitly
// allowed read-only roots — e.g. bundled skill directories that live outside
// the working tree (mounted at /opt/<agent>-skills). Write/edit tools must
// keep using ResolveInside directly: this relaxation is read-only by intent.
func ResolveReadable(root string, extraRoots []string, requested string) (string, error) {
	if target, err := ResolveInside(root, requested); err == nil {
		return target, nil
	}
	for _, r := range extraRoots {
		if r == "" {
			continue
		}
		if target, err := ResolveInside(r, requested); err == nil {
			return target, nil
		}
	}
	return "", fmt.Errorf("Path %s resolves outside the working directory", requested)
}

// ReadAllowRoots returns the extra read-only roots configured via the
// YOLI_READ_ALLOW environment variable (an OS-path-list of absolute
// directories). Returns nil when unset. Yolium sets this to the bundled
// skill directories so the Read tool can load SKILL.md files that live
// outside the agent's working directory.
func ReadAllowRoots() []string {
	v := os.Getenv("YOLI_READ_ALLOW")
	if v == "" {
		return nil
	}
	return filepath.SplitList(v)
}
