package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	agentsession "yoli/internal/agent/session"
	"yoli/internal/ai"
)

func createCLISession(t *testing.T, root, cwd string, texts ...string) *agentsession.Session {
	t.Helper()
	s, err := agentsession.Create(agentsession.Options{RootDir: root, Cwd: cwd})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i, text := range texts {
		role := ai.RoleUser
		if i%2 == 1 {
			role = ai.RoleAssistant
		}
		if _, err := s.AppendMessage(ai.Message{Role: role, Content: &text}); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	return s
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionList_PrintsSessionsForCurrentCwd(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	_ = createCLISession(t, root, cwd, "current")
	_ = createCLISession(t, root, t.TempDir(), "other")
	var out, errOut bytes.Buffer
	code := runSession([]string{"list", "--root", root, "--cwd", cwd}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "current") || strings.Contains(out.String(), "other") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestSessionListAll_IncludesOtherCwds(t *testing.T) {
	root := t.TempDir()
	_ = createCLISession(t, root, t.TempDir(), "one")
	_ = createCLISession(t, root, t.TempDir(), "two")
	var out, errOut bytes.Buffer
	code := runSession([]string{"list", "--all", "--root", root}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "one") || !strings.Contains(out.String(), "two") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestSessionCurrent_PrintsIDFileCwdAndLeaf(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	s := createCLISession(t, root, cwd, "hello")
	var out, errOut bytes.Buffer
	code := runSession([]string{"current", "--root", root, "--session", s.GetSessionID()}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{s.GetSessionID(), s.GetSessionFile(), cwd, s.GetLeafID()} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout missing %q: %q", want, got)
		}
	}
}

func TestSessionTree_PrintsBranchingStructure(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	s := createCLISession(t, root, cwd, "root", "a")
	mustNoErr(t, s.Branch(s.GetEntries()[0].ID))
	_, err := s.AppendMessage(ai.Message{Role: ai.RoleAssistant, Content: strp("b")})
	mustNoErr(t, err)
	var out, errOut bytes.Buffer
	code := runSession([]string{"tree", "--root", root, "--session", s.GetSessionFile()}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "root") || !strings.Contains(out.String(), "a") || !strings.Contains(out.String(), "b") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestSessionBranch_MovesLeafAndPreservesEntries(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	s := createCLISession(t, root, cwd, "root", "a")
	rootID := s.GetEntries()[0].ID
	var out, errOut bytes.Buffer
	code := runSession([]string{"branch", "--root", root, "--session", s.GetSessionFile(), "--entry", rootID}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, errOut.String())
	}
	opened, err := agentsession.Open(s.GetSessionFile())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.GetLeafID() != rootID || len(opened.GetEntries()) != 2 {
		t.Fatalf("leaf=%q entries=%d", opened.GetLeafID(), len(opened.GetEntries()))
	}
}

func TestTopLevelSessionFlags_ParseMissingValues(t *testing.T) {
	for _, args := range [][]string{
		{"--session"},
		{"--fork"},
		{"session", "current", "--session"},
		{"session", "branch", "--entry"},
	} {
		r := runCli(t, args, runOpts{cwd: t.TempDir(), home: filepath.Join(t.TempDir(), "home")})
		if r.exitCode == 0 {
			t.Fatalf("%v exit = 0", args)
		}
		if !strings.Contains(r.stderr, "requires a value") {
			t.Fatalf("%v stderr = %q", args, r.stderr)
		}
	}
}
