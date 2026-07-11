package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoli/internal/agent/skills"
	"yoli/internal/agent/yolium"
	"yoli/internal/ai"
	"yoli/internal/ai/providers"
	builtinskills "yoli/skills"
)

// loadCLITestSkills writes one SKILL.md per (name, body) pair under a
// tempdir and loads them through the real loader so BodyPath is valid.
func loadCLITestSkills(t *testing.T, defs map[string]string) []skills.LoadedSkill {
	t.Helper()
	dir := t.TempDir()
	for name, body := range defs {
		skillDir := filepath.Join(dir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		content := "---\ndescription: " + name + " skill\ntrigger: when testing " + name + "\n---\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	loaded, err := skills.Load(skills.LoadOptions{ProjectDir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return loaded
}

func hasTool(defs []ai.ToolDefinition, name string) bool {
	for _, td := range defs {
		if td.Name == name {
			return true
		}
	}
	return false
}

func TestRunAgentLoop_SystemPromptIncludesSkillsSection(t *testing.T) {
	skillList := loadCLITestSkills(t, map[string]string{"foo": "# Foo Body"})

	cases := []struct {
		name       string
		yoliumMode bool
		responses  []ai.ChatResponse
	}{
		{
			name: "standalone",
			responses: []ai.ChatResponse{
				{Content: strp(`@@YOLIUM:{"type":"complete","summary":"done"}`)},
			},
		},
		{
			name:       "yolium-mode",
			yoliumMode: true,
			responses: []ai.ChatResponse{
				{ToolCalls: []ai.ToolCall{{
					ID:        "c1",
					Name:      yolium.ToolComplete,
					Arguments: `{"summary":"done"}`,
				}}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			rec := &recordingProvider{inner: providers.NewFauxProvider(tc.responses)}
			code := runAgentLoop(agentLoopConfig{
				provider:   rec,
				model:      "faux",
				prompt:     "go",
				repoPath:   t.TempDir(),
				yoliumMode: tc.yoliumMode,
				skillList:  skillList,
				// AGENT_TOOLS-style whitelist must not strand the
				// advertised skills: Skill registers after filtering.
				whitelist: []string{"Read"},
			}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit = %d stderr=%q", code, stderr.String())
			}
			system := *rec.reqs[0].Messages[0].Content
			if !strings.Contains(system, "## Available Skills") {
				t.Fatalf("system prompt missing skills section: %q", system)
			}
			if !strings.Contains(system, "- foo: foo skill (trigger: when testing foo)") {
				t.Fatalf("system prompt missing skill bullet: %q", system)
			}
			if !hasTool(rec.reqs[0].Tools, "Skill") {
				t.Fatalf("Skill tool not registered: %+v", rec.reqs[0].Tools)
			}
		})
	}
}

func TestRunAgentLoop_NoSkillsNoSectionNoTool(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rec := &recordingProvider{inner: providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp(`@@YOLIUM:{"type":"complete","summary":"done"}`)},
	})}
	code := runAgentLoop(agentLoopConfig{
		provider: rec,
		model:    "faux",
		prompt:   "go",
		repoPath: t.TempDir(),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}
	if strings.Contains(*rec.reqs[0].Messages[0].Content, "## Available Skills") {
		t.Fatalf("unexpected skills section: %q", *rec.reqs[0].Messages[0].Content)
	}
	if hasTool(rec.reqs[0].Tools, "Skill") {
		t.Fatalf("Skill tool registered without skills: %+v", rec.reqs[0].Tools)
	}
}

func TestRunAgentLoop_SkillToolCallReturnsBody(t *testing.T) {
	skillList := loadCLITestSkills(t, map[string]string{"foo": "unique foo methodology text"})
	var stdout, stderr bytes.Buffer
	prov := providers.NewFauxProvider([]ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "s1", Name: "Skill", Arguments: `{"name":"foo"}`}}},
		{Content: strp(`@@YOLIUM:{"type":"complete","summary":"done"}`)},
	})
	code := runAgentLoop(agentLoopConfig{
		provider:  prov,
		model:     "faux",
		prompt:    "go",
		repoPath:  t.TempDir(),
		skillList: skillList,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}
	// The tool result is logged to stderr via logToolResult.
	if !strings.Contains(stderr.String(), "unique foo methodology text") {
		t.Fatalf("skill body missing from tool result log: %q", stderr.String())
	}
}

func TestLoadSkillsForPrompt_LoadsFromUserDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	skillDir := filepath.Join(home, ".yoli", "skills", "homeskill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\ndescription: from home\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var warn bytes.Buffer
	got := loadSkillsForPrompt(&warn)
	// The embedded built-ins (plan) are always present alongside the
	// user skill.
	names := make(map[string]bool)
	for _, s := range got {
		names[s.Name] = true
	}
	if !names["homeskill"] || !names["plan"] {
		t.Fatalf("got %+v", got)
	}
	if warn.Len() != 0 {
		t.Fatalf("unexpected warning: %q", warn.String())
	}
}

func TestLoadSkillsForPrompt_WarnsAndReturnsNilOnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks are bypassed as root")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".yoli", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	var warn bytes.Buffer
	got := loadSkillsForPrompt(&warn)
	if got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
	if !strings.Contains(warn.String(), "continuing without skills") {
		t.Fatalf("warning missing: %q", warn.String())
	}
}

