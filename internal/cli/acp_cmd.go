package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"yoli/internal/agent"
	agentsession "yoli/internal/agent/session"
	"yoli/internal/agent/skills"
	"yoli/internal/agent/tools"
	"yoli/internal/ai"
)

const acpUsage = "Usage: yoli acp [--provider <name>] [--no-session]\n" +
	"Serve the Agent Client Protocol (JSON-RPC over stdio) so editors can run yoli as an agent.\n"

// acpProtocolVersion is the only ACP major version yoli speaks.
const acpProtocolVersion = 1

// acpMaxToolOutput caps the tool result text echoed to the client in a
// tool_call_update. The model still sees the full result.
const acpMaxToolOutput = 4000

// JSON-RPC 2.0 error codes.
const (
	rpcParseError     = -32700
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
	rpcBusy           = -32000
)

// rpcRequest is every inbound line. An absent ID marks a notification;
// the ID is echoed back raw so numeric and string ids both work.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcResponse carries exactly one of Result or Error. A nil ID encodes
// as null, which is what a parse error must answer with.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type acpInitializeResult struct {
	ProtocolVersion   int                  `json:"protocolVersion"`
	AgentCapabilities acpAgentCapabilities `json:"agentCapabilities"`
	AgentInfo         acpImplementation    `json:"agentInfo"`
	AuthMethods       []any                `json:"authMethods"`
}

type acpAgentCapabilities struct {
	LoadSession        bool                  `json:"loadSession"`
	PromptCapabilities acpPromptCapabilities `json:"promptCapabilities"`
}

type acpPromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type acpImplementation struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type acpNewSessionParams struct {
	Cwd        string            `json:"cwd"`
	MCPServers []json.RawMessage `json:"mcpServers"`
}

type acpNewSessionResult struct {
	SessionID string `json:"sessionId"`
}

type acpLoadSessionParams struct {
	SessionID  string            `json:"sessionId"`
	Cwd        string            `json:"cwd"`
	MCPServers []json.RawMessage `json:"mcpServers"`
}

type acpPromptParams struct {
	SessionID string            `json:"sessionId"`
	Prompt    []acpContentBlock `json:"prompt"`
}

type acpPromptResult struct {
	StopReason string `json:"stopReason"`
}

type acpCancelParams struct {
	SessionID string `json:"sessionId"`
}

// acpContentBlock is an inbound prompt block. Only the fields yoli reads
// are decoded.
type acpContentBlock struct {
	Type     string               `json:"type"`
	Text     string               `json:"text,omitempty"`
	URI      string               `json:"uri,omitempty"`
	Name     string               `json:"name,omitempty"`
	Resource *acpEmbeddedResource `json:"resource,omitempty"`
}

// acpEmbeddedResource is either text contents (Text set, possibly empty)
// or binary contents (Blob set).
type acpEmbeddedResource struct {
	URI  string  `json:"uri"`
	Text *string `json:"text,omitempty"`
	Blob string  `json:"blob,omitempty"`
}

// acpText is an outbound text content block.
type acpText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func acpTextBlock(s string) acpText { return acpText{Type: "text", Text: s} }

// acpToolCallContent wraps a content block in a tool call's content list.
type acpToolCallContent struct {
	Type    string  `json:"type"`
	Content acpText `json:"content"`
}

type acpLocation struct {
	Path string `json:"path"`
}

// acpUpdate covers every session/update variant yoli emits; unused
// fields are omitted.
type acpUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Content       any             `json:"content,omitempty"`
	ToolCallID    string          `json:"toolCallId,omitempty"`
	Title         string          `json:"title,omitempty"`
	Kind          string          `json:"kind,omitempty"`
	Status        string          `json:"status,omitempty"`
	Locations     []acpLocation   `json:"locations,omitempty"`
	RawInput      json.RawMessage `json:"rawInput,omitempty"`
}

type acpSessionNotification struct {
	SessionID string    `json:"sessionId"`
	Update    acpUpdate `json:"update"`
}

// acpToolKind maps a yoli tool name to an ACP tool kind.
func acpToolKind(name string) string {
	switch name {
	case "Read":
		return "read"
	case "Write", "Edit":
		return "edit"
	case "LS", "Glob", "Grep":
		return "search"
	case "Bash":
		return "execute"
	case "WebSearch":
		return "fetch"
	}
	return "other"
}

