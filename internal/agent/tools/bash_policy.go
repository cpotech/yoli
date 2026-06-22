package tools

import (
	"fmt"
	"regexp"
)

// disallowedCommandPatterns are git/gh subcommand invocations that
// Yolium agents must not perform from inside a worktree. The host
// process owns branch creation, pushing, and PR opening; agents commit
// locally only.
//
// Each regex anchors on a statement boundary (start, whitespace, or one
// of `;`, `&`, `|`, `(`, `)`, newline, backtick) so that substrings
// inside another word (e.g. "git-pushed") do not match, while compound
// shell commands like `cd dir && git push` still trip the check.
var disallowedCommandPatterns = []struct {
	label string
	re    *regexp.Regexp
}{
	{"git push", regexp.MustCompile("(?:^|[\\s;&|()\\n`])git\\s+push\\b")},
	{"git checkout -b/-B", regexp.MustCompile("(?:^|[\\s;&|()\\n`])git\\s+checkout\\s+-[bB]\\b")},
	{"git switch -c/-C", regexp.MustCompile("(?:^|[\\s;&|()\\n`])git\\s+switch\\s+-[cC]\\b")},
	{"git reset --hard", regexp.MustCompile("(?:^|[\\s;&|()\\n`])git\\s+reset\\s+--hard\\b")},
	{"git stash drop", regexp.MustCompile("(?:^|[\\s;&|()\\n`])git\\s+stash\\s+drop\\b")},
	{"gh pr create", regexp.MustCompile("(?:^|[\\s;&|()\\n`])gh\\s+pr\\s+create\\b")},
}

// CheckBashCommandPolicy returns an error if cmd contains a banned
// git/gh subcommand. The check is intentionally narrow: it blocks the
// well-known footguns called out by Yolium's code-agent rules (commit
// locally, never push, never create branches, never open PRs) without
// trying to be a general-purpose shell parser.
func CheckBashCommandPolicy(cmd string) error {
	// Prepend whitespace so a command at byte 0 still matches the
	// boundary class.
	probe := " " + cmd
	for _, p := range disallowedCommandPatterns {
		if p.re.MatchString(probe) {
			return fmt.Errorf(
				"run_bash: disallowed command %q. "+
					"Yolium worktree agents must commit locally only — "+
					"use `git commit` via run_bash; the host pushes and opens PRs.",
				p.label)
		}
	}
	return nil
}
