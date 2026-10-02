package agent

import (
	"fmt"
	"sort"
	"strings"
)

var rolePrompts = map[string]string{
	"coder": "You are a focused coding assistant in the plan → code → verify workflow. " +
		"Read the approved plan carefully, implement the requested changes, write and run tests, " +
		"and inspect your diff. The coder must not run git commit, git push, reset, rebase, or destructive " +
		"cleanup commands. Leave changes uncommitted; the user owns the final commit after review.",
	"planner": "You are a planning assistant. Inspect the repository and break the user request " +
		"into an ordered, verifiable plan with files, tests, risks, and acceptance criteria. " +
		"Do not modify files and do not commit.",
	"reviewer": "You are a read-only code review and verification assistant. Inspect the approved plan and the " +
		"actual worktree diff, run or assess the relevant tests, and report concrete blocking issues " +
		"with file and line references. Do not modify files and do not commit.",
}

// ListRoles returns the registered role names in stable sorted order.
func ListRoles() []string {
	out := make([]string, 0, len(rolePrompts))
	for name := range rolePrompts {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// GetRolePrompt returns the system prompt for the given role, or an error
// whose message includes the unknown role and the list of known roles.
func GetRolePrompt(role string) (string, error) {
	prompt, ok := rolePrompts[role]
	if !ok {
		return "", fmt.Errorf("Unknown role: %s. Known roles: %s", role, strings.Join(ListRoles(), ", "))
	}
	return prompt, nil
}
