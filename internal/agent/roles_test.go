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
}

func TestGetRolePrompt_PlannerMentionsPlan(t *testing.T) {
	p, err := GetRolePrompt("planner")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(p) == 0 || !strings.Contains(strings.ToLower(p), "plan") {
		t.Fatalf("prompt = %q", p)
	}
}

func TestGetRolePrompt_ReviewerMentionsReview(t *testing.T) {
	p, err := GetRolePrompt("reviewer")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(p) == 0 || !strings.Contains(strings.ToLower(p), "review") {
		t.Fatalf("prompt = %q", p)
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
