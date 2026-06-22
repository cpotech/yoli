package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"yoli/internal/agent"
	agentsession "yoli/internal/agent/session"
	"yoli/internal/agent/tools"
	"yoli/internal/ai"
	"yoli/internal/ai/providers"
)

const tuiUsage = "Usage: yoli tui [--loglevel debug|info|error|none] [session options]\n" +
	"Interactive REPL with prompt history (↑/↓) and cursor movement (←/→). Type /help inside.\n"

const (
	ansiDim   = "\x1b[2m"
	ansiRed   = "\x1b[31m"
	ansiReset = "\x1b[0m"
)

const tuiHelp = `commands:
  /help            show this list
  /model [slug]    show or switch the model
  /context         show estimated context size
  /clear           start a new session
  /exit, /quit     leave the REPL (or Ctrl-D)`

// tuiLoopConfig carries everything runTUILoop needs, with the provider
// and session injected so tests can drive the loop with a FauxProvider
// and scripted stdin (mirrors the runAgent/runAgentLoop split).
type tuiLoopConfig struct {
	provider ai.Provider
	model    string
	tools    []tools.Tool
	sess     *agentsession.Session
	// newSession backs /clear; it honours the original session flags
	// (e.g. --no-session keeps sessions in-memory).
	newSession func() (*agentsession.Session, error)
	// color gates ANSI escapes on stdout (stdout is a terminal and
	// NO_COLOR is unset).
	color bool
	// interactive gates the banner, "> " prompt, and spinner on stderr.
	interactive bool
	// handleSignals enables per-turn SIGINT handling so Ctrl-C cancels
	// the in-flight turn instead of killing the REPL. Off in tests.
	handleSignals bool
}

// tuiIsTerminal reports whether f is a character device (a terminal).
func tuiIsTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// tuiColorEnabled reports whether ANSI color should be emitted on f:
// f is a terminal and NO_COLOR is unset.
func tuiColorEnabled(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return tuiIsTerminal(f)
}

// tuiPaint wraps s in the given ANSI code when color is on.
func tuiPaint(s, code string, color bool) string {
	if !color {
		return s
	}
	return code + s + ansiReset
}

// tuiSpinner renders a braille "thinking" indicator on w until stopped.
// Stop is idempotent, safe on a nil receiver, and blocks until the
// spinner line has been cleared, so callers can write output immediately
// after without interleaving.
type tuiSpinner struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func startTUISpinner(w io.Writer) *tuiSpinner {
	s := &tuiSpinner{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
		const label = " thinking…"
		clear := "\r" + strings.Repeat(" ", 1+len([]rune(label))) + "\r"
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		i := 0
		fmt.Fprintf(w, "\r%c%s", frames[i], label)
		for {
			select {
			case <-s.stop:
				fmt.Fprint(w, clear)
				return
			case <-tick.C:
				i = (i + 1) % len(frames)
				fmt.Fprintf(w, "\r%c%s", frames[i], label)
			}
		}
	}()
	return s
}

func (s *tuiSpinner) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.stop) })
	<-s.done
}

// tuiLineEditor provides a line editing interface with:
//   - Up/Down arrows to navigate prompt history
//   - Left/Right arrows to move cursor within the current prompt
//   - Backspace/Delete to edit
//   - Enter to submit
//
// It operates in terminal raw mode and restores the terminal on Close.
type tuiLineEditor struct {
	stdin   int           // file descriptor for stdin
	stdout  *bufio.Writer // buffered stdout writer
	history []string      // prompt history (oldest first)
	histIdx int           // current position in history (-1 = new prompt)
	prompt  string        // current input buffer (may contain '\n' from paste)
	cursor  int           // cursor position within prompt (0 = beginning)
	curRow  int           // visual row of the cursor within the rendered buffer
}

