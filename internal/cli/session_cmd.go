package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	agentsession "yoli/internal/agent/session"
	"yoli/internal/ai"
)

const sessionUsage = `Usage: yoli session <list|current|tree|branch> [flags]
Subcommands:
  list [--all] [--cwd <dir>] [--root <dir>]   List sessions
  current --session <path|id> [--root <dir>]  Print info for a session
  tree --session <path|id> [--root <dir>]     Print the entry tree
  branch --session <path|id> --entry <id>     Move the active leaf
`

// runSession implements the `yoli session` subcommand.
func runSession(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, sessionUsage)
		return 1
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return runSessionList(rest, stdout, stderr)
	case "current":
		return runSessionCurrent(rest, stdout, stderr)
	case "tree":
		return runSessionTree(rest, stdout, stderr)
	case "branch":
		return runSessionBranch(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown session subcommand: %s\n", sub)
		return 1
	}
}

type sessionListFlags struct {
	All  bool
	Cwd  string
	Root string
}

func parseSessionListFlags(args []string) (sessionListFlags, error) {
	f := sessionListFlags{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all":
			f.All = true
		case "--cwd":
			if i+1 >= len(args) {
				return f, fmt.Errorf("--cwd requires a value")
			}
			f.Cwd = args[i+1]
			i++
		case "--root":
			if i+1 >= len(args) {
				return f, fmt.Errorf("--root requires a value")
			}
			f.Root = args[i+1]
			i++
		default:
			return f, fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	return f, nil
}

func runSessionList(args []string, stdout, stderr io.Writer) int {
	flags, err := parseSessionListFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	opts := agentsession.Options{RootDir: flags.Root}
	var sessions []*agentsession.Session
	if flags.All {
		sessions, err = agentsession.ListAll(opts)
	} else {
		cwd := flags.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		opts.Cwd = cwd
		sessions, err = agentsession.List(opts)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, s := range sessions {
		first := firstUserText(s)
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", s.GetSessionID(), s.GetHeader().Cwd, first)
	}
	return 0
}

type sessionRefFlags struct {
	Session string
	Root    string
	Entry   string
}

func parseSessionRefFlags(args []string, allowEntry bool) (sessionRefFlags, error) {
	f := sessionRefFlags{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--session":
			if i+1 >= len(args) {
				return f, fmt.Errorf("--session requires a value")
			}
			f.Session = args[i+1]
			i++
		case "--root":
			if i+1 >= len(args) {
				return f, fmt.Errorf("--root requires a value")
			}
			f.Root = args[i+1]
			i++
		case "--entry":
			if !allowEntry {
				return f, fmt.Errorf("unknown flag: --entry")
			}
			if i+1 >= len(args) {
				return f, fmt.Errorf("--entry requires a value")
			}
			f.Entry = args[i+1]
			i++
		default:
			return f, fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	return f, nil
}

func runSessionCurrent(args []string, stdout, stderr io.Writer) int {
	flags, err := parseSessionRefFlags(args, false)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if flags.Session == "" {
		fmt.Fprintln(stderr, "session current: --session is required")
		return 1
	}
	s, err := agentsession.Resolve(agentsession.Options{RootDir: flags.Root}, flags.Session)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "id:    %s\n", s.GetSessionID())
	fmt.Fprintf(stdout, "file:  %s\n", s.GetSessionFile())
	fmt.Fprintf(stdout, "cwd:   %s\n", s.GetHeader().Cwd)
	fmt.Fprintf(stdout, "leaf:  %s\n", s.GetLeafID())
	return 0
}

func runSessionTree(args []string, stdout, stderr io.Writer) int {
	flags, err := parseSessionRefFlags(args, false)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if flags.Session == "" {
		fmt.Fprintln(stderr, "session tree: --session is required")
		return 1
	}
	s, err := agentsession.Resolve(agentsession.Options{RootDir: flags.Root}, flags.Session)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	tree := s.GetTree()
	printTree(stdout, tree, "", 0, s.GetLeafID())
	return 0
}

func runSessionBranch(args []string, stdout, stderr io.Writer) int {
	flags, err := parseSessionRefFlags(args, true)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if flags.Session == "" {
		fmt.Fprintln(stderr, "session branch: --session is required")
		return 1
	}
	if flags.Entry == "" {
		fmt.Fprintln(stderr, "session branch: --entry is required")
		return 1
	}
	s, err := agentsession.Resolve(agentsession.Options{RootDir: flags.Root}, flags.Session)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := s.Branch(flags.Entry); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "leaf moved to %s\n", flags.Entry)
	return 0
}

func printTree(out io.Writer, tree map[string][]agentsession.Entry, parent string, depth int, leaf string) {
	children := tree[parent]
	sort.SliceStable(children, func(i, j int) bool {
		return children[i].Timestamp < children[j].Timestamp
	})
	for _, c := range children {
		indent := strings.Repeat("  ", depth)
		marker := " "
		if c.ID == leaf {
			marker = "*"
		}
		role := ""
		text := ""
		if c.Message != nil {
			role = string(c.Message.Role)
			if c.Message.Content != nil {
				text = previewText(*c.Message.Content)
			}
		}
		fmt.Fprintf(out, "%s%s %s [%s] %s\n", indent, marker, c.ID, role, text)
		printTree(out, tree, c.ID, depth+1, leaf)
	}
}

func previewText(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func firstUserText(s *agentsession.Session) string {
	for _, e := range s.GetEntries() {
		if e.Message != nil && e.Message.Role == ai.RoleUser && e.Message.Content != nil {
			return previewText(*e.Message.Content)
		}
	}
	return ""
}
