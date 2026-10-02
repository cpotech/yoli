package agent

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestGetRolePrompt_CoderMentionsCode(t *testing.T) {
	p, err := GetRolePrompt("coder")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(p) == 0 || !strings.Contains(strings.ToLower(p), "code") {
		t.Fatalf("prompt = %q", p)
	}
	for _, want := range []string{"plan", "verify", "must not run git commit", "user owns the final commit"} {
		if !strings.Contains(strings.ToLower(p), want) {
			t.Fatalf("coder prompt missing %q: %q", want, p)
		}
	}
}

func TestGetRolePrompt_PlannerMentionsPlanAndNoMutation(t *testing.T) {
	p, err := GetRolePrompt("planner")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, want := range []string{"plan", "do not modify files", "do not commit"} {
		if !strings.Contains(strings.ToLower(p), want) {
			t.Fatalf("planner prompt missing %q: %q", want, p)
		}
	}
}

func TestGetRolePrompt_ReviewerIsReadOnly(t *testing.T) {
	p, err := GetRolePrompt("reviewer")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, want := range []string{"review", "read-only", "worktree diff", "do not modify files"} {
		if !strings.Contains(strings.ToLower(p), want) {
			t.Fatalf("reviewer prompt missing %q: %q", want, p)
		}
	}
}

func TestGetRolePrompt_UnknownErrorListsRoles(t *testing.T) {
	_, err := GetRolePrompt("bogus")
	if err == nil {
		t.Fatalf("want error")
	}
	msg := err.Error()
	for _, want := range []string{"bogus", "coder", "planner", "reviewer"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestListRoles_ContainsKnownRoles(t *testing.T) {
	roles := ListRoles()
	for _, want := range []string{"coder", "planner", "reviewer"} {
		found := false
		for _, r := range roles {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in %v", want, roles)
		}
	}
}

func TestListRoles_ReturnsSorted(t *testing.T) {
	roles := ListRoles()
	sorted := make([]string, len(roles))
	copy(sorted, roles)
	sort.Strings(sorted)
	if !reflect.DeepEqual(roles, sorted) {
		t.Fatalf("not sorted: %v", roles)
	}
}
