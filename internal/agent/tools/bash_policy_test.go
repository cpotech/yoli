package tools

import (
	"strings"
	"testing"
)

func TestCheckBashCommandPolicy_AllowsBenign(t *testing.T) {
	for _, cmd := range []string{
		"ls -la",
		"go test ./...",
		"git status",
		"git diff main...HEAD",
		"git log --oneline -n 5",
		"git commit -m 'feat: x'",
		"echo 'I pushed yesterday'",
		"git pushed", // not a real subcommand, must not trip "push" rule
		"git-push something",
	} {
		if err := CheckBashCommandPolicy(cmd); err != nil {
			t.Errorf("expected %q to be allowed, got %v", cmd, err)
		}
	}
}

func TestCheckBashCommandPolicy_BlocksDangerous(t *testing.T) {
	cases := []struct {
		cmd  string
		want string
	}{
		{"git push", "git push"},
		{"git push origin main", "git push"},
		{"cd src && git push", "git push"},
		{"git checkout -b feature", "git checkout -b/-B"},
		{"git checkout -B feature", "git checkout -b/-B"},
		{"git switch -c feature", "git switch -c/-C"},
		{"git switch -C feature", "git switch -c/-C"},
		{"git reset --hard origin/main", "git reset --hard"},
		{"git stash drop", "git stash drop"},
		{"gh pr create --title foo", "gh pr create"},
		{"true && gh pr create", "gh pr create"},
	}
	for _, tc := range cases {
		err := CheckBashCommandPolicy(tc.cmd)
		if err == nil {
			t.Errorf("expected %q to be blocked", tc.cmd)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("blocked %q but error %q missing label %q", tc.cmd, err.Error(), tc.want)
		}
	}
}

// TestCheckBashCommandPolicy_AcknowledgedBypasses pins shell constructs
// the lexical regex policy does NOT catch. The policy is intentionally
// narrow (see package comment); these cases document its limits so
// future readers don't mistake it for a security boundary. If you
// genuinely need to block these, do it at a process wrapper / sandbox
// layer, not here.
func TestCheckBashCommandPolicy_AcknowledgedBypasses(t *testing.T) {
	bypasses := []struct {
		cmd, note string
	}{
		{`git -C /tmp push`, "flag between git and subcommand defeats the literal 'git\\s+push' anchor"},
		{`X=push; git $X`, "variable expansion hides the banned subcommand"},
		{`eval "git push"`, `double-quote is not in the boundary character class`},
		{`$(printf 'git ')push origin main`, "command substitution"},
		{`alias gp='git push' && gp`, "alias hides the literal from the regex"},
	}
	for _, tc := range bypasses {
		if err := CheckBashCommandPolicy(tc.cmd); err != nil {
			t.Errorf("policy now blocks %q (%s): %v — update package doc and this test if intentional", tc.cmd, tc.note, err)
		}
	}
}

// TestCheckBashCommandPolicy_KnownFalsePositives pins benign commands
// the lexical policy still rejects. They are listed here so a future
// tightening pass doesn't silently regress acceptable use without
// someone re-confirming the tradeoff.
func TestCheckBashCommandPolicy_KnownFalsePositives(t *testing.T) {
	falsePositives := []struct {
		cmd, note string
	}{
		{`echo git push`, "literal text passed as argv to echo, never executed"},
		{"cat <<EOF\ngit push\nEOF", "heredoc body fed to cat, not bash"},
		{`# git push`, "shell comment"},
	}
	for _, tc := range falsePositives {
		if err := CheckBashCommandPolicy(tc.cmd); err == nil {
			t.Errorf("policy stopped rejecting %q (%s); confirm intentional and update this test", tc.cmd, tc.note)
		}
	}
}
