package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	agentsession "yoli/internal/agent/session"
)

// TestMain implements the "self-as-CLI" pattern: when
// YOLI_CLI_TEST_HELPER=1 is set, the test binary dispatches to Run
// instead of running the test suite.
func TestMain(m *testing.M) {
	if os.Getenv("YOLI_CLI_TEST_HELPER") == "1" {
		os.Exit(Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

type runResult struct {
	exitCode int
	stdout   string
	stderr   string
}

type runOpts struct {
	cwd      string
	home     string
	stdin    string
	extraEnv map[string]string
}

func runCli(t *testing.T, args []string, opts runOpts) runResult {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, args...)
	if opts.cwd != "" {
		cmd.Dir = opts.cwd
	}
	home := opts.home
	if home == "" {
		home = t.TempDir()
	}
	env := []string{
		"YOLI_CLI_TEST_HELPER=1",
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"OPENROUTER_API_KEY=",
	}
	for k, v := range opts.extraEnv {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	if opts.stdin != "" {
		cmd.Stdin = strings.NewReader(opts.stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	res := runResult{stdout: stdout.String(), stderr: stderr.String()}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.exitCode = ee.ExitCode()
		} else {
			t.Fatalf("cmd.Run: %v", err)
		}
	}
	return res
}

// ---- top-level dispatch ----

func TestCLI_VersionPrintsVersionToStdoutExitZero(t *testing.T) {
	r := runCli(t, []string{"version"}, runOpts{})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d", r.exitCode)
	}
	if strings.TrimSpace(r.stdout) != Version {
		t.Fatalf("stdout = %q", r.stdout)
	}
	if r.stderr != "" {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestCLI_DashDashVersionAlias(t *testing.T) {
	a := runCli(t, []string{"version"}, runOpts{})
	b := runCli(t, []string{"--version"}, runOpts{})
	if b.exitCode != 0 {
		t.Fatalf("exit = %d", b.exitCode)
	}
	if a.stdout != b.stdout {
		t.Fatalf("stdout mismatch: %q vs %q", a.stdout, b.stdout)
	}
}

func TestCLI_NoArgsPrintsUsageToStderr(t *testing.T) {
	r := runCli(t, nil, runOpts{})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(strings.ToLower(r.stderr), "usage") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestCLI_NoArgsUsageListsAllSubcommands(t *testing.T) {
	r := runCli(t, nil, runOpts{})
	for _, want := range []string{"version", "chat", "tui", "run", "agent", "session", "skills", "config"} {
		if !strings.Contains(r.stderr, want) {
			t.Fatalf("stderr missing %q: %q", want, r.stderr)
		}
	}
}

func TestCLI_NoArgsUsageListsSessionOptions(t *testing.T) {
	r := runCli(t, nil, runOpts{})
	for _, want := range []string{"-c", "-r", "--session", "--fork", "--no-session"} {
		if !strings.Contains(r.stderr, want) {
			t.Fatalf("stderr missing %q: %q", want, r.stderr)
		}
	}
}

func TestCLI_UnknownSubcommandMentionsIt(t *testing.T) {
	r := runCli(t, []string{"bogus-command"}, runOpts{})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(r.stderr, "bogus-command") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

// ---- tui ----

func TestTUI_MissingAPIKeyErrors(t *testing.T) {
	r := runCli(t, []string{"tui"}, runOpts{})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(r.stderr, "OPENROUTER_API_KEY") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestTUI_PositionalArgsPrintUsage(t *testing.T) {
	r := runCli(t, []string{"tui", "stray-prompt"}, runOpts{})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(strings.ToLower(r.stderr), "usage") ||
		!strings.Contains(r.stderr, "tui") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

// ---- chat / -p / --prompt ----

func TestChat_NoPromptPrintsUsage(t *testing.T) {
	r := runCli(t, []string{"chat"}, runOpts{})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(strings.ToLower(r.stderr), "usage") ||
		!strings.Contains(r.stderr, "chat") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestChat_MissingAPIKeyErrors(t *testing.T) {
	r := runCli(t, []string{"chat", "hello"}, runOpts{})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(r.stderr, "OPENROUTER_API_KEY") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestChat_DashPAliasMissingPrompt(t *testing.T) {
	r := runCli(t, []string{"-p"}, runOpts{})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(strings.ToLower(r.stderr), "usage") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestChat_DashPAliasReachesAPIKeyCheck(t *testing.T) {
	r := runCli(t, []string{"-p", "hello"}, runOpts{})
	if !strings.Contains(r.stderr, "OPENROUTER_API_KEY") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestChat_LongPromptAliasAlsoAccepted(t *testing.T) {
	r := runCli(t, []string{"--prompt", "hello"}, runOpts{})
	if !strings.Contains(r.stderr, "OPENROUTER_API_KEY") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestChat_NoSessionSkipsSessionFileCreation(t *testing.T) {
	home := t.TempDir()
	r := runCli(t, []string{"chat", "--no-session", "hello"}, runOpts{home: home})
	if !strings.Contains(r.stderr, "OPENROUTER_API_KEY") {
		t.Fatalf("stderr = %q", r.stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".yoli", "agent", "sessions")); !os.IsNotExist(err) {
		t.Fatalf("session dir err = %v", err)
	}
}

func TestChat_ContinueUsesMostRecentSession(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	s, err := resolveChatSession(chatFlags{Continue: true, SessionRoot: root}, cwd, nil)
	if err != nil {
		t.Fatalf("resolveChatSession: %v", err)
	}
	if s.GetSessionID() == "" || s.GetHeader().Cwd != cwd {
		t.Fatalf("session = %+v", s.GetHeader())
	}
}

func TestChat_SessionIDResumesSpecificSession(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	s, err := agentsession.Create(agentsession.Options{RootDir: root, Cwd: cwd})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := resolveChatSession(chatFlags{Session: s.GetSessionID(), SessionRoot: root}, cwd, nil)
	if err != nil {
		t.Fatalf("resolveChatSession: %v", err)
	}
	if got.GetSessionID() != s.GetSessionID() {
		t.Fatalf("got %s want %s", got.GetSessionID(), s.GetSessionID())
	}
}

func TestChat_ForkCreatesNewSessionWithParentSession(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	s, err := agentsession.Create(agentsession.Options{RootDir: root, Cwd: cwd})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := resolveChatSession(chatFlags{Fork: s.GetSessionID(), SessionRoot: root}, cwd, nil)
	if err != nil {
		t.Fatalf("resolveChatSession: %v", err)
	}
	if got.GetSessionID() == s.GetSessionID() || got.GetHeader().ParentSession != s.GetSessionID() {
		t.Fatalf("fork header = %+v", got.GetHeader())
	}
}

// ---- run --role ----

func TestRun_MissingRoleFlagErrors(t *testing.T) {
	r := runCli(t, []string{"run"}, runOpts{stdin: ""})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(r.stderr, "--role") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestRun_UnknownRoleListsValidOnes(t *testing.T) {
	r := runCli(t, []string{"run", "--role", "bogus"}, runOpts{stdin: ""})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	for _, want := range []string{"bogus", "coder", "planner", "reviewer"} {
		if !strings.Contains(r.stderr, want) {
			t.Fatalf("stderr missing %q: %q", want, r.stderr)
		}
	}
}

func TestRun_ValidRoleErrorsWithoutAPIKey(t *testing.T) {
	r := runCli(t, []string{"run", "--role", "coder"}, runOpts{stdin: ""})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(r.stderr, "OPENROUTER_API_KEY") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestRun_EqualsFormAccepted(t *testing.T) {
	r := runCli(t, []string{"run", "--role=planner"}, runOpts{stdin: ""})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(r.stderr, "OPENROUTER_API_KEY") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

// ---- skills ----

func writeSkill(t *testing.T, root, name, body, description string) {
	t.Helper()
	dir := filepath.Join(root, ".yoli", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\ndescription: " + description + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestSkills_NoSubPrintsUsage(t *testing.T) {
	r := runCli(t, []string{"skills"}, runOpts{cwd: t.TempDir()})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	low := strings.ToLower(r.stderr)
	if !strings.Contains(low, "usage") ||
		!strings.Contains(r.stderr, "list") ||
		!strings.Contains(r.stderr, "show") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestSkills_UnknownSubMentionsIt(t *testing.T) {
	r := runCli(t, []string{"skills", "definitely-not-a-subcommand"}, runOpts{cwd: t.TempDir()})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(r.stderr, "definitely-not-a-subcommand") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestSkills_ListEmptyState(t *testing.T) {
	r := runCli(t, []string{"skills", "list"}, runOpts{cwd: t.TempDir()})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d stderr=%q", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "No skills") {
		t.Fatalf("stdout = %q", r.stdout)
	}
}

func TestSkills_ListProjectSkill(t *testing.T) {
	cwd := t.TempDir()
	writeSkill(t, cwd, "my-skill", "Body content", "My project skill")
	r := runCli(t, []string{"skills", "list"}, runOpts{cwd: cwd})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d stderr=%q", r.exitCode, r.stderr)
	}
	for _, want := range []string{"my-skill", "project", "My project skill"} {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("stdout missing %q: %q", want, r.stdout)
		}
	}
}

func TestSkills_ListUserSkillFromHome(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	writeSkill(t, home, "user-skill", "Body", "A user skill")
	r := runCli(t, []string{"skills", "list"}, runOpts{cwd: cwd, home: home})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d stderr=%q", r.exitCode, r.stderr)
	}
	for _, want := range []string{"user-skill", "user", "A user skill"} {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("stdout missing %q: %q", want, r.stdout)
		}
	}
}

func TestSkills_ProjectBeatsUserOnNameClash(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	writeSkill(t, cwd, "shared", "Body", "Project version")
	writeSkill(t, home, "shared", "Body", "User version")
	r := runCli(t, []string{"skills", "list"}, runOpts{cwd: cwd, home: home})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d", r.exitCode)
	}
	var matchedLines []string
	for _, line := range strings.Split(r.stdout, "\n") {
		if strings.Contains(line, "shared") {
			matchedLines = append(matchedLines, line)
		}
	}
	if len(matchedLines) != 1 {
		t.Fatalf("expected exactly one 'shared' line: %v", matchedLines)
	}
	if !strings.Contains(matchedLines[0], "project") ||
		strings.Contains(matchedLines[0], "User version") {
		t.Fatalf("line = %q", matchedLines[0])
	}
}

func TestSkills_ShowPrintsBodyWithoutFrontmatter(t *testing.T) {
	cwd := t.TempDir()
	writeSkill(t, cwd, "hello", "Hello body\nMore content", "Hello skill")
	r := runCli(t, []string{"skills", "show", "hello"}, runOpts{cwd: cwd})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d stderr=%q", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "Hello body") ||
		!strings.Contains(r.stdout, "More content") {
		t.Fatalf("stdout = %q", r.stdout)
	}
	if strings.Contains(r.stdout, "description: Hello skill") ||
		strings.Contains(r.stdout, "---") {
		t.Fatalf("frontmatter leaked: %q", r.stdout)
	}
}

func TestSkills_ShowUnknownNameErrors(t *testing.T) {
	cwd := t.TempDir()
	r := runCli(t, []string{"skills", "show", "nonexistent"}, runOpts{cwd: cwd})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(r.stderr, "nonexistent") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

func TestSkills_ShowNoNamePrintsUsage(t *testing.T) {
	cwd := t.TempDir()
	r := runCli(t, []string{"skills", "show"}, runOpts{cwd: cwd})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(strings.ToLower(r.stderr), "usage") ||
		!strings.Contains(r.stderr, "skills show") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}

// ---- config ----

func TestConfig_PathPrintsResolvedPath(t *testing.T) {
	home := t.TempDir()
	r := runCli(t, []string{"config", "path"}, runOpts{home: home})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d stderr=%q", r.exitCode, r.stderr)
	}
	want := filepath.Join(home, ".config", "yoli", "config.json")
	if strings.TrimSpace(r.stdout) != want {
		t.Fatalf("stdout = %q want %q", r.stdout, want)
	}
}

func TestConfig_GetReadsUserConfig(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "yoli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"),
		[]byte(`{"default_provider":"faux"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := runCli(t, []string{"config", "get", "default_provider"}, runOpts{home: home})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d", r.exitCode)
	}
	if strings.TrimSpace(r.stdout) != "faux" {
		t.Fatalf("stdout = %q", r.stdout)
	}
}

func TestConfig_GetUnsetReturnsEmptyExitZero(t *testing.T) {
	r := runCli(t, []string{"config", "get", "default_provider"}, runOpts{})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d", r.exitCode)
	}
	if r.stdout != "" {
		t.Fatalf("stdout = %q", r.stdout)
	}
}

func TestConfig_SetThenGetRoundtrip(t *testing.T) {
	home := t.TempDir()
	set := runCli(t, []string{"config", "set", "default_provider", "openrouter"},
		runOpts{home: home})
	if set.exitCode != 0 {
		t.Fatalf("set exit = %d stderr=%q", set.exitCode, set.stderr)
	}
	body, err := os.ReadFile(filepath.Join(home, ".config", "yoli", "config.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var parsed map[string]string
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["default_provider"] != "openrouter" {
		t.Fatalf("parsed = %+v", parsed)
	}
	get := runCli(t, []string{"config", "get", "default_provider"}, runOpts{home: home})
	if get.exitCode != 0 {
		t.Fatalf("get exit = %d", get.exitCode)
	}
	if strings.TrimSpace(get.stdout) != "openrouter" {
		t.Fatalf("get stdout = %q", get.stdout)
	}
}

func TestConfig_SetInvalidKeyDoesNotCreateFile(t *testing.T) {
	home := t.TempDir()
	r := runCli(t, []string{"config", "set", "invalid_key", "foo"}, runOpts{home: home})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(r.stderr, "invalid_key") {
		t.Fatalf("stderr = %q", r.stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "yoli", "config.json")); err == nil {
		t.Fatalf("file should not exist")
	}
}

func TestConfig_ListShowsAllKeys(t *testing.T) {
	r := runCli(t, []string{"config", "list"}, runOpts{})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d", r.exitCode)
	}
	for _, want := range ConfigKeys {
		if !strings.Contains(r.stdout, want) {
			t.Fatalf("stdout missing %q: %q", want, r.stdout)
		}
	}
	if !strings.Contains(r.stdout, "default") {
		t.Fatalf("no default labels: %q", r.stdout)
	}
}

func TestConfig_ListProjectOverridesUser(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "yoli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"),
		[]byte(`{"default_provider":"faux"}`), 0o644); err != nil {
		t.Fatalf("write user: %v", err)
	}
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".yolirc.json"),
		[]byte(`{"default_provider":"faux"}`), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}
	r := runCli(t, []string{"config", "list"}, runOpts{home: home, cwd: cwd})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d stderr=%q", r.exitCode, r.stderr)
	}
	re := regexp.MustCompile(`default_provider\s*=\s*faux\s+\(project\)`)
	if !re.MatchString(r.stdout) {
		t.Fatalf("missing project label: %q", r.stdout)
	}
}

func TestConfig_ListEnvOverridesProject(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".yolirc.json"),
		[]byte(`{"openrouter_api_key":"project-key"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := runCli(t, []string{"config", "list"}, runOpts{
		home: home, cwd: cwd,
		extraEnv: map[string]string{"OPENROUTER_API_KEY": "env-key"},
	})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d stderr=%q", r.exitCode, r.stderr)
	}
	re := regexp.MustCompile(`openrouter_api_key\s*=\s*env-key\s+\(env\)`)
	if !re.MatchString(r.stdout) {
		t.Fatalf("missing env label: %q", r.stdout)
	}
}

func TestConfig_NoSubPrintsUsage(t *testing.T) {
	r := runCli(t, []string{"config"}, runOpts{})
	if r.exitCode == 0 {
		t.Fatalf("exit = 0")
	}
	if !strings.Contains(strings.ToLower(r.stderr), "usage") {
		t.Fatalf("stderr = %q", r.stderr)
	}
}