// acpTitleArgs names the argument that best describes a call to each
// tool; tools not listed get a title of just their name.
var acpTitleArgs = map[string]string{
	"Read":      "path",
	"Write":     "path",
	"Edit":      "path",
	"LS":        "path",
	"Glob":      "pattern",
	"Grep":      "pattern",
	"Bash":      "command",
	"WebSearch": "query",
	"Skill":     "name",
	"Agent":     "role",
}

// acpToolTitle returns a one-line title such as "Skill plan" or
// "Read src/a.go", never raw JSON: CodeCompanion cuts titles at the
// first ':'. A Bash command is wrapped in backticks, which editors show
// as the command itself.
func acpToolTitle(call ai.ToolCall) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(call.Arguments), &args)
	arg, _ := args[acpTitleArgs[call.Name]].(string)
	arg = summarizeArgs(arg, 80)
	switch {
	case arg == "":
		return call.Name
	case call.Name == "Bash":
		return "`" + arg + "`"
	}
	return call.Name + " " + arg
}

// acpLocations returns the file a Read/Write/Edit call touches, made
// absolute against cwd, so the editor can follow along. Other tools and
// undecodable arguments yield nil.
func acpLocations(call ai.ToolCall, cwd string) []acpLocation {
	switch call.Name {
	case "Read", "Write", "Edit":
	default:
		return nil
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil || args.Path == "" {
		return nil
	}
	p := args.Path
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return []acpLocation{{Path: p}}
}

// acpURIPath returns the filesystem path of a file:// URI, or the URI
// unchanged for any other scheme.
func acpURIPath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return uri
	}
	u, err := url.Parse(uri)
	if err != nil || u.Path == "" {
		return uri
	}
	return u.Path
}

// acpPromptText flattens an ACP prompt into the single user message the
// agent loop takes. Block types yoli doesn't advertise are an error.
func acpPromptText(blocks []acpContentBlock) (string, error) {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "resource_link":
			parts = append(parts, "Referenced file: "+acpURIPath(b.URI))
		case "resource":
			if b.Resource == nil {
				return "", errors.New("resource block has no resource")
			}
			p := acpURIPath(b.Resource.URI)
			if b.Resource.Text != nil {
				parts = append(parts, "Contents of "+p+":\n```\n"+strings.TrimSuffix(*b.Resource.Text, "\n")+"\n```")
			} else {
				parts = append(parts, "Referenced file: "+p)
			}
		default:
			return "", fmt.Errorf("unsupported prompt content type %q", b.Type)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// acpUpdatesForMessage maps one yoli message to the session/update
// payloads that render it. It is used both live and for session/load
// replay.
func acpUpdatesForMessage(m ai.Message, cwd string) []acpUpdate {
	var out []acpUpdate
	switch m.Role {
	case ai.RoleUser:
		if m.Content != nil && *m.Content != "" {
			out = append(out, acpUpdate{SessionUpdate: "user_message_chunk", Content: acpTextBlock(*m.Content)})
		}
	case ai.RoleAssistant:
		if m.Reasoning != nil && *m.Reasoning != "" {
			out = append(out, acpUpdate{SessionUpdate: "agent_thought_chunk", Content: acpTextBlock(*m.Reasoning)})
		}
		if m.Content != nil && *m.Content != "" {
			out = append(out, acpUpdate{SessionUpdate: "agent_message_chunk", Content: acpTextBlock(*m.Content)})
		}
		for _, tc := range m.ToolCalls {
			u := acpUpdate{
				SessionUpdate: "tool_call",
				ToolCallID:    tc.ID,
				Title:         acpToolTitle(tc),
				Kind:          acpToolKind(tc.Name),
				Status:        "in_progress",
				Locations:     acpLocations(tc, cwd),
			}
			if json.Valid([]byte(tc.Arguments)) {
				u.RawInput = json.RawMessage(tc.Arguments)
			}
			out = append(out, u)
		}
	case ai.RoleTool:
		body := ""
		if m.Content != nil {
			body = *m.Content
		}
		status := "completed"
		if isToolError(body) {
			status = "failed"
		}
		if len(body) > acpMaxToolOutput {
			n := acpMaxToolOutput
			for n > 0 && !utf8.RuneStart(body[n]) {
				n--
			}
			body = body[:n] + "…(truncated)"
		}
		out = append(out, acpUpdate{
			SessionUpdate: "tool_call_update",
			ToolCallID:    m.ToolCallID,
			Status:        status,
			Content:       []acpToolCallContent{{Type: "content", Content: acpTextBlock(body)}},
		})
	}
	return out
}

// acpConfig carries everything serveACP needs, with the provider and
// skill loader injected so tests can drive the server with a
// FauxProvider (mirrors tuiLoopConfig).
type acpConfig struct {
	provider      ai.Provider
	model         string
	profileName   string
	braveAPIKey   string
	exe           string
	contextWindow int
	maxTokens     int
	// maxIterations is passed to RunOptions.MaxIterations; zero means
	// the loop default.
	maxIterations int
	sessionRoot   string
	noSession     bool
	loadSkills    func(cwd string) []skills.LoadedSkill
}

// acpSession is one ACP session: a yoli session plus a tool set and
// system prompt bound to the client's cwd. cancel is non-nil only while
// a prompt is running.
type acpSession struct {
	id     string
	cwd    string
	sess   *agentsession.Session
	tools  []tools.Tool
	system string
	cancel context.CancelFunc
}

type acpServer struct {
	cfg acpConfig
	log io.Writer

	// outMu serializes every write to out (and log), so frames from
	// concurrent turns never interleave.
	outMu sync.Mutex
	out   io.Writer

	mu       sync.Mutex
	sessions map[string]*acpSession

	wg sync.WaitGroup
}

// serveACP runs the JSON-RPC read loop until stdin closes. Prompts run
// on their own goroutines so session/cancel can be read while a turn is
// in flight. Provider-agnostic and fully testable.
func serveACP(cfg acpConfig, in io.Reader, out, log io.Writer) int {
	s := &acpServer{cfg: cfg, out: out, log: log, sessions: map[string]*acpSession{}}
	// ReadBytes has no line-length cap; embedded resources can be large.
	br := bufio.NewReader(in)
	code := 0
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			s.handle(line)
		}
		if err != nil {
			if err != io.EOF {
				s.logf("yoli: acp: read: %v", err)
				code = 1
			}
			break
		}
	}
	s.mu.Lock()
	for _, as := range s.sessions {
		if as.cancel != nil {
			as.cancel()
		}
	}
	s.mu.Unlock()
	s.wg.Wait()
	return code
}