// newTUILineEditor creates a line editor if stdin is a terminal.
// Returns nil if not a terminal (falls back to basic reading).
// Raw mode is entered per-readLine rather than here, so output between
// prompts is emitted in cooked mode (see readLine).
func newTUILineEditor(stdin *os.File, stdout io.Writer) *tuiLineEditor {
	if !tuiIsTerminal(stdin) {
		return nil
	}
	return &tuiLineEditor{
		stdin:   int(stdin.Fd()),
		stdout:  bufio.NewWriter(stdout),
		history: make([]string, 0),
		histIdx: -1,
		prompt:  "",
		cursor:  0,
	}
}

// addToHistory adds a prompt to the history.
func (e *tuiLineEditor) addToHistory(s string) {
	if s == "" {
		return
	}
	// Don't add duplicates of the most recent entry
	if len(e.history) > 0 && e.history[len(e.history)-1] == s {
		return
	}
	e.history = append(e.history, s)
	// Keep history bounded (optional: make configurable)
	if len(e.history) > 1000 {
		e.history = e.history[1:]
	}
}

// readLine reads one line with editing support.
// Returns the line (without trailing newline), eof flag, and any error.
func (e *tuiLineEditor) readLine() (string, bool, error) {
	// Enter raw mode only while editing this line. Restoring cooked mode
	// between prompts lets the terminal translate "\n" into "\r\n", so
	// agent replies and command output don't drift rightward down the
	// screen. Staying in raw mode across turns drops the carriage return.
	prev, err := term.MakeRaw(e.stdin)
	if err != nil {
		return "", false, err
	}
	defer term.Restore(e.stdin, prev)

	// Enable bracketed paste so the terminal wraps pasted text in
	// ESC[200~ … ESC[201~. Without this, embedded newlines arrive as bare
	// CR/LF and the first one submits the line, truncating the paste.
	e.stdout.WriteString("\x1b[?2004h")
	e.stdout.Flush()
	defer func() {
		e.stdout.WriteString("\x1b[?2004l")
		e.stdout.Flush()
	}()

	// Reset state for new line
	e.prompt = ""
	e.cursor = 0
	e.histIdx = -1
	e.curRow = 0

	for {
		ch := make([]byte, 1)
		n, err := os.Stdin.Read(ch)
		if err != nil || n == 0 {
			return e.prompt, false, err
		}

		switch ch[0] {
		case 3: // Ctrl-C
			e.prompt = ""
			e.cursor = 0
			e.redrawLine()
			// Don't quit, just clear the line

		case 4: // Ctrl-D (EOF)
			if len(e.prompt) == 0 {
				return "", true, io.EOF
			}

		case 13, 10: // Enter (CR or LF)
			result := e.prompt
			e.addToHistory(result)
			// Move the cursor past the last rendered row so the newline
			// lands below all content rather than mid-buffer.
			e.cursor = len(e.prompt)
			e.redrawLine()
			e.stdout.WriteString("\r\n")
			e.stdout.Flush()
			return result, false, nil

		case 127, 8: // Backspace or Ctrl-H
			if e.cursor > 0 {
				e.prompt = e.prompt[:e.cursor-1] + e.prompt[e.cursor:]
				e.cursor--
				e.redrawLine()
			}

		case 27: // Escape sequence (arrows, bracketed paste, etc.)
			// CSI sequences start with ESC '['. Arrow keys are a single
			// final byte (A/B/C/D); paste markers are ESC[200~ / ESC[201~.
			b0, err := e.readByte()
			if err != nil || b0 != '[' {
				continue
			}
			b1, err := e.readByte()
			if err != nil {
				continue
			}
			switch b1 {
			case 'A': // Up arrow
				e.historyUp()
			case 'B': // Down arrow
				e.historyDown()
			case 'C': // Right arrow
				if e.cursor < len(e.prompt) {
					e.cursor++
					e.redrawLine()
				}
			case 'D': // Left arrow
				if e.cursor > 0 {
					e.cursor--
					e.redrawLine()
				}
			default:
				if b1 < '0' || b1 > '9' {
					continue
				}
				// Numeric CSI sequence: read digits up to the final byte.
				params := []byte{b1}
				for {
					nb, err := e.readByte()
					if err != nil {
						break
					}
					params = append(params, nb)
					if nb < '0' || nb > '9' {
						break
					}
				}
				if string(params) == "200~" {
					e.insertPaste()
				}
				// "201~" (stray paste-end) and other CSI sequences are
				// ignored.
			}
		default:
			// Regular character - insert at cursor
			e.prompt = e.prompt[:e.cursor] + string(ch[0]) + e.prompt[e.cursor:]
			e.cursor++
			e.redrawLine()
		}
	}
}

