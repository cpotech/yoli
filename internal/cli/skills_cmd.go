package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"yoli/internal/agent/skills"
)

// ResolvedSkillDirs holds the three directories the skill loader scans.
// UserDir is empty when no $HOME is available.
type ResolvedSkillDirs struct {
	ProjectDir string
	UserDir    string
	BuiltInDir string
}

// ResolveSkillDirs returns the canonical skill-directory layout used by
// the CLI. BuiltInDir is anchored to <dirname(cliEntry)>/../skills so
// it tracks the binary's installation location.
func ResolveSkillDirs(cwd, home, cliEntry string) ResolvedSkillDirs {
	var userDir string
	if home != "" {
		userDir = filepath.Join(home, ".yoli", "skills")
	}
	return ResolvedSkillDirs{
		ProjectDir: filepath.Join(cwd, ".yoli", "skills"),
		UserDir:    userDir,
		BuiltInDir: filepath.Join(filepath.Dir(cliEntry), "..", "skills"),
	}
}

// FormatSkillsList renders one line per skill ("<name>  [<origin>]
// <description>") preserving input order. An empty list produces a
// single-line empty-state message.
func FormatSkillsList(list []skills.LoadedSkill) string {
	if len(list) == 0 {
		return "No skills found."
	}
	var b strings.Builder
	for i, s := range list {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s  [%s]  %s", s.Name, s.Origin, s.Description)
	}
	return b.String()
}

const skillsUsage = `Usage: yoli skills <list|show>
  skills list          List skills available to the agent
  skills show <name>   Print the SKILL.md body for the named skill
`

const skillsShowUsage = "Usage: yoli skills show <name>\n"

func runSkills(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, skillsUsage)
		return 1
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return runSkillsList(stdout, stderr)
	case "show":
		return runSkillsShow(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Unknown skills subcommand: %s\n%s", sub, skillsUsage)
		return 1
	}
}

func loadSkillsFromEnv() ([]skills.LoadedSkill, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	home := os.Getenv("HOME")
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	dirs := ResolveSkillDirs(cwd, home, exe)
	return skills.Load(skills.LoadOptions{
		ProjectDir: dirs.ProjectDir,
		UserDir:    dirs.UserDir,
		BuiltInDir: dirs.BuiltInDir,
	})
}

func runSkillsList(stdout, stderr io.Writer) int {
	list, err := loadSkillsFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, FormatSkillsList(list))
	return 0
}

func runSkillsShow(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, skillsShowUsage)
		return 1
	}
	name := args[0]
	list, err := loadSkillsFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	body, err := skills.Expand(name, list)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", err.Error())
		return 1
	}
	fmt.Fprintln(stdout, body)
	return 0
}