// TestBuiltInPlanSkillParses guards the embedded built-in skill bundle:
// the plan skill must load through the real loader with a description
// and trigger, and its body must expand from the embedded filesystem.
func TestBuiltInPlanSkillParses(t *testing.T) {
	got, err := skills.Load(skills.LoadOptions{BuiltIn: builtinskills.FS})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var plan *skills.LoadedSkill
	for i := range got {
		if got[i].Name == "plan" {
			plan = &got[i]
		}
	}
	if plan == nil {
		t.Fatalf("plan skill not found in repo skills/: %+v", got)
	}
	if plan.Description == "" {
		t.Fatal("plan skill has empty description")
	}
	if plan.Trigger() == "" {
		t.Fatal("plan skill has empty trigger")
	}
	body, err := skills.Expand("plan", got)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if !strings.Contains(body, "Plan Structure") {
		t.Fatalf("plan body missing methodology: %q", body)
	}
}

func TestChatSystemPrompt_AppendsSkillsSection(t *testing.T) {
	skillList := loadCLITestSkills(t, map[string]string{"foo": "body"})
	got := chatSystemPrompt(skillList)
	if !strings.HasPrefix(got, chatSystem) {
		t.Fatalf("base prompt missing: %q", got)
	}
	if !strings.Contains(got, "## Available Skills") {
		t.Fatalf("skills section missing: %q", got)
	}
}

func TestChatSystemPrompt_PlainWhenEmpty(t *testing.T) {
	if got := chatSystemPrompt(nil); got != chatSystem {
		t.Fatalf("got %q, want bare chatSystem", got)
	}
}

func TestCycleSkill_Order(t *testing.T) {
	list := []skills.LoadedSkill{{Name: "alpha"}, {Name: "beta"}}
	cases := []struct {
		current string
		want    string
	}{
		{"", "alpha"},
		{"alpha", "beta"},
		{"beta", ""},
		{"stale-name", ""},
	}
	for _, tc := range cases {
		if got := cycleSkill(tc.current, list); got != tc.want {
			t.Fatalf("cycleSkill(%q) = %q, want %q", tc.current, got, tc.want)
		}
	}
	if got := cycleSkill("", nil); got != "" {
		t.Fatalf("cycleSkill with no skills = %q, want empty", got)
	}
}

func TestTUIPromptPrefix(t *testing.T) {
	if got := tuiPromptPrefix(""); got != "> " {
		t.Fatalf("empty prefix = %q", got)
	}
	if got := tuiPromptPrefix("plan"); got != "[plan] > " {
		t.Fatalf("active prefix = %q", got)
	}
}

func TestTUISystemWithSkill_AppendsBody(t *testing.T) {
	c := &tuiLoopConfig{
		skillList:   loadCLITestSkills(t, map[string]string{"foo": "unique foo methodology text"}),
		activeSkill: "foo",
	}
	var warn bytes.Buffer
	got := tuiSystemWithSkill("BASE", c, &warn)
	if !strings.HasPrefix(got, "BASE") {
		t.Fatalf("base prompt missing: %q", got)
	}
	if !strings.Contains(got, "## Active Skill: foo") ||
		!strings.Contains(got, "unique foo methodology text") {
		t.Fatalf("skill body missing: %q", got)
	}
	if warn.Len() != 0 {
		t.Fatalf("unexpected warning: %q", warn.String())
	}
}

func TestTUISystemWithSkill_FallsBackOnExpandError(t *testing.T) {
	c := &tuiLoopConfig{activeSkill: "ghost"}
	var warn bytes.Buffer
	if got := tuiSystemWithSkill("BASE", c, &warn); got != "BASE" {
		t.Fatalf("got %q, want bare base", got)
	}
	if !strings.Contains(warn.String(), "ghost") {
		t.Fatalf("warning missing: %q", warn.String())
	}
}

func TestTUI_SkillCommandActivatesSkillForNextTurn(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp("ok")},
	})}
	c := newTUITestConfig(rec)
	c.skillList = loadCLITestSkills(t, map[string]string{"foo": "unique foo methodology text"})
	code, stdout, stderr := runTUITest(t, c, "/skill foo\nhello\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "skill set to foo") {
		t.Fatalf("activation feedback missing: %q", stdout)
	}
	if len(rec.reqs) == 0 {
		t.Fatal("provider never called")
	}
	system := *rec.reqs[0].Messages[0].Content
	if !strings.Contains(system, "## Active Skill: foo") ||
		!strings.Contains(system, "unique foo methodology text") {
		t.Fatalf("active skill missing from system prompt: %q", system)
	}
}

func TestTUI_SkillCommandShowsAndClears(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	c := newTUITestConfig(rec)
	c.skillList = loadCLITestSkills(t, map[string]string{"foo": "body"})
	code, stdout, _ := runTUITest(t, c, "/skill\n/skill foo\n/skill off\n/skill\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"skill: none", "available: foo", "skill set to foo", "skill cleared"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %q", want, stdout)
		}
	}
}

func TestTUI_SkillCommandUnknownWarns(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	c := newTUITestConfig(rec)
	c.skillList = loadCLITestSkills(t, map[string]string{"foo": "body"})
	code, _, stderr := runTUITest(t, c, "/skill nope\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr, "unknown skill: nope") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRunTUILoop_SystemPromptIncludesSkills(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strp("ok")},
	})}
	c := newTUITestConfig(rec)
	c.skillList = loadCLITestSkills(t, map[string]string{"foo": "body"})
	code, _, stderr := runTUITest(t, c, "hello\n/exit\n")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, stderr)
	}
	if len(rec.reqs) == 0 {
		t.Fatal("provider never called")
	}
	if !strings.Contains(*rec.reqs[0].Messages[0].Content, "## Available Skills") {
		t.Fatalf("system prompt missing skills section: %q", *rec.reqs[0].Messages[0].Content)
	}
}