// historyUp moves to the previous history entry.
func (e *tuiLineEditor) historyUp() {
	if len(e.history) == 0 {
		return
	}
	if e.histIdx == -1 {
		// Save current input before navigating away
		e.histIdx = len(e.history) - 1
	} else if e.histIdx > 0 {
		e.histIdx--
	}
	if e.histIdx >= 0 && e.histIdx < len(e.history) {
		e.prompt = e.history[e.histIdx]
		e.cursor = len(e.prompt)
		e.redrawLine()
	}
}

// historyDown moves to the next history entry.
func (e *tuiLineEditor) historyDown() {
	if e.histIdx == -1 {
		return
	}
	if e.histIdx < len(e.history)-1 {
		e.histIdx++
		e.prompt = e.history[e.histIdx]
	} else {
		// Back to empty new prompt
		e.histIdx = -1
		e.prompt = ""
	}
	e.cursor = len(e.prompt)
	e.redrawLine()
}

// readByte reads a single byte from stdin in raw mode.
func (e *tuiLineEditor) readByte() (byte, error) {
	var b [1]byte
	n, err := os.Stdin.Read(b[:])
	if n == 0 && err == nil {
		err = io.EOF
	}
	return b[0], err
}

// readN reads up to n bytes, returning early on error.
func (e *tuiLineEditor) readN(n int) []byte {
	out := make([]byte, 0, n)
	for len(out) < n {
		b, err := e.readByte()
		if err != nil {
			break
		}
		out = append(out, b)
	}
	return out
}