func (s *acpServer) handle(line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.respondError(nil, rpcParseError, "parse error: "+err.Error())
		return
	}
	if len(req.ID) == 0 {
		// Notifications never get a response; unknown ones are ignored.
		if req.Method == "session/cancel" {
			s.cancelPrompt(req.Params)
		}
		return
	}
	var result any
	var rerr *rpcError
	switch req.Method {
	case "initialize":
		result = s.initialize()
	case "authenticate":
		result = struct{}{}
	case "session/new":
		result, rerr = s.newSession(req.Params)
	case "session/load":
		result, rerr = s.loadSession(req.Params)
	case "session/prompt":
		// On success the turn goroutine sends the response.
		if rerr = s.startPrompt(req.ID, req.Params); rerr == nil {
			return
		}
	default:
		rerr = &rpcError{Code: rpcMethodNotFound, Message: "method not found: " + req.Method}
	}
	if rerr != nil {
		s.write(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rerr})
		return
	}
	s.respond(req.ID, result)
}

func (s *acpServer) initialize() acpInitializeResult {
	return acpInitializeResult{
		ProtocolVersion: acpProtocolVersion,
		AgentCapabilities: acpAgentCapabilities{
			// In-memory sessions can't be resolved later.
			LoadSession:        !s.cfg.noSession,
			PromptCapabilities: acpPromptCapabilities{EmbeddedContext: true},
		},
		AgentInfo:   acpImplementation{Name: "yoli", Title: "yoli", Version: Version},
		AuthMethods: []any{},
	}
}

func (s *acpServer) newSession(params json.RawMessage) (any, *rpcError) {
	var p acpNewSessionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: rpcInvalidParams, Message: err.Error()}
	}
	if !filepath.IsAbs(p.Cwd) {
		return nil, &rpcError{Code: rpcInvalidParams, Message: fmt.Sprintf("cwd must be an absolute path (got %q)", p.Cwd)}
	}
	cwd := filepath.Clean(p.Cwd)
	s.logMCPServers(p.MCPServers)
	opts := agentsession.Options{RootDir: s.cfg.sessionRoot, Cwd: cwd}
	var sess *agentsession.Session
	if s.cfg.noSession {
		sess = agentsession.InMemory(opts)
	} else {
		var err error
		if sess, err = agentsession.Create(opts); err != nil {
			return nil, &rpcError{Code: rpcInternalError, Message: err.Error()}
		}
	}
	as := s.register(sess, cwd)
	return acpNewSessionResult{SessionID: as.id}, nil
}

