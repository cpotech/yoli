package agent

import (
	"fmt"
	"sort"
	"strings"
)

var rolePrompts = map[string]string{
	"coder": "You are a focused coding assistant. Read the user request carefully " +
		"and respond with clear, correct code and concise explanations. " +
		"Prefer minimal changes that solve the stated problem.",
	"planner": "You are a planning assistant. Break the user request into an ordered " +
		"plan of small, verifiable steps. Identify risks and unknowns before " +
		"suggesting code changes.",
	"reviewer": "You are a code review assistant. Review the supplied code or change " +
		"for correctness, clarity, and risk. Point out concrete issues with file " +
		"and line references when possible, and suggest specific fixes.",
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
