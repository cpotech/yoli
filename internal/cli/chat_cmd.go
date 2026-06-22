package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"yoli/internal/agent"
	agentsession "yoli/internal/agent/session"
	"yoli/internal/agent/tools"
	"yoli/internal/ai"
	"yoli/internal/ai/providers"
)

const chatUsage = "Usage: yoli chat [--loglevel debug|info|error|none] <prompt>\n"

// Log level ranks: higher = more verbose. A message tagged at rank R
// is shown when the active level's rank is >= R.
const (
	logRankNone  = 0
	logRankError = 1
	logRankInfo  = 2
	logRankDebug = 3
)

func logRank(level string) int {
	switch level {
	case "none":
		return logRankNone
	case "error":
		return logRankError
	case "info":
		return logRankInfo
	case "debug":
		return logRankDebug
	}
	return -1
}

const chatSystem = "You are Yoli, a small coding agent. " +
	"Use the provided tools to inspect and modify the user's working directory. " +
	"Use Agent to delegate a focused sub-task to another role (e.g. planner, reviewer) in an isolated subprocess. " +
	"Keep responses concise."

type chatFlags struct {
	LogLevel    string
	Continue    bool
	Resume      bool
	NoSession   bool
	Session     string
	Fork        string
	SessionRoot string
}

// parseChatFlags extracts known flags from the chat args and returns
// the remaining tokens, which form the prompt.
func parseChatFlags(args []string) (chatFlags, []string, error) {
	f := chatFlags{LogLevel: "info"}
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--loglevel":
			if i+1 >= len(args) {
				return f, nil, fmt.Errorf("--loglevel requires a value")
			}
			f.LogLevel = args[i+1]
			i++
		case strings.HasPrefix(arg, "--loglevel="):
			f.LogLevel = strings.TrimPrefix(arg, "--loglevel=")
		case arg == "-c":
			f.Continue = true
		case arg == "-r":
			f.Resume = true
		case arg == "--no-session":
			f.NoSession = true
		case arg == "--session":
			if i+1 >= len(args) {
				return f, nil, fmt.Errorf("--session requires a value")
			}
			f.Session = args[i+1]
			i++
		case strings.HasPrefix(arg, "--session="):
			f.Session = strings.TrimPrefix(arg, "--session=")
		case arg == "--fork":
			if i+1 >= len(args) {
				return f, nil, fmt.Errorf("--fork requires a value")
			}
			f.Fork = args[i+1]
			i++
		case strings.HasPrefix(arg, "--fork="):
			f.Fork = strings.TrimPrefix(arg, "--fork=")
		default:
			rest = append(rest, arg)
		}
	}
	if logRank(f.LogLevel) < 0 {
		return f, nil, fmt.Errorf("invalid --loglevel %q (want debug, info, error, or none)", f.LogLevel)
	}
	return f, rest, nil
}

// resolveChatSession returns the session to use for this chat
// invocation, honouring the chat flags: NoSession yields an in-memory
// session, Fork copies a source, Session resumes a specific spec,
// Continue picks the most recent for cwd, and the default creates a
// fresh session.
func resolveChatSession(f chatFlags, cwd string, in io.Reader) (*agentsession.Session, error) {
	opts := agentsession.Options{RootDir: f.SessionRoot, Cwd: cwd}
	switch {
	case f.NoSession:
		return agentsession.InMemory(opts), nil
	case f.Fork != "":
		return agentsession.ForkFrom(opts, f.Fork)
	case f.Session != "":
		return agentsession.Resolve(opts, f.Session)
	case f.Continue:
		return agentsession.ContinueRecent(opts)
	case f.Resume:
		return resumeInteractive(opts, in)
	default:
		return agentsession.Create(opts)
	}
}

// resumeInteractive prompts on stdin (when in is non-nil) for one of
// the listed sessions; falls back to creating a new session if no
// reader is available.
func resumeInteractive(opts agentsession.Options, in io.Reader) (*agentsession.Session, error) {
	sessions, err := agentsession.List(opts)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return agentsession.Create(opts)
	}
	if in == nil {
		return nil, fmt.Errorf("resume: no input reader; pass --session <id> instead")
	}
	for i, s := range sessions {
		fmt.Printf("%d) %s\n", i+1, s.GetSessionID())
	}
	br := bufio.NewReader(in)
	line, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	idx, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || idx < 1 || idx > len(sessions) {
		return nil, fmt.Errorf("invalid selection %q", strings.TrimSpace(line))
	}
	return sessions[idx-1], nil
}

