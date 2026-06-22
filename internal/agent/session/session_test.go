package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoli/internal/ai"
)

func msg(role ai.Role, text string) ai.Message {
	return ai.Message{Role: role, Content: &text}
}

func TestCreate_WritesV3HeaderAndInitializesLeaf(t *testing.T) {
	root := t.TempDir()
	s, err := Create(Options{RootDir: root, Cwd: "/repo"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.GetLeafID() != "" {
		t.Fatalf("leaf = %q, want empty", s.GetLeafID())
	}
	data, err := os.ReadFile(s.GetSessionFile())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var h Header
	if err := json.Unmarshal([]byte(strings.Split(string(data), "\n")[0]), &h); err != nil {
		t.Fatalf("header json: %v", err)
	}
	if h.Type != "session" || h.Version != Version || h.ID == "" || h.Cwd != "/repo" {
		t.Fatalf("header = %+v", h)
	}
}

func TestOpen_LoadsHeaderEntriesAndLeaf(t *testing.T) {
	root := t.TempDir()
	s, err := Create(Options{RootDir: root, Cwd: "/repo"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.AppendMessage(msg(ai.RoleUser, "one")); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	leaf, err := s.AppendMessage(msg(ai.RoleAssistant, "two"))
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	opened, err := Open(s.GetSessionFile())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.GetSessionID() != s.GetSessionID() || opened.GetLeafID() != leaf {
		t.Fatalf("opened id/leaf = %s/%s", opened.GetSessionID(), opened.GetLeafID())
	}
	if len(opened.GetEntries()) != 2 {
		t.Fatalf("entries = %d, want 2", len(opened.GetEntries()))
	}
}

func TestAppendMessage_WritesJSONLMessageWithParentID(t *testing.T) {
	s, err := Create(Options{RootDir: t.TempDir(), Cwd: "/repo"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	parent, err := s.AppendMessage(msg(ai.RoleUser, "root"))
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	child, err := s.AppendMessage(msg(ai.RoleAssistant, "child"))
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	entries := s.GetEntries()
	if entries[1].ID != child || entries[1].ParentID != parent || entries[1].Timestamp == "" {
		t.Fatalf("child entry = %+v", entries[1])
	}
}

func TestBuildMessages_ReturnsOnlyActiveBranchRootToLeaf(t *testing.T) {
	s, err := Create(Options{RootDir: t.TempDir(), Cwd: "/repo"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	root, _ := s.AppendMessage(msg(ai.RoleUser, "root"))
	a, _ := s.AppendMessage(msg(ai.RoleAssistant, "a"))
	if err := s.Branch(root); err != nil {
		t.Fatalf("Branch: %v", err)
	}
	_, _ = s.AppendMessage(msg(ai.RoleAssistant, "b"))
	if err := s.Branch(a); err != nil {
		t.Fatalf("Branch: %v", err)
	}
	got := s.BuildMessages()
	if len(got) != 2 || *got[0].Content != "root" || *got[1].Content != "a" {
		t.Fatalf("messages = %+v", got)
	}
}

func TestBranch_AppendsFromEarlierEntryWithoutDeletingAlternateChildren(t *testing.T) {
	s, err := Create(Options{RootDir: t.TempDir(), Cwd: "/repo"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	root, _ := s.AppendMessage(msg(ai.RoleUser, "root"))
	a, _ := s.AppendMessage(msg(ai.RoleAssistant, "a"))
	if err := s.Branch(root); err != nil {
		t.Fatalf("Branch: %v", err)
	}
	b, _ := s.AppendMessage(msg(ai.RoleAssistant, "b"))
	children := s.GetChildren(root)
	if len(children) != 2 || children[0].ID != a || children[1].ID != b {
		t.Fatalf("children = %+v", children)
	}
	if len(s.GetEntries()) != 3 {
		t.Fatalf("entries = %d, want 3", len(s.GetEntries()))
	}
}

func TestContinueRecent_SelectsNewestSessionForCwd(t *testing.T) {
	root := t.TempDir()
	old, _ := Create(Options{RootDir: root, Cwd: "/repo"})
	newest, _ := Create(Options{RootDir: root, Cwd: "/repo"})
	other, _ := Create(Options{RootDir: root, Cwd: "/other"})
	os.Chtimes(old.GetSessionFile(), mustTime("2024-01-01T00:00:00Z"), mustTime("2024-01-01T00:00:00Z"))
	os.Chtimes(newest.GetSessionFile(), mustTime("2024-01-02T00:00:00Z"), mustTime("2024-01-02T00:00:00Z"))
	os.Chtimes(other.GetSessionFile(), mustTime("2024-01-03T00:00:00Z"), mustTime("2024-01-03T00:00:00Z"))
	got, err := ContinueRecent(Options{RootDir: root, Cwd: "/repo"})
	if err != nil {
		t.Fatalf("ContinueRecent: %v", err)
	}
	if got.GetSessionID() != newest.GetSessionID() {
		t.Fatalf("got %s want %s", got.GetSessionID(), newest.GetSessionID())
	}
}

func TestResolve_AcceptsExactPathFullIDAndUniquePrefix(t *testing.T) {
	root := t.TempDir()
	s, _ := Create(Options{RootDir: root, Cwd: "/repo"})
	for _, spec := range []string{s.GetSessionFile(), s.GetSessionID(), s.GetSessionID()[:8]} {
		got, err := Resolve(Options{RootDir: root, Cwd: "/repo"}, spec)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", spec, err)
		}
		if got.GetSessionID() != s.GetSessionID() {
			t.Fatalf("Resolve(%q) = %s", spec, got.GetSessionID())
		}
	}
}

func TestForkFrom_CopiesSourceBranchIntoNewSessionWithParentSession(t *testing.T) {
	root := t.TempDir()
	src, _ := Create(Options{RootDir: root, Cwd: "/repo"})
	_, _ = src.AppendMessage(msg(ai.RoleUser, "root"))
	_, _ = src.AppendMessage(msg(ai.RoleAssistant, "leaf"))
	fork, err := ForkFrom(Options{RootDir: root, Cwd: "/repo"}, src.GetSessionFile())
	if err != nil {
		t.Fatalf("ForkFrom: %v", err)
	}
	if fork.GetHeader().ParentSession != src.GetSessionID() {
		t.Fatalf("parent session = %q", fork.GetHeader().ParentSession)
	}
	got := fork.BuildMessages()
	if len(got) != 2 || *got[0].Content != "root" || *got[1].Content != "leaf" {
		t.Fatalf("fork messages = %+v", got)
	}
}

func TestInMemory_DoesNotCreateFiles(t *testing.T) {
	root := t.TempDir()
	s := InMemory(Options{RootDir: root, Cwd: "/repo"})
	if _, err := s.AppendMessage(msg(ai.RoleUser, "one")); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(root, "**", "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(files) != 0 || s.GetSessionFile() != "" {
		t.Fatalf("files=%v sessionFile=%q", files, s.GetSessionFile())
	}
}