func (s *acpServer) loadSession(params json.RawMessage) (any, *rpcError) {
	if s.cfg.noSession {
		return nil, &rpcError{Code: rpcMethodNotFound, Message: "session/load is unavailable under --no-session"}
	}
	var p acpLoadSessionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: rpcInvalidParams, Message: err.Error()}
	}
	if !filepath.IsAbs(p.Cwd) {
		return nil, &rpcError{Code: rpcInvalidParams, Message: fmt.Sprintf("cwd must be an absolute path (got %q)", p.Cwd)}
	}
	if p.SessionID == "" {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "sessionId is required"}
	}
	if s.busy(p.SessionID) {
		return nil, &rpcError{Code: rpcBusy, Message: "prompt already in progress"}
	}
	cwd := filepath.Clean(p.Cwd)
	s.logMCPServers(p.MCPServers)
	sess, err := agentsession.Resolve(agentsession.Options{RootDir: s.cfg.sessionRoot, Cwd: cwd}, p.SessionID)
	if err != nil {
		return nil, &rpcError{Code: rpcInvalidParams, Message: err.Error()}
	}
	as := s.register(sess, cwd)
	for _, m := range sess.BuildMessages() {
		s.sendUpdates(as, m)
	}
	return struct{}{}, nil
}

// register builds the session's tool set exactly as runTUI does, but
// rooted at the session's cwd, and makes the session addressable.
func (s *acpServer) register(sess *agentsession.Session, cwd string) *acpSession {
	toolset := append(
		tools.DefaultTools(cwd, s.cfg.braveAPIKey),
		tools.NewSubAgentTool(tools.SubAgentOptions{
			CLIEntry: s.cfg.exe,
			Provider: s.cfg.profileName,
			Model:    s.cfg.model,
		}),
	)
	skillList := s.cfg.loadSkills(cwd)
	if len(skillList) > 0 {
		toolset = append(toolset, tools.NewSkillTool(skillList))
	}
	as := &acpSession{
		id:     sess.GetSessionID(),
		cwd:    cwd,
		sess:   sess,
		tools:  toolset,
		system: chatSystemPrompt(skillList),
	}
	s.mu.Lock()
	s.sessions[as.id] = as
	s.mu.Unlock()
	return as
}

func (s *acpServer) busy(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	as := s.sessions[sessionID]
	return as != nil && as.cancel != nil
}

// startPrompt validates a session/prompt, records the user message, and
// starts the turn. The cancel func is stored here, in the read loop,
// before the goroutine starts, so a session/cancel on the next line
// always finds it.
func (s *acpServer) startPrompt(id, params json.RawMessage) *rpcError {
	var p acpPromptParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &rpcError{Code: rpcInvalidParams, Message: err.Error()}
	}
	s.mu.Lock()
	as := s.sessions[p.SessionID]
	s.mu.Unlock()
	if as == nil {
		return &rpcError{Code: rpcInvalidParams, Message: fmt.Sprintf("unknown session %q", p.SessionID)}
	}
	if s.busy(p.SessionID) {
		return &rpcError{Code: rpcBusy, Message: "prompt already in progress"}
	}
	text, err := acpPromptText(p.Prompt)
	if err != nil {
		return &rpcError{Code: rpcInvalidParams, Message: err.Error()}
	}
	if _, err := as.sess.AppendMessage(ai.Message{Role: ai.RoleUser, Content: &text}); err != nil {
		return &rpcError{Code: rpcInternalError, Message: err.Error()}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	as.cancel = cancel
	s.mu.Unlock()
	s.wg.Add(1)
	go s.runTurn(ctx, cancel, as, id)
	return nil
}

