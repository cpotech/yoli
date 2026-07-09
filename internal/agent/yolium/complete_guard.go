package yolium

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Dirty-worktree completion guard.
//
// Yolium marks a work item done the moment it sees a complete event, and
// the host merges the worktree branch later — so a complete signal with
// uncommitted changes strands the work (observed with Qwen3-Coder-class
// models: the model emits a "commit" progress message and calls
// yolium_complete without ever running `git add`/`git commit`). The guard
// rejects completion while `git status --porcelain` reports changes,
// returning a corrective tool result so the model can commit and retry.
//
// Harness-written paths are excluded from the check:
//   - anything starting with ".yolium" (.yolium/, .yolium.json,
//     .yolium-code-agent-instructions.md, ...) — written by Yolium's
//     onboarding and by yoli itself (summary.md), often not gitignored;
//   - the root "AGENTS.md" — Yolium's container entrypoint overwrites the
//     project's AGENTS.md at every container start, so it is dirty on
//     every run through no fault of the agent.
//
// The guard FAILS OPEN: no git, not a work tree, or a wedged git must
// never block a completion signal. YOLI_COMPLETE_GUARD=off disables it.

// completeGuardTimeout bounds the git status call so a wedged git (huge
// worktree, dead NFS) cannot block the terminator path.
const completeGuardTimeout = 15 * time.Second

// maxListedDirtyPaths caps the file list in the corrective message.
const maxListedDirtyPaths = 20

// DirtyPaths parses `git status --porcelain` (v1) output and returns the
// repo-root-relative paths that block completion, with harness-written
// paths excluded. Staged, unstaged, and untracked entries all count.
func DirtyPaths(porcelain string) []string {
	var out []string
	for _, line := range strings.Split(porcelain, "\n") {
		// Porcelain v1: two status columns, a space, then the path.
		if len(line) < 4 || strings.TrimSpace(line) == "" {
			continue
		}
		entry := line[3:]
		if oldRaw, newRaw, renamed := strings.Cut(entry, " -> "); renamed {
			// Rename/copy: excluded only when BOTH sides are harness
			// paths — a rename out of .yolium/ into the tree is real
			// work. Report the destination path.
			oldPath := unquoteGitPath(oldRaw)
			newPath := unquoteGitPath(newRaw)
			if guardExcluded(oldPath) && guardExcluded(newPath) {
				continue
			}
			out = append(out, newPath)
			continue
		}
		path := unquoteGitPath(entry)
		if guardExcluded(path) {
			continue
		}
		out = append(out, path)
	}
	return out
}

// guardExcluded reports whether a repo-root-relative path is harness
// noise the guard must ignore (see package comment for why).
func guardExcluded(path string) bool {
	return strings.HasPrefix(path, ".yolium") || path == "AGENTS.md"
}

// unquoteGitPath undoes git's C-style path quoting (`"tab\there"`).
// strconv.Unquote covers git's escapes (\t, \n, \", \\, octal); on any
// parse failure the input is returned unchanged.
func unquoteGitPath(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

// worktreeDirtyPaths runs git status in repoPath and returns the blocking
// paths. ok=false means the guard could not determine the state (git
// missing, not a work tree, timeout) and the caller must fail open.
func worktreeDirtyPaths(ctx context.Context, repoPath string) ([]string, bool) {
	if repoPath == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, completeGuardTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-c", "core.quotepath=off", "status", "--porcelain")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	return DirtyPaths(string(out)), true
}

// completeGuardDisabled reports whether the operator kill switch is set.
// Read directly from the environment (like YOLI_READ_ALLOW), not part of
// the config file schema.
func completeGuardDisabled() bool {
	return os.Getenv("YOLI_COMPLETE_GUARD") == "off"
}

// formatDirtyGuardError builds the corrective tool result for a rejected
// completion. It must tell the model exactly how to recover.
func formatDirtyGuardError(paths []string) error {
	listed := paths
	extra := 0
	if len(listed) > maxListedDirtyPaths {
		extra = len(listed) - maxListedDirtyPaths
		listed = listed[:maxListedDirtyPaths]
	}
	list := strings.Join(listed, ", ")
	if extra > 0 {
		list += fmt.Sprintf(" and %d more", extra)
	}
	return fmt.Errorf("cannot complete: %d uncommitted change(s) in the worktree: %s. "+
		"The complete event was NOT emitted. Commit your work first — run Bash with: "+
		"git add <files> && git commit -m \"<conventional commit message>\" — "+
		"then call yolium_complete again", len(paths), list)
}

// WorktreeBlocksComplete reports whether a completion signal for repoPath
// must be blocked: guard enabled, git state readable, and non-harness
// uncommitted changes present. The CLI uses this for the text-protocol
// and summary-fallback completion paths so they enforce the same
// invariant as the yolium_complete tool.
func WorktreeBlocksComplete(ctx context.Context, repoPath string) bool {
	if completeGuardDisabled() {
		return false
	}
	dirty, ok := worktreeDirtyPaths(ctx, repoPath)
	return ok && len(dirty) > 0
}
