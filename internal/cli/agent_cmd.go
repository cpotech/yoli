package cli

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"yoli/internal/agent"
	agentsession "yoli/internal/agent/session"
	"yoli/internal/agent/tools"
	"yoli/internal/agent/yolium"
	"yoli/internal/ai"
	"yoli/internal/ai/providers"
)

const defaultModel = "openrouter/free"

const agentUsage = `Usage: yoli agent [--model <slug>] [--tools <comma-separated>] [--prompt-file <file>] [--prompt <text>] [--yolium-mode] [--events-fd <N>]

Run the headless agent loop. Reads prompt from AGENT_PROMPT_FILE (file)
or AGENT_PROMPT (base64) or --prompt-file or --prompt.

Flags:
  --yolium-mode      Register yolium_* protocol tools, change loop-exit
                     semantics, and treat terminator tools (yolium_complete /
                     yolium_error / yolium_ask_question) as the sole way to
                     end the loop. Standalone yoli runs should leave this off.
  --events-fd N      Write structured NDJSON events (one event per line) to
                     the given file descriptor. The events channel is used
                     when --yolium-mode is enabled. Defaults unset (events
                     discarded). Yolium passes fd=3.

Environment:
  AGENT_MODEL          Model slug (default: openrouter/free)
  AGENT_TOOLS          Comma-separated tool whitelist (default: all)
  AGENT_PROMPT_FILE    Path to prompt file
  AGENT_PROMPT         Base64-encoded prompt text
  AGENT_GOAL           Base64-encoded goal description
  OPENROUTER_API_KEY   Required
  YOLIUM_CAVEMAN_MODE  off | lite | full | ultra (yolium-mode only)
`

type agentFlags struct {
	Model      string
	Tools      string
	PromptFile string
	Prompt     string
	YoliumMode bool
	EventsFD   int
	Session    string
	Fork       string
	Continue   bool
	NoSession  bool
}

func parseAgentFlags(args []string) (agentFlags, error) {
	f := agentFlags{}
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--model":
			if i+1 >= len(args) {
				return f, errors.New("--model requires a value")
			}
			f.Model = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--model="):
			f.Model = strings.TrimPrefix(args[i], "--model=")
		case args[i] == "--tools":
			if i+1 >= len(args) {
				return f, errors.New("--tools requires a value")
			}
			f.Tools = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--tools="):
			f.Tools = strings.TrimPrefix(args[i], "--tools=")
		case args[i] == "--prompt-file":
			if i+1 >= len(args) {
				return f, errors.New("--prompt-file requires a value")
			}
			f.PromptFile = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--prompt-file="):
			f.PromptFile = strings.TrimPrefix(args[i], "--prompt-file=")
		case args[i] == "--prompt":
			if i+1 >= len(args) {
				return f, errors.New("--prompt requires a value")
			}
			f.Prompt = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--prompt="):
			f.Prompt = strings.TrimPrefix(args[i], "--prompt=")
		case args[i] == "--yolium-mode":
			f.YoliumMode = true
		case args[i] == "--events-fd":
			if i+1 >= len(args) {
				return f, errors.New("--events-fd requires a value")
			}
			fd, err := strconv.Atoi(args[i+1])
			if err != nil {
				return f, fmt.Errorf("--events-fd: %w", err)
			}
			f.EventsFD = fd
			i++
		case strings.HasPrefix(args[i], "--events-fd="):
			fd, err := strconv.Atoi(strings.TrimPrefix(args[i], "--events-fd="))
			if err != nil {
				return f, fmt.Errorf("--events-fd: %w", err)
			}
			f.EventsFD = fd
		case args[i] == "--session":
			if i+1 >= len(args) {
				return f, errors.New("--session requires a value")
			}
			f.Session = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--session="):
			f.Session = strings.TrimPrefix(args[i], "--session=")
		case args[i] == "--fork":
			if i+1 >= len(args) {
				return f, errors.New("--fork requires a value")
			}
			f.Fork = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--fork="):
			f.Fork = strings.TrimPrefix(args[i], "--fork=")
		case args[i] == "--continue":
			f.Continue = true
		case args[i] == "--no-session":
			f.NoSession = true
		default:
			return f, fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	return f, nil
}

