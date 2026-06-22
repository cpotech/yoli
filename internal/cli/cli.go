package cli

import (
	"fmt"
	"io"
	"strings"
)

const usage = `Usage: yoli [--loglevel info|error] <command>
Commands:
  version              Print the yoli version
  chat <prompt>        Run a one-shot agent chat via OpenRouter
  -p, --prompt <text>  Shorthand for ` + "`chat <text>`" + `
  tui                  Run an interactive line-based REPL
  run --role <role>    Run the stdio agent with the given role
  agent                Run the headless agent loop (Yolium protocol)
  session <list|...>   Inspect or operate on session files
  skills <list|show>   Inspect skills available to the agent
  config <get|set|...> Inspect or modify yoli configuration

Session options (may also precede chat):
  -c                   Continue the most recent session for the cwd
  -r                   Resume a session interactively (TTY) or list (non-TTY)
  --no-session         Run without auto-saving a session
  --session <path|id>  Resume a specific session
  --fork <path|id>     Fork a session into a new one
`

// Run dispatches the top-level yoli CLI. It returns the desired process
// exit code: 0 on success, non-zero on any user-visible error.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// Accept --loglevel and session shorthands before the subcommand for
	// ergonomics; reinject them into the per-command args so subcommands
	// like chat see them.
	var pre []string
	chatShorthand := false
	for len(args) > 0 {
		switch {
		case args[0] == "--loglevel":
			if len(args) < 2 {
				fmt.Fprintln(stderr, "--loglevel requires a value")
				return 1
			}
			pre = append(pre, args[0], args[1])
			args = args[2:]
		case strings.HasPrefix(args[0], "--loglevel="):
			pre = append(pre, args[0])
			args = args[1:]
		case args[0] == "--session" || args[0] == "--fork":
			if len(args) < 2 {
				fmt.Fprintln(stderr, args[0]+" requires a value")
				return 1
			}
			pre = append(pre, args[0], args[1])
			args = args[2:]
			chatShorthand = true
		case strings.HasPrefix(args[0], "--session=") || strings.HasPrefix(args[0], "--fork="):
			pre = append(pre, args[0])
			args = args[1:]
			chatShorthand = true
		case args[0] == "--no-session" || args[0] == "-c" || args[0] == "-r":
			pre = append(pre, args[0])
			args = args[1:]
			chatShorthand = true
		default:
			goto done
		}
	}
done:
	if len(args) == 0 && !chatShorthand {
		fmt.Fprint(stderr, usage)
		return 1
	}
	// Session shorthands imply chat: forward everything to runChat.
	if chatShorthand {
		merged := append(append([]string{}, pre...), args...)
		return runChat(merged, stdout, stderr)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "chat", "-p", "--prompt", "tui":
		rest = append(append([]string{}, pre...), rest...)
	default:
		if len(pre) > 0 {
			fmt.Fprintln(stderr, "--loglevel is only valid for chat/-p/--prompt/tui")
			return 1
		}
	}
	switch sub {
	case "version", "--version":
		fmt.Fprintln(stdout, Version)
		return 0
	case "chat":
		return runChat(rest, stdout, stderr)
	case "-p", "--prompt":
		return runChat(rest, stdout, stderr)
	case "tui":
		return runTUI(rest, stdin, stdout, stderr)
	case "run":
		return runRun(rest, stdin, stdout, stderr)
	case "agent":
		return runAgent(rest, stdout, stderr)
	case "session":
		return runSession(rest, stdout, stderr)
	case "skills":
		return runSkills(rest, stdout, stderr)
	case "config":
		return runConfig(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n", sub)
		return 1
	}
}