// runTurn runs one agent turn and answers the pending session/prompt.
// It mirrors the TUI's turn handling.
func (s *acpServer) runTurn(ctx context.Context, cancel context.CancelFunc, as *acpSession, id json.RawMessage) {
	defer s.wg.Done()
	defer cancel()
	seed := []ai.Message{{Role: ai.RoleSystem, Content: &as.system}}
	seed = append(seed, as.sess.BuildMessages()...)
	_, err := agent.Run(ctx, agent.RunOptions{
		Provider:            s.cfg.provider,
		Model:               s.cfg.model,
		Tools:               as.tools,
		Messages:            seed,
		MaxIterations:       s.cfg.maxIterations,
		MaxTokens:           s.cfg.maxTokens,
		ContextBudgetTokens: s.cfg.contextWindow,
		OnMessage: func(m ai.Message) {
			s.sendUpdates(as, m)
			// Reasoning is display-only: strip it before persisting so a
			// resumed session never replays it to a provider.
			if m.Role == ai.RoleAssistant || m.Role == ai.RoleTool {
				m.Reasoning = nil
				_, _ = as.sess.AppendMessage(m)
			}
		},
	})
	cancelled := ctx.Err() != nil
	s.mu.Lock()
	as.cancel = nil
	s.mu.Unlock()
	var mi *agent.MaxIterationsError
	switch {
	// A cancelled turn reports cancelled whatever error Run returned.
	case cancelled:
		s.respond(id, acpPromptResult{StopReason: "cancelled"})
	case errors.As(err, &mi):
		s.respond(id, acpPromptResult{StopReason: "max_turn_requests"})
	case err != nil:
		s.respondError(id, rpcInternalError, err.Error())
	default:
		s.respond(id, acpPromptResult{StopReason: "end_turn"})
	}
}

func (s *acpServer) cancelPrompt(params json.RawMessage) {
	var p acpCancelParams
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if as := s.sessions[p.SessionID]; as != nil && as.cancel != nil {
		as.cancel()
	}
}

func (s *acpServer) logMCPServers(servers []json.RawMessage) {
	if len(servers) > 0 {
		s.logf("yoli: acp: ignoring %d MCP server(s)", len(servers))
	}
}

func (s *acpServer) sendUpdates(as *acpSession, m ai.Message) {
	for _, u := range acpUpdatesForMessage(m, as.cwd) {
		s.write(rpcNotification{
			JSONRPC: "2.0",
			Method:  "session/update",
			Params:  acpSessionNotification{SessionID: as.id, Update: u},
		})
	}
}

func (s *acpServer) respond(id json.RawMessage, result any) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *acpServer) respondError(id json.RawMessage, code int, msg string) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

// write sends one frame: compact JSON (encoding/json escapes newlines in
// strings) followed by '\n'.
func (s *acpServer) write(v any) {
	b, err := json.Marshal(v)
	s.outMu.Lock()
	defer s.outMu.Unlock()
	if err != nil {
		fmt.Fprintf(s.log, "yoli: acp: encode: %v\n", err)
		return
	}
	_, _ = s.out.Write(append(b, '\n'))
}

func (s *acpServer) logf(format string, args ...any) {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	fmt.Fprintf(s.log, format+"\n", args...)
}

// runACP implements `yoli acp`: it resolves flags, config, and the
// provider like runTUI, then hands off to serveACP. Nothing but ACP
// frames is ever written to stdout.
func runACP(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	providerName := ""
	noSession := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--provider":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "--provider requires a value")
				fmt.Fprint(stderr, acpUsage)
				return 1
			}
			providerName = args[i+1]
			i++
		case strings.HasPrefix(arg, "--provider="):
			providerName = strings.TrimPrefix(arg, "--provider=")
		case arg == "--no-session":
			noSession = true
		default:
			fmt.Fprintf(stderr, "unknown acp argument %q\n", arg)
			fmt.Fprint(stderr, acpUsage)
			return 1
		}
	}
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptionsFromEnv(),
		Warnings:    stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	profiles, err := LoadProviderProfiles(LoadOptions{
		PathOptions: PathOptionsFromEnv(),
		Warnings:    stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	prof, profileName, err := selectProviderProfile(cfg, profiles, providerName)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	provider, err := newProviderFromProfile(prof, "Yoli")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	exe, _ := os.Executable()
	contextWindow, maxTokens := contextLimits(prof)
	fmt.Fprintf(stderr, "yoli: acp: provider=%s model=%s\n", profileName, prof.Model)
	return serveACP(acpConfig{
		provider:      provider,
		model:         prof.Model,
		profileName:   profileName,
		braveAPIKey:   cfg["BRAVE_API_KEY"],
		exe:           exe,
		contextWindow: contextWindow,
		maxTokens:     maxTokens,
		noSession:     noSession,
		loadSkills: func(cwd string) []skills.LoadedSkill {
			return loadSkillsForPromptIn(cwd, stderr)
		},
	}, stdin, stdout, stderr)
}