func readPrompt(f agentFlags) (string, error) {
	// Priority: AGENT_PROMPT_FILE env, --prompt-file flag, --prompt flag, AGENT_PROMPT env
	if pf := os.Getenv("AGENT_PROMPT_FILE"); pf != "" {
		data, err := os.ReadFile(pf)
		if err != nil {
			return "", fmt.Errorf("reading AGENT_PROMPT_FILE %s: %w", pf, err)
		}
		return string(data), nil
	}
	if f.PromptFile != "" {
		data, err := os.ReadFile(f.PromptFile)
		if err != nil {
			return "", fmt.Errorf("reading prompt file %s: %w", f.PromptFile, err)
		}
		return string(data), nil
	}
	if f.Prompt != "" {
		return f.Prompt, nil
	}
	if b64 := os.Getenv("AGENT_PROMPT"); b64 != "" {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return "", fmt.Errorf("decoding AGENT_PROMPT: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}

func readGoal() string {
	if b64 := os.Getenv("AGENT_GOAL"); b64 != "" {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err == nil {
			return string(data)
		}
	}
	return ""
}

// filterAgentTools filters a tool list to only include tools whose names
// appear in the whitelist. If whitelist is nil or empty, all tools are
// returned. Unknown names in the whitelist are silently ignored.
// ask_question is NEVER included in the result (headless safety).
func filterAgentTools(all []tools.Tool, whitelist []string) []tools.Tool {
	if len(whitelist) == 0 {
		var out []tools.Tool
		for _, t := range all {
			if t.Definition().Name != "ask_question" {
				out = append(out, t)
			}
		}
		return out
	}
	set := make(map[string]bool, len(whitelist))
	for _, w := range whitelist {
		set[w] = true
	}
	var out []tools.Tool
	for _, t := range all {
		n := t.Definition().Name
		if n == "ask_question" {
			continue
		}
		if set[n] {
			out = append(out, t)
		}
	}
	return out
}

// usageRecordingProvider wraps ai.Provider and emits usage/cost progress
// events for each Chat call via the given io.Writer.
type usageRecordingProvider struct {
	inner      ai.Provider
	out        io.Writer
	total      ai.Usage
	turnNumber int
}

func newUsageRecordingProvider(inner ai.Provider, out io.Writer) *usageRecordingProvider {
	return &usageRecordingProvider{inner: inner, out: out}
}

func (p *usageRecordingProvider) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	resp, err := p.inner.Chat(ctx, req)
	if err != nil {
		return resp, err
	}
	if resp.Usage != nil {
		p.turnNumber++
		p.total.PromptTokens += resp.Usage.PromptTokens
		p.total.CompletionTokens += resp.Usage.CompletionTokens
		p.total.TotalTokens += resp.Usage.TotalTokens
		p.total.Cost += resp.Usage.Cost
		detail := fmt.Sprintf("input:%d output:%d total:%d cost:$%.6f",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens,
			resp.Usage.TotalTokens, resp.Usage.Cost)
		yolium.Emit(p.out, yolium.ProgressEvent{
			Step:   "usage",
			Detail: detail,
		})
	}
	return resp, nil
}

func (p *usageRecordingProvider) Cumulative() ai.Usage {
	return p.total
}

// resolveModel returns the model slug, preferring the flag, then AGENT_MODEL,
// then the free default. The slug is passed through to OpenRouter unchanged.
func resolveModel(flagModel string) string {
	if flagModel != "" {
		return flagModel
	}
	if m := os.Getenv("AGENT_MODEL"); m != "" {
		return m
	}
	return defaultModel
}

// resolveRepoPath returns the working directory the agent operates on.
// It uses the process working directory; callers are responsible for
// launching yoli from the directory they want tools to operate in.
func resolveRepoPath() string {
	cwd, _ := os.Getwd()
	return cwd
}

// parseToolWhitelist splits a comma-separated tool list, trimming spaces.
func parseToolWhitelist(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// agentLoopConfig holds the resolved inputs for runAgentLoop. Separating
// this from provider/flag construction keeps the loop testable with a
// faux provider and no network access.
type agentLoopConfig struct {
	provider   ai.Provider
	model      string
	prompt     string
	goal       string
	exe        string
	whitelist  []string
	repoPath   string
	yoliumMode bool
	// eventSink receives structured events when yoliumMode is on. Nil
	// is treated as yolium.NopSink. Standalone runs always use NopSink.
	eventSink      yolium.EventSink
	sessionRoot    string
	sessionTarget  string
	sessionFork    string
	sessionContinue bool
	noSession       bool
}

// runAgent implements the `yoli agent` subcommand: it resolves flags, env,
// and the OpenRouter provider, then drives the headless loop.
func runAgent(args []string, stdout, stderr io.Writer) int {
	flags, err := parseAgentFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprint(stderr, agentUsage)
		return 1
	}

	prompt, err := readPrompt(flags)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if prompt == "" {
		fmt.Fprint(stderr, agentUsage)
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

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		if k, ok := cfg["openrouter_api_key"]; ok && k != "" {
			apiKey = k
		}
	}
	if apiKey == "" {
		fmt.Fprint(stderr, "Error: OPENROUTER_API_KEY is not set\n")
		return 1
	}

	model := resolveModel(flags.Model)
	goal := readGoal()

	fmt.Fprintf(stderr, "yoli: model=%s\n", model)
	preview := prompt
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	fmt.Fprintf(stderr, "yoli: prompt=%s\n", preview)
	if goal != "" {
		fmt.Fprintf(stderr, "yoli: goal=%s\n", goal)
	}

	provider, err := providers.NewOpenRouterProvider(providers.OpenRouterOptions{
		APIKey:  apiKey,
		Referer: "https://github.com/yolium/yoli",
		Title:   "Yoli Agent",
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	whitelist := parseToolWhitelist(firstNonEmpty(flags.Tools, os.Getenv("AGENT_TOOLS")))
	exe, _ := os.Executable()

	var sink yolium.EventSink = yolium.NopSink()
	if flags.YoliumMode && flags.EventsFD > 0 {
		// Adopt the inherited fd as an *os.File for writing. The fd
		// is provided by the parent (Yolium) which opened the write
		// end of an OS pipe; we never close it here so the writer
		// stays open for the lifetime of the process.
		fdFile := os.NewFile(uintptr(flags.EventsFD), fmt.Sprintf("/dev/fd/%d", flags.EventsFD))
		if fdFile == nil {
			fmt.Fprintf(stderr, "Error: --events-fd=%d is not a valid file descriptor\n", flags.EventsFD)
			return 1
		}
		sink = yolium.NewNDJSONSink(fdFile)
	}

	return runAgentLoop(agentLoopConfig{
		provider:        provider,
		model:           model,
		prompt:          prompt,
		goal:            goal,
		exe:             exe,
		whitelist:       whitelist,
		repoPath:        resolveRepoPath(),
		yoliumMode:      flags.YoliumMode,
		eventSink:       sink,
		sessionTarget:   firstNonEmpty(flags.Session, os.Getenv("AGENT_SESSION")),
		sessionFork:     firstNonEmpty(flags.Fork, os.Getenv("AGENT_FORK")),
		sessionContinue: flags.Continue || os.Getenv("AGENT_CONTINUE") != "",
		noSession:       flags.NoSession,
	}, stdout, stderr)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// runAgentLoop drives the headless agent loop with a resolved config. It is
// provider-agnostic so tests can inject a faux provider.
func runAgentLoop(c agentLoopConfig, stdout, stderr io.Writer) int {
	exit := yolium.NewExitSignal()
	sink := c.eventSink
	if sink == nil {
		sink = yolium.NopSink()
	}

	// Remove any stale .yolium/summary.md left by a prior agent run on
	// the same worktree (e.g. plan-agent → code-agent on the same
	// branch). The fallback path below uses summary.md as a graceful
	// recovery signal for "model finished but forgot the terminator";
	// without this cleanup that signal silently mixes prior runs'
	// summaries into the current run's `yolium_complete` event.
	removeStaleSummaryFile(c.repoPath)

	sess, err := resolveAgentSession(c)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	defaultToolset := tools.DefaultTools(c.repoPath)
	filteredTools := filterAgentTools(defaultToolset, c.whitelist)

	subAgent := tools.NewSubAgentTool(tools.SubAgentOptions{
		CLIEntry:     c.exe,
		DefaultModel: c.model,
	})

	allTools := append(filteredTools, subAgent)

	// Under --yolium-mode, register the yolium_* protocol tools. The
	// terminator tools (yolium_complete, yolium_error, yolium_ask_question)
	// set exit.Pending which is checked by Stop() below. Non-terminator
	// tools (progress, comment, etc.) emit structured events via the
	// EventSink and return short acks.
	if c.yoliumMode {
		allTools = append(allTools, yolium.NewTools(sink, exit)...)
	}

	for i, t := range allTools {
		allTools[i] = tools.WithOutputCap(t, agent.DefaultToolOutputBytes)
	}

	recordingProvider := newUsageRecordingProvider(c.provider, stdout)

	// Under --yolium-mode, yoli is the sole emitter of the mandatory
	// first protocol message (model declaration). This replaces the
	// pre-exec `echo "@@YOLIUM:{...progress,model...}"` in yolium's
	// agent.sh.
	if c.yoliumMode {
		_ = sink.Emit(yolium.ProgressEvent{
			Step:   "model",
			Detail: "yoli/" + c.model,
		})
	}

	var system string
	if c.yoliumMode {
		system = "You are Yoli, a headless coding agent integrated with Yolium. " +
			"Use the provided tools to inspect and modify the working directory. " +
			"Communicate progress and final results by calling the `yolium_*` " +
			"protocol tools: yolium_progress, yolium_add_comment, " +
			"yolium_update_description, yolium_set_test_specs, " +
			"yolium_create_item, yolium_action, yolium_start_agent for " +
			"non-terminating events; and exactly one of yolium_complete, " +
			"yolium_error, or yolium_ask_question to end the run. " +
			"DO NOT emit `@@YOLIUM:{...}` lines as plain text — under " +
			"--yolium-mode those are ignored. The terminator tools " +
			"are the ONLY way to end the loop; a turn with no tool calls " +
			"is treated as 'keep going'. If you cannot finish the task, " +
			"call yolium_error with a one-sentence reason."
	} else {
		system = "You are Yoli, a headless coding agent. " +
			"Use the provided tools to inspect and modify the working directory. " +
			"Communicate progress and final results by writing protocol lines " +
			"DIRECTLY in your assistant text — one line per message, in the form " +
			"`@@YOLIUM:<json>`. " +
			"Examples (paste these as your own message text, not as tool arguments): " +
			`@@YOLIUM:{"type":"progress","step":"plan","detail":"reading src/"} ` +
			`@@YOLIUM:{"type":"comment","text":"found 3 candidates"} ` +
			`@@YOLIUM:{"type":"ask_question","text":"which library?","options":["a","b"]} ` +
			`@@YOLIUM:{"type":"complete","summary":"all tests pass"} ` +
			`@@YOLIUM:{"type":"error","message":"could not build"}. ` +
			"NEVER use the Bash tool with `echo '@@YOLIUM:...'` to emit protocol " +
			"lines — type them straight into your text response. Bash echoes waste " +
			"a tool call and force a follow-up turn. " +
			"Every run MUST end with exactly one of `complete`, `error`, or " +
			"`ask_question`. Emitting any of those three ends the run immediately " +
			"— do not call tools or write more text after the terminating line. " +
			"There is no `complete` tool; the line itself is the signal. If you " +
			"cannot finish the task, emit `error` with a one-sentence reason " +
			"rather than going silent."
	}

	messages := []ai.Message{
		{Role: ai.RoleSystem, Content: &system},
	}
	messages = append(messages, sess.BuildMessages()...)
	if c.goal != "" {
		goalMsg := "Goal: " + c.goal
		messages = append(messages, ai.Message{Role: ai.RoleUser, Content: &goalMsg})
		_, _ = sess.AppendMessage(ai.Message{Role: ai.RoleUser, Content: &goalMsg})
	}
	messages = append(messages, ai.Message{Role: ai.RoleUser, Content: &c.prompt})
	_, _ = sess.AppendMessage(ai.Message{Role: ai.RoleUser, Content: &c.prompt})

	fmt.Fprintf(stderr, "yoli: context-size: %s\n", formatContextSize(agent.EstimateContextTokens(messages), agent.DefaultContextBudget))

	turn := 0
	// bashCallIDs tracks tool-call IDs whose call name was "Bash" so we
	// can scan only Bash tool results for protocol lines when they come
	// back. Scanning every tool result is unsafe — Read/Glob/Grep can
	// return file contents that legitimately mention `@@YOLIUM:` (e.g.
	// agent definition docs with example lines) and would otherwise be
	// misparsed as terminal events.
	bashCallIDs := make(map[string]bool)
	onMessage := func(m ai.Message) {
		switch m.Role {
		case ai.RoleAssistant:
			turn++
			if m.Content != nil && *m.Content != "" {
				logAssistantContent(stderr, turn, *m.Content)
				dispatchAssistantEvents(*m.Content, stdout, exit)
				// Under --yolium-mode, mirror the raw assistant text on the
				// structured EventSink so Yolium can populate its
				// agentMessageTexts list (used for the "non-Claude provider
				// synthetic comment" fallback and for "Agent never started"
				// failure classification).
				if c.yoliumMode {
					_ = sink.Emit(yolium.AssistantTurnEvent{
						Turn: turn,
						Text: *m.Content,
					})
				}
			}
			for _, call := range m.ToolCalls {
				logToolCall(stderr, turn, call)
				if call.Name == "Bash" {
					bashCallIDs[call.ID] = true
				}
			}
			_, _ = sess.AppendMessage(m)
		case ai.RoleTool:
			if m.Content != nil {
				logToolResult(stderr, turn, m.ToolCallID, *m.Content)
				if bashCallIDs[m.ToolCallID] {
					dispatchBashResultEvents(*m.Content, stdout, exit)
					delete(bashCallIDs, m.ToolCallID)
				}
			}
			_, _ = sess.AppendMessage(m)
		}
	}

	conv, runErr := agent.Run(context.Background(), agent.RunOptions{
		Provider:   recordingProvider,
		Model:      c.model,
		Tools:      allTools,
		Messages:   messages,
		OnMessage:  onMessage,
		Stop:       func() bool { return exit.Pending != nil },
		YoliumMode: c.yoliumMode,
	})

	// Emit cumulative usage
	cum := recordingProvider.Cumulative()
	if cum.TotalTokens > 0 || cum.Cost > 0 {
		detail := fmt.Sprintf("cumulative input:%d output:%d total:%d cost:$%.6f",
			cum.PromptTokens, cum.CompletionTokens, cum.TotalTokens, cum.Cost)
		yolium.Emit(stdout, yolium.ProgressEvent{
			Step:   "usage",
			Detail: detail,
		})
	}

	// Handle result
	if exit.Pending != nil {
		switch exit.Pending.Kind {
		case yolium.ExitPendingComplete:
			writeSummaryFile(c.repoPath, exit.Pending.Summary)
			if runErr != nil {
				fmt.Fprintln(stderr, runErr)
				return 1
			}
			return 0
		case yolium.ExitPendingError:
			writeSummaryFile(c.repoPath, "Error: "+exit.Pending.Message)
			fmt.Fprintln(stderr, "Agent error: "+exit.Pending.Message)
			return 1
		case yolium.ExitPendingQuestion:
			// Question already emitted on stdout via scanning. Yolium
			// will surface it and resume with the answer as a fresh
			// prompt; do NOT write a summary or emit a fallback
			// complete here.
			if runErr != nil {
				fmt.Fprintln(stderr, runErr)
				return 1
			}
			return 0
		}
	}

	// Fallback: model finished without emitting complete / error /
	// question. Distinguish "model wrote a summary file but forgot the
	// terminator" (treat as success) from "model just stopped" (honest
	// error — yolium should not mark this run as successful, otherwise
	// downstream agents see a non-existent plan).
	if runErr != nil {
		fmt.Fprintln(stderr, runErr)
	}

	if n := len(conv); n > 0 {
		last := conv[n-1]
		if last.Role == ai.RoleAssistant && (last.Content == nil || *last.Content == "") {
			fmt.Fprintln(stderr, "(no final assistant message)")
		}
	}

	// Only read summary.md as a graceful "model forgot the terminator"
	// fallback when the loop itself succeeded. When runErr != nil the
	// run is unambiguously a failure (e.g. maxIterations exhausted)
	// and the host must see yolium_error — even
	// if the model wrote a partial summary, treating it as a successful
	// `yolium_complete` would silently mark a failed run as done.
	if runErr == nil {
		if summary, ok := readExistingSummary(c.repoPath); ok {
			yolium.Emit(stdout, yolium.CompleteEvent{Summary: summary})
			return 0
		}
	}

	msg := "Agent stopped without emitting complete/error/ask_question"
	if runErr != nil {
		msg = msg + ": " + runErr.Error()
	}
	writeSummaryFile(c.repoPath, "Error: "+msg)
	yolium.Emit(stdout, yolium.ErrorEvent{Message: msg})
	// Yolium-mode runs also surface the error on the structured events
	// channel (fd-3) so the host sees a clean failure event during the
	// run rather than having to wait for the container to exit and the
	// summary file to be read. Without this, transient provider errors
	// such as OpenRouter HTTP/2 stream resets (`stream error: stream ID
	// N; INTERNAL_ERROR`) left the work item visibly stuck in `running`
	// for the duration of the container teardown.
	if c.eventSink != nil {
		_ = c.eventSink.Emit(yolium.ErrorEvent{Message: msg})
	}
	fmt.Fprintln(stderr, "Agent error: "+msg)
	return 1
}

// removeStaleSummaryFile clears any pre-existing .yolium/summary.md so
// that the fallback path can trust an existing summary as having been
// written by THIS run. Best-effort: a missing parent dir is fine.
func removeStaleSummaryFile(repoPath string) {
	_ = os.Remove(filepath.Join(repoPath, ".yolium", "summary.md"))
}

// readExistingSummary returns the first non-empty line of
// .yolium/summary.md when the file exists with content. Returns
// ("", false) when the file is absent or empty — in which case the
// fallback path treats the run as an error, not a silent success.
func readExistingSummary(repoPath string) (string, bool) {
	f, err := os.Open(filepath.Join(repoPath, ".yolium", "summary.md"))
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			return line, true
		}
	}
	return "", false
}

// Caps for log previews. Keep stderr verbose enough to debug protocol
// failures (model emits prose instead of @@YOLIUM:{...}) but not so
// verbose that a 65KB tool output floods the trace.
const (
	maxAssistantLogChars  = 4000
	maxToolArgLogChars    = 400
	maxToolResultLogChars = 600
)

func logAssistantContent(w io.Writer, turn int, content string) {
	text, truncated := truncate(content, maxAssistantLogChars)
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(w, "yoli: assistant[%d]> %s\n", turn, line)
	}
	if truncated {
		fmt.Fprintf(w, "yoli: assistant[%d]> ...(truncated, %d chars total)\n", turn, len(content))
	}
}

func logToolCall(w io.Writer, turn int, call ai.ToolCall) {
	args, truncated := truncate(singleLine(call.Arguments), maxToolArgLogChars)
	suffix := ""
	if truncated {
		suffix = "..."
	}
	fmt.Fprintf(w, "yoli: tool_call[%d] %s id=%s args=%s%s\n", turn, call.Name, call.ID, args, suffix)
}

func logToolResult(w io.Writer, turn int, toolCallID, result string) {
	text, truncated := truncate(singleLine(result), maxToolResultLogChars)
	suffix := ""
	if truncated {
		suffix = fmt.Sprintf("...(%d bytes total)", len(result))
	}
	fmt.Fprintf(w, "yoli: tool_result[%d] id=%s %s%s\n", turn, toolCallID, text, suffix)
}

// truncate returns the first n characters of s and a flag indicating
// whether truncation occurred.
func truncate(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	return s[:n], true
}

// singleLine collapses internal whitespace runs to a single space so the
// log entry stays on one line.
func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// dispatchAssistantEvents scans an assistant message for @@YOLIUM: lines,
// re-emits each parsed event on stdout so yolium's stdout parser sees a
// well-formed line (it already saw the assistant's raw text, but this
// normalises whitespace / casing and lets us treat the assistant prose
// and the protocol stream as the same source of truth), and sets
// exit.Pending for the first terminal event (complete / error /
// question). Subsequent events in the same message after a terminal one
// are still re-emitted so progress lines aren't lost.
func dispatchAssistantEvents(content string, stdout io.Writer, exit *yolium.ExitSignal) {
	dispatchEvents(content, stdout, exit, true)
}

// dispatchBashResultEvents scans the stdout of a Bash tool call (the
// content of a tool_result message) for @@YOLIUM: lines and treats
// terminal events as run-stopping signals. Non-terminal events (progress,
// comment, etc.) are NOT re-emitted here: the model is expected to write
// those directly in its prose, and forwarding Bash-narrated progress
// twice would double-count usage and clutter yolium's stream. This path
// exists specifically so that a model which insists on using
// `echo '@@YOLIUM:{"type":"complete",...}'` can still cleanly terminate
// the loop instead of running until the iteration cap or fallback.
func dispatchBashResultEvents(content string, stdout io.Writer, exit *yolium.ExitSignal) {
	dispatchEvents(content, stdout, exit, false)
}

// dispatchEvents is the shared implementation for both assistant-prose
// and Bash-result scanning. When emitProgress is true, every parsed
// event is re-emitted on stdout (used for assistant prose). When false,
// only terminal events are re-emitted (used for Bash results).
func dispatchEvents(content string, stdout io.Writer, exit *yolium.ExitSignal, emitProgress bool) {
	for _, evt := range yolium.ScanText(content) {
		kind, summary, message, terminal := yolium.TerminalEvent(evt)
		if emitProgress || terminal {
			_ = yolium.Emit(stdout, evt)
		}
		if !terminal || exit.Pending != nil {
			continue
		}
		switch kind {
		case "complete":
			exit.Pending = &yolium.ExitPending{
				Kind:    yolium.ExitPendingComplete,
				Summary: summary,
			}
		case "error":
			exit.Pending = &yolium.ExitPending{
				Kind:    yolium.ExitPendingError,
				Message: message,
			}
		case "question":
			exit.Pending = &yolium.ExitPending{
				Kind: yolium.ExitPendingQuestion,
			}
		}
	}
}

// resolveAgentSession picks a session for the agent run based on the
// resolved config. NoSession yields an in-memory session that never
// touches disk; otherwise we honour --fork / --session / --continue and
// fall through to creating a fresh session for the current cwd.
func resolveAgentSession(c agentLoopConfig) (*agentsession.Session, error) {
	opts := agentsession.Options{RootDir: c.sessionRoot, Cwd: c.repoPath}
	switch {
	case c.noSession:
		return agentsession.InMemory(opts), nil
	case c.sessionFork != "":
		return agentsession.ForkFrom(opts, c.sessionFork)
	case c.sessionTarget != "":
		return agentsession.Resolve(opts, c.sessionTarget)
	case c.sessionContinue:
		return agentsession.ContinueRecent(opts)
	default:
		return agentsession.Create(opts)
	}
}

func writeSummaryFile(repoPath, summary string) {
	dir := filepath.Join(repoPath, ".yolium")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.Create(filepath.Join(dir, "summary.md"))
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(summary + "\n")
}