// normalizePaste folds CR and CRLF to LF so pasted line breaks live in the
// buffer as '\n' rather than carriage returns.
func normalizePaste(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// insertPaste consumes a bracketed-paste payload (already past the ESC[200~
// marker) up to the ESC[201~ terminator and inserts it at the cursor,
// preserving embedded newlines instead of submitting on the first one.
func (e *tuiLineEditor) insertPaste() {
	var buf []byte
	for {
		b, err := e.readByte()
		if err != nil {
			break
		}
		if b == 27 { // possible end marker ESC[201~
			seq := e.readN(5)
			if string(seq) == "[201~" {
				break
			}
			// Not the terminator: keep the ESC and what followed as
			// literal content.
			buf = append(buf, 27)
			buf = append(buf, seq...)
			continue
		}
		buf = append(buf, b)
	}
	s := normalizePaste(string(buf))
	e.prompt = e.prompt[:e.cursor] + s + e.prompt[e.cursor:]
	e.cursor += len(s)
	e.redrawLine()
}

// redrawLine redraws the current buffer, which may span multiple rows when
// it contains newlines from a paste. It assumes no single logical line
// exceeds the terminal width (line wrapping is not accounted for).
func (e *tuiLineEditor) redrawLine() {
	// Return to the first rendered row, then clear it and everything below.
	if e.curRow > 0 {
		fmt.Fprintf(e.stdout, "\x1b[%dA", e.curRow)
	}
	e.stdout.WriteString("\r\x1b[J")

	// Print the buffer, prefixing the first row with "> " and breaking
	// rows with CRLF (raw mode needs the explicit carriage return).
	lines := strings.Split(e.prompt, "\n")
	for i, ln := range lines {
		if i == 0 {
			e.stdout.WriteString("> " + ln)
		} else {
			e.stdout.WriteString("\r\n" + ln)
		}
	}

	// Locate the cursor's target row/column from its byte offset.
	before := e.prompt[:e.cursor]
	targetRow := strings.Count(before, "\n")
	col := len(before) - (strings.LastIndexByte(before, '\n') + 1)
	if targetRow == 0 {
		col += 2 // account for the "> " prefix
	}

	// The cursor currently sits at the end of the last row; move it to the
	// target position.
	if up := (len(lines) - 1) - targetRow; up > 0 {
		fmt.Fprintf(e.stdout, "\x1b[%dA", up)
	}
	e.stdout.WriteString("\r")
	if col > 0 {
		fmt.Fprintf(e.stdout, "\x1b[%dC", col)
	}
	e.curRow = targetRow
	e.stdout.Flush()
}

// runTUI implements `yoli tui`: it resolves flags, config, provider,
// toolset, and session exactly like runChat, then hands off to the
// provider-agnostic runTUILoop.
func runTUI(args []string, in io.Reader, stdout, stderr io.Writer) int {
	flags, rest, err := parseChatFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		fmt.Fprint(stderr, tuiUsage)
		return 1
	}
	if len(rest) > 0 {
		fmt.Fprintf(stderr, "tui takes no prompt argument (got %q)\n", strings.Join(rest, " "))
		fmt.Fprint(stderr, tuiUsage)
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
		// Note: the sub-agent tool captures the startup model; /model
		// switches the REPL's chat model only.
		tools.NewSubAgentTool(tools.SubAgentOptions{
			CLIEntry:     exe,
			DefaultModel: model,
		}),
	)
	sess, err := resolveChatSession(flags, cwd, in)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return runTUILoop(tuiLoopConfig{
		provider: provider,
		model:    model,
		tools:    toolset,
		sess:     sess,
		newSession: func() (*agentsession.Session, error) {
			opts := agentsession.Options{RootDir: flags.SessionRoot, Cwd: cwd}
			if flags.NoSession {
				return agentsession.InMemory(opts), nil
			}
			return agentsession.Create(opts)
		},
		color:         tuiColorEnabled(os.Stdout),
		interactive:   tuiIsTerminal(os.Stderr),
		handleSignals: true,
	}, in, stdout, stderr)
}

