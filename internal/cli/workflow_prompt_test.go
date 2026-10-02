package cli

import (
	"strings"
	"testing"
)

func TestChatSystem_UsesOrderedWorkflowWithoutCommit(t *testing.T) {
	assertWorkflowPrompt(t, chatSystem)
}

func TestHeadlessSystemPrompt_UsesOrderedWorkflowWithoutCommit(t *testing.T) {
	assertWorkflowPrompt(t, headlessSystemPrompt(false))
	assertWorkflowPrompt(t, headlessSystemPrompt(true))
	if strings.Contains(headlessSystemPrompt(true), "commit your work") {
		t.Fatal("Yolium prompt must not require the agent to commit")
	}
}

func assertWorkflowPrompt(t *testing.T, prompt string) {
	t.Helper()
	lower := strings.ToLower(prompt)
	plan := strings.Index(lower, "plan")
	code := strings.Index(lower, "code")
	verify := strings.Index(lower, "verify")
	if plan < 0 || code < 0 || verify < 0 || !(plan < code && code < verify) {
		t.Fatalf("prompt does not order plan, code, verify: %q", prompt)
	}
	for _, want := range []string{"must not run git commit", "leave changes uncommitted", "user owns the final commit"} {
		if !strings.Contains(lower, want) {
			t.Fatalf("prompt missing %q: %q", want, prompt)
		}
	}
}