// summarizeArgs returns a single-line rendering of a tool call's raw
// JSON arguments. If max > 0, the result is truncated to that length.
func summarizeArgs(raw string, max int) string {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "\n", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	if max > 0 && len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// isToolError reports whether a tool result body is an error produced by
// the agent loop (unknown tool / bad args / tool failure). Matches the
// prefixes emitted by agent.runToolCall.
func isToolError(body string) bool {
	return strings.HasPrefix(body, "Error:") || strings.HasPrefix(body, "Error running ")
}

// summarizeResult collapses a tool result to one line for debug logs.
func summarizeResult(raw string, max int) string {
	s := strings.ReplaceAll(raw, "\n", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	if max > 0 && len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// runChat implements `yoli chat <prompt>` (and its -p / --prompt
// aliases). It performs the full agent loop against OpenRouter with
// the default tool set plus a sub-agent dispatcher.
func runChat(args []string, stdout, stderr io.Writer) int {
	flags, rest, err := parseChatFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprint(stderr, chatUsage)
		return 1
	}
	prompt := strings.TrimSpace(strings.Join(rest, " "))
	if prompt == "" {
		fmt.Fprint(stderr, chatUsage)
		return 1
	}
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptionsFromEnv(),
		Warnings:    stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ApplyEnvDefaults(cfg)
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		fmt.Fprint(stderr, "Error: OPENROUTER_API_KEY is not set\n")
		return 1
	}
	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		model = defaultModel
	}
	rank := logRank(flags.LogLevel)
	if rank >= logRankInfo {
		fmt.Fprintf(stderr, "yoli: model=%s\n", model)
	}
	provider, err := providers.NewOpenRouterProvider(providers.OpenRouterOptions{
		APIKey:  os.Getenv("OPENROUTER_API_KEY"),
		Referer: "https://github.com/yolium/yoli",
		Title:   "Yoli",
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	toolset := append(
		tools.DefaultTools(cwd),
		tools.NewSubAgentTool(tools.SubAgentOptions{
			CLIEntry:     exe,
			DefaultModel: model,
		}),
	)
	system := chatSystem
	user := prompt
	sess, err := resolveChatSession(flags, cwd, os.Stdin)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	seed := []ai.Message{{Role: ai.RoleSystem, Content: &system}}
	seed = append(seed, sess.BuildMessages()...)
	seed = append(seed, ai.Message{Role: ai.RoleUser, Content: &user})
	if rank >= logRankInfo {
		fmt.Fprintf(stderr, "yoli: context-size: %s\n", formatContextSize(agent.EstimateContextTokens(seed), agent.DefaultContextBudget))
	}
	if _, err := sess.AppendMessage(ai.Message{Role: ai.RoleUser, Content: &user}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	nameByID := map[string]string{}
	messages, err := agent.Run(context.Background(), agent.RunOptions{
		Provider: provider,
		Model:    model,
		Tools:    toolset,
		Messages: seed,
		OnMessage: func(m ai.Message) {
			if m.Role == ai.RoleAssistant || m.Role == ai.RoleTool {
				_, _ = sess.AppendMessage(m)
			}
			switch m.Role {
			case ai.RoleAssistant:
				if m.Content != nil && *m.Content != "" {
					fmt.Fprintln(stdout, *m.Content)
				}
				for _, tc := range m.ToolCalls {
					nameByID[tc.ID] = tc.Name
					switch {
					case rank >= logRankDebug:
						fmt.Fprintf(stderr, "yoli: → %s %s\n", tc.Name, summarizeArgs(tc.Arguments, 0))
					case rank >= logRankInfo:
						fmt.Fprintf(stderr, "yoli: → %s %s\n", tc.Name, summarizeArgs(tc.Arguments, 120))
					}
				}
			case ai.RoleTool:
				name := nameByID[m.ToolCallID]
				if name == "" {
					name = "tool"
				}
				body := ""
				if m.Content != nil {
					body = *m.Content
				}
				isErr := isToolError(body)
				switch {
				case rank >= logRankDebug:
					fmt.Fprintf(stderr, "yoli: ← %s (%d bytes) %s\n", name, len(body), summarizeResult(body, 200))
				case rank >= logRankInfo:
					fmt.Fprintf(stderr, "yoli: ← %s (%d bytes)\n", name, len(body))
				case rank >= logRankError && isErr:
					fmt.Fprintf(stderr, "yoli: ← %s %s\n", name, summarizeResult(body, 200))
				}
			}
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if n := len(messages); n > 0 {
		last := messages[n-1]
		if last.Role == ai.RoleAssistant && (last.Content == nil || *last.Content == "") {
			fmt.Fprintln(stderr, "(no final assistant message)")
		}
	}
	return 0
}