// runTUILoop is the REPL proper: read a line, handle slash commands, or
// run one agent turn. Provider-agnostic and fully testable.
func runTUILoop(c tuiLoopConfig, in io.Reader, stdout, stderr io.Writer) int {
	system := chatSystem
	if c.interactive {
		fmt.Fprintf(stderr, "yoli tui — model=%s session=%s (/help for commands)\n", c.model, c.sess.GetSessionID())
	}

	// Create line editor for interactive mode (terminal)
	var editor *tuiLineEditor
	if c.interactive && tuiIsTerminal(os.Stdin) {
		editor = newTUILineEditor(os.Stdin, stderr)
	}

	// Create bufio reader for non-interactive mode (scripted input)
	var br *bufio.Reader
	if editor == nil {
		br = bufio.NewReader(in)
	}

	// Render state shared across turns: tool-call IDs map to names so
	// tool results can be labelled, and the active spinner (if any) is
	// stopped before the first output of each turn.
	nameByID := map[string]string{}
	var sp *tuiSpinner
	render := func(m ai.Message) {
		sp.Stop()
		if m.Role == ai.RoleAssistant || m.Role == ai.RoleTool {
			_, _ = c.sess.AppendMessage(m)
		}
		switch m.Role {
		case ai.RoleAssistant:
			if m.Content != nil && *m.Content != "" {
				fmt.Fprintln(stdout, *m.Content)
			}
			for _, tc := range m.ToolCalls {
				nameByID[tc.ID] = tc.Name
				fmt.Fprintln(stdout, tuiPaint(fmt.Sprintf("→ %s %s", tc.Name, summarizeArgs(tc.Arguments, 120)), ansiDim, c.color))
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
			line := fmt.Sprintf("← %s (%d bytes)", name, len(body))
			if isToolError(body) {
				fmt.Fprintln(stdout, tuiPaint(line, ansiRed, c.color))
			} else {
				fmt.Fprintln(stdout, tuiPaint(line, ansiDim, c.color))
			}
		}
	}

	for {
		if c.interactive {
			fmt.Fprint(stderr, "> ")
		}

		var line string
		var atEOF bool

		if editor != nil {
			// Use the line editor with history and cursor support
			result, eof, err := editor.readLine()
			if err != nil && err != io.EOF {
				fmt.Fprintln(stderr, "read error:", err)
				continue
			}
			line = strings.TrimSpace(result)
			atEOF = eof
		} else {
			// Fallback to bufio for non-interactive mode
			raw, err := br.ReadString('\n')
			atEOF = err != nil
			line = strings.TrimSpace(raw)
		}

		if line == "" {
			if atEOF {
				if c.interactive {
					fmt.Fprintln(stderr)
				}
				return 0
			}
			continue
		}

		if strings.HasPrefix(line, "/") {
			if quit := tuiSlashCommand(&c, line, system, stdout, stderr); quit {
				return 0
			}
			if atEOF {
				return 0
			}
			continue
		}

		user := line
		if _, err := c.sess.AppendMessage(ai.Message{Role: ai.RoleUser, Content: &user}); err != nil {
			fmt.Fprintln(stderr, err)
			continue
		}
		seed := []ai.Message{{Role: ai.RoleSystem, Content: &system}}
		seed = append(seed, c.sess.BuildMessages()...)

		ctx, cancel := context.WithCancel(context.Background())
		var sigCh chan os.Signal
		if c.handleSignals {
			sigCh = make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt)
			go func() {
				select {
				case <-sigCh:
					cancel()
				case <-ctx.Done():
				}
			}()
		}
		if c.interactive {
			sp = startTUISpinner(stderr)
		}
		_, runErr := agent.Run(ctx, agent.RunOptions{
			Provider:  c.provider,
			Model:     c.model,
			Tools:     c.tools,
			Messages:  seed,
			OnMessage: render,
		})
		sp.Stop()
		sp = nil
		interrupted := ctx.Err() != nil
		if sigCh != nil {
			signal.Stop(sigCh)
		}
		cancel()
		if runErr != nil {
			if interrupted {
				fmt.Fprintln(stderr, "(interrupted)")
			} else {
				fmt.Fprintln(stderr, runErr)
			}
			// A failed turn must not kill the REPL.
		}
		if atEOF {
			return 0
		}
	}
}

// tuiSlashCommand handles a "/..." line. It returns true when the REPL
// should exit. It may swap c.sess (/clear) or c.model (/model).
func tuiSlashCommand(c *tuiLoopConfig, line, system string, stdout, stderr io.Writer) bool {
	fields := strings.Fields(line)
	cmd, args := fields[0], fields[1:]
	switch cmd {
	case "/exit", "/quit":
		return true
	case "/help":
		fmt.Fprintln(stdout, tuiHelp)
	case "/clear":
		ns, err := c.newSession()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return false
		}
		c.sess = ns
		fmt.Fprintf(stdout, "started new session %s\n", ns.GetSessionID())
	case "/model":
		if len(args) == 0 {
			fmt.Fprintf(stdout, "model: %s\n", c.model)
			return false
		}
		c.model = args[0]
		fmt.Fprintf(stdout, "model set to %s\n", c.model)
	case "/context":
		seed := []ai.Message{{Role: ai.RoleSystem, Content: &system}}
		seed = append(seed, c.sess.BuildMessages()...)
		fmt.Fprintf(stdout, "context-size: %s\n", formatContextSize(agent.EstimateContextTokens(seed), agent.DefaultContextBudget))
	default:
		fmt.Fprintf(stdout, "unknown command %s — try /help\n", cmd)
	}
	return false
}
