package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"yoli/internal/agent/skills"
	"yoli/internal/ai"
	"yoli/internal/ai/providers"
)

// newACPTestConfig builds an acpConfig wired for in-process tests:
// in-memory sessions and no skills. Tests that need sessions on disk
// clear noSession and set sessionRoot to t.TempDir().
func newACPTestConfig(p ai.Provider) acpConfig {
	return acpConfig{
		provider:   p,
		model:      "test/model",
		noSession:  true,
		loadSkills: func(string) []skills.LoadedSkill { return nil },
	}
}

// acpHarness drives serveACP over pipes like a well-behaved client: it
// can wait for the response to each request before sending the next
// line, so a turn is never raced by stdin EOF.
type acpHarness struct {
	t      *testing.T
	in     *io.PipeWriter
	frames chan map[string]any
	exit   chan int
	nextID int
}

func startACP(t *testing.T, cfg acpConfig) *acpHarness {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := &acpHarness{t: t, in: inW, frames: make(chan map[string]any, 256), exit: make(chan int, 1)}
	go func() {
		code := serveACP(cfg, inR, outW, io.Discard)
		outW.Close()
		h.exit <- code
	}()
	go func() {
		br := bufio.NewReader(outR)
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
				var f map[string]any
				if !json.Valid(line) || json.Unmarshal(line, &f) != nil {
					t.Errorf("stdout line is not a JSON object: %q", line)
				} else {
					h.frames <- f
				}
			}
			if err != nil {
				close(h.frames)
				return
			}
		}
	}()
	// Shut the server down even when a test fails early, so no
	// goroutine outlives the test.
	t.Cleanup(func() {
		inW.Close()
		for range h.frames {
		}
	})
	return h
}

func (h *acpHarness) send(line string) {
	h.t.Helper()
	if _, err := io.WriteString(h.in, line+"\n"); err != nil {
		h.t.Fatalf("write stdin: %v", err)
	}
}

func (h *acpHarness) sendJSON(v any) {
	h.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		h.t.Fatalf("marshal: %v", err)
	}
	h.send(string(b))
}

func (h *acpHarness) next() map[string]any {
	h.t.Helper()
	select {
	case f, ok := <-h.frames:
		if !ok {
			h.t.Fatal("server exited before the expected frame")
		}
		return f
	case <-time.After(10 * time.Second):
		h.t.Fatal("timed out waiting for a frame")
	}
	return nil
}

// until returns the frames read up to and including the response to id.
func (h *acpHarness) until(id any) []map[string]any {
	h.t.Helper()
	var got []map[string]any
	for {
		f := h.next()
		got = append(got, f)
		if isACPResponse(f) && reflect.DeepEqual(f["id"], id) {
			return got
		}
	}
}

// request sends a request with a fresh numeric id and returns the frames
// up to and including its response.
func (h *acpHarness) request(method string, params any) []map[string]any {
	h.t.Helper()
	h.nextID++
	h.sendJSON(map[string]any{"jsonrpc": "2.0", "id": h.nextID, "method": method, "params": params})
	return h.until(float64(h.nextID))
}

func (h *acpHarness) newSession(cwd string) string {
	h.t.Helper()
	frames := h.request("session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	id, _ := dig(frames[len(frames)-1], "result", "sessionId").(string)
	if id == "" {
		h.t.Fatalf("session/new: %v", frames)
	}
	return id
}

func (h *acpHarness) prompt(sessionID string, blocks ...map[string]any) []map[string]any {
	h.t.Helper()
	return h.request("session/prompt", map[string]any{"sessionId": sessionID, "prompt": blocks})
}

// close closes stdin and returns the remaining frames and the exit code.
func (h *acpHarness) close() ([]map[string]any, int) {
	h.t.Helper()
	h.in.Close()
	var got []map[string]any
	for {
		select {
		case f, ok := <-h.frames:
			if !ok {
				return got, <-h.exit
			}
			got = append(got, f)
		case <-time.After(10 * time.Second):
			h.t.Fatal("timed out waiting for the server to exit")
		}
	}
}

// runACPTest sends raw request lines one at a time, waiting for each
// request's response, then closes stdin and returns every frame in
// order.
func runACPTest(t *testing.T, cfg acpConfig, lines ...string) []map[string]any {
	t.Helper()
	h := startACP(t, cfg)
	var frames []map[string]any
	for _, line := range lines {
		h.send(line)
		var req map[string]any
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			frames = append(frames, h.until(nil)...) // parse errors answer id null
		} else if id, ok := req["id"]; ok {
			frames = append(frames, h.until(id)...)
		}
	}
	rest, code := h.close()
	if code != 0 {
		t.Fatalf("serveACP exit = %d", code)
	}
	return append(frames, rest...)
}

func isACPResponse(f map[string]any) bool {
	_, hasID := f["id"]
	_, hasResult := f["result"]
	_, hasError := f["error"]
	return hasID && (hasResult || hasError)
}

// dig walks nested decoded JSON: string keys index objects, int keys
// index arrays. Missing paths yield nil.
func dig(v any, path ...any) any {
	for _, k := range path {
		switch k := k.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok || k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

// acpUpdatesOf returns the update payloads of the session/update
// notifications among frames, in order.
func acpUpdatesOf(frames []map[string]any) []map[string]any {
	var out []map[string]any
	for _, f := range frames {
		if f["method"] == "session/update" {
			if u, ok := dig(f, "params", "update").(map[string]any); ok {
				out = append(out, u)
			}
		}
	}
	return out
}

func acpKinds(updates []map[string]any) []string {
	out := make([]string, len(updates))
	for i, u := range updates {
		out[i], _ = u["sessionUpdate"].(string)
	}
	return out
}

func lastFrame(frames []map[string]any) map[string]any { return frames[len(frames)-1] }

func textBlock(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

// blockingProvider blocks every Chat until its context is cancelled.
type blockingProvider struct {
	started chan struct{}
	once    sync.Once
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{started: make(chan struct{})}
}

func (p *blockingProvider) Chat(ctx context.Context, _ ai.ChatRequest) (ai.ChatResponse, error) {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return ai.ChatResponse{}, ctx.Err()
}

func (p *blockingProvider) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-p.started:
	case <-time.After(10 * time.Second):
		t.Fatal("provider was never called")
	}
}

func TestACP_InitializeAdvertisesCapabilities(t *testing.T) {
	cfg := newACPTestConfig(providers.NewFauxProvider(nil))
	cfg.noSession = false
	frames := runACPTest(t, cfg,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":99,"clientCapabilities":{}}}`)
	if len(frames) != 1 {
		t.Fatalf("frames = %v", frames)
	}
	res := frames[0]["result"]
	if got := dig(res, "protocolVersion"); got != float64(1) {
		t.Fatalf("protocolVersion = %v", got)
	}
	if got := dig(res, "agentCapabilities", "loadSession"); got != true {
		t.Fatalf("loadSession = %v", got)
	}
	pc := dig(res, "agentCapabilities", "promptCapabilities")
	if dig(pc, "embeddedContext") != true || dig(pc, "image") != false || dig(pc, "audio") != false {
		t.Fatalf("promptCapabilities = %v", pc)
	}
	if am, ok := dig(res, "authMethods").([]any); !ok || len(am) != 0 {
		t.Fatalf("authMethods = %v", dig(res, "authMethods"))
	}
	if got := dig(res, "agentInfo", "version"); got != Version {
		t.Fatalf("agentInfo.version = %v want %q", got, Version)
	}
}

func TestACP_InitializeLoadSessionFalseWhenNoSession(t *testing.T) {
	frames := runACPTest(t, newACPTestConfig(providers.NewFauxProvider(nil)),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`)
	if got := dig(frames[0], "result", "agentCapabilities", "loadSession"); got != false {
		t.Fatalf("loadSession = %v", got)
	}
}

func TestACP_AuthenticateReturnsEmptyObject(t *testing.T) {
	frames := runACPTest(t, newACPTestConfig(providers.NewFauxProvider(nil)),
		`{"jsonrpc":"2.0","id":1,"method":"authenticate","params":{"methodId":"x"}}`)
	if res, ok := frames[0]["result"].(map[string]any); !ok || len(res) != 0 {
		t.Fatalf("frame = %v", frames[0])
	}
}

func TestACP_MalformedJSONReturnsParseError(t *testing.T) {
	frames := runACPTest(t, newACPTestConfig(providers.NewFauxProvider(nil)), `{not json`)
	if len(frames) != 1 {
		t.Fatalf("frames = %v", frames)
	}
	if got := dig(frames[0], "error", "code"); got != float64(rpcParseError) {
		t.Fatalf("code = %v", got)
	}
	if id, ok := frames[0]["id"]; !ok || id != nil {
		t.Fatalf("id = %v (present=%v), want null", id, ok)
	}
}

func TestACP_UnknownMethodReturnsMethodNotFound(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want any
	}{
		{"numeric id", `7`, float64(7)},
		{"string id", `"abc"`, "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frames := runACPTest(t, newACPTestConfig(providers.NewFauxProvider(nil)),
				`{"jsonrpc":"2.0","id":`+tc.id+`,"method":"session/set_mode","params":{}}`)
			if len(frames) != 1 {
				t.Fatalf("frames = %v", frames)
			}
			if got := dig(frames[0], "error", "code"); got != float64(rpcMethodNotFound) {
				t.Fatalf("code = %v", got)
			}
			if frames[0]["id"] != tc.want {
				t.Fatalf("id = %#v want %#v", frames[0]["id"], tc.want)
			}
		})
	}
}

func TestACP_UnknownNotificationIsIgnored(t *testing.T) {
	frames := runACPTest(t, newACPTestConfig(providers.NewFauxProvider(nil)),
		`{"jsonrpc":"2.0","method":"bogus/notify","params":{}}`)
	if len(frames) != 0 {
		t.Fatalf("frames = %v", frames)
	}
}

func TestACP_NewSessionRejectsRelativeCwd(t *testing.T) {
	frames := runACPTest(t, newACPTestConfig(providers.NewFauxProvider(nil)),
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"rel/dir","mcpServers":[]}}`)
	if got := dig(frames[0], "error", "code"); got != float64(rpcInvalidParams) {
		t.Fatalf("frame = %v", frames[0])
	}
}

func TestACP_NewSessionCreatesSessionFileUnderCwdBucket(t *testing.T) {
	cfg := newACPTestConfig(providers.NewFauxProvider(nil))
	cfg.noSession = false
	cfg.sessionRoot = t.TempDir()
	h := startACP(t, cfg)
	id := h.newSession(t.TempDir())
	h.close()
	matches, _ := filepath.Glob(filepath.Join(cfg.sessionRoot, "*", id+".jsonl"))
	if len(matches) != 1 {
		t.Fatalf("session file for %s not found under %s", id, cfg.sessionRoot)
	}
}

func TestACP_PromptEmitsMessageUpdateThenEndTurn(t *testing.T) {
	faux := providers.NewFauxProvider([]ai.ChatResponse{{Content: strptr("hi")}})
	h := startACP(t, newACPTestConfig(faux))
	sid := h.newSession(t.TempDir())
	frames := h.prompt(sid, textBlock("hello"))
	if len(frames) != 2 {
		t.Fatalf("frames = %v", frames)
	}
	if got := dig(frames[0], "params", "sessionId"); got != sid {
		t.Fatalf("update sessionId = %v", got)
	}
	u := dig(frames[0], "params", "update")
	if dig(u, "sessionUpdate") != "agent_message_chunk" || dig(u, "content", "text") != "hi" {
		t.Fatalf("update = %v", u)
	}
	if got := dig(frames[1], "result", "stopReason"); got != "end_turn" {
		t.Fatalf("response = %v", frames[1])
	}
}

func TestACP_PromptToolCallUpdates(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "a.txt"), []byte("hello file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "Read", Arguments: `{"path":"a.txt"}`}}},
		{Content: strptr("done")},
	})
	h := startACP(t, newACPTestConfig(faux))
	sid := h.newSession(cwd)
	frames := h.prompt(sid, textBlock("read it"))
	ups := acpUpdatesOf(frames)
	if got, want := acpKinds(ups), []string{"tool_call", "tool_call_update", "agent_message_chunk"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("updates = %v want %v", got, want)
	}
	call := ups[0]
	if call["toolCallId"] != "c1" || call["kind"] != "read" || call["status"] != "in_progress" {
		t.Fatalf("tool_call = %v", call)
	}
	if got := dig(call, "locations", 0, "path"); got != filepath.Join(cwd, "a.txt") {
		t.Fatalf("location = %v", got)
	}
	if got := dig(call, "rawInput", "path"); got != "a.txt" {
		t.Fatalf("rawInput = %v", call["rawInput"])
	}
	upd := ups[1]
	if upd["toolCallId"] != "c1" || upd["status"] != "completed" {
		t.Fatalf("tool_call_update = %v", upd)
	}
	if text, _ := dig(upd, "content", 0, "content", "text").(string); !strings.Contains(text, "hello file") {
		t.Fatalf("tool output = %q", text)
	}
	if dig(ups[2], "content", "text") != "done" {
		t.Fatalf("message = %v", ups[2])
	}
	if got := dig(lastFrame(frames), "result", "stopReason"); got != "end_turn" {
		t.Fatalf("response = %v", lastFrame(frames))
	}
}

func TestACP_ToolErrorMarkedFailed(t *testing.T) {
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "Read", Arguments: `{"path":"missing.txt"}`}}},
		{Content: strptr("no such file")},
	})
	h := startACP(t, newACPTestConfig(faux))
	sid := h.newSession(t.TempDir())
	ups := acpUpdatesOf(h.prompt(sid, textBlock("read it")))
	if len(ups) < 2 || ups[1]["sessionUpdate"] != "tool_call_update" || ups[1]["status"] != "failed" {
		t.Fatalf("updates = %v", ups)
	}
}

func TestACP_ToolsUseSessionCwdNotProcessCwd(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "acp-marker.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "Glob", Arguments: `{"pattern":"acp-marker*"}`}}},
		{Content: strptr("found")},
	})
	h := startACP(t, newACPTestConfig(faux))
	sid := h.newSession(cwd)
	ups := acpUpdatesOf(h.prompt(sid, textBlock("find it")))
	if len(ups) < 2 || ups[1]["status"] != "completed" {
		t.Fatalf("updates = %v", ups)
	}
	if text, _ := dig(ups[1], "content", 0, "content", "text").(string); !strings.Contains(text, "acp-marker.txt") {
		t.Fatalf("glob output = %q", text)
	}
}

func TestACP_ReasoningEmitsThoughtChunkAndIsNotPersisted(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	faux := providers.NewFauxProvider([]ai.ChatResponse{
		{Content: strptr("answer"), Reasoning: strptr("secret thoughts")},
	})
	cfg := newACPTestConfig(faux)
	cfg.noSession = false
	cfg.sessionRoot = root
	h := startACP(t, cfg)
	sid := h.newSession(cwd)
	ups := acpUpdatesOf(h.prompt(sid, textBlock("think")))
	if got, want := acpKinds(ups), []string{"agent_thought_chunk", "agent_message_chunk"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("updates = %v want %v", got, want)
	}
	if dig(ups[0], "content", "text") != "secret thoughts" {
		t.Fatalf("thought = %v", ups[0])
	}
	h.close()

	h2 := startACP(t, cfg)
	frames := h2.request("session/load", map[string]any{"sessionId": sid, "cwd": cwd, "mcpServers": []any{}})
	for _, u := range acpUpdatesOf(frames) {
		if u["sessionUpdate"] == "agent_thought_chunk" {
			t.Fatalf("reasoning replayed: %v", u)
		}
	}
}

func TestACP_PromptConvertsResourceBlocks(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider([]ai.ChatResponse{{Content: strptr("ok")}})}
	h := startACP(t, newACPTestConfig(rec))
	sid := h.newSession(t.TempDir())
	frames := h.prompt(sid,
		textBlock("explain these"),
		map[string]any{"type": "resource_link", "uri": "file:///x/y.go", "name": "y.go"},
		map[string]any{"type": "resource", "resource": map[string]any{"uri": "file:///x/z.go", "text": "package z\n"}},
	)
	if got := dig(lastFrame(frames), "result", "stopReason"); got != "end_turn" {
		t.Fatalf("response = %v", lastFrame(frames))
	}
	if len(rec.reqs) != 1 {
		t.Fatalf("provider calls = %d", len(rec.reqs))
	}
	msgs := rec.reqs[0].Messages
	last := msgs[len(msgs)-1]
	if last.Role != ai.RoleUser || last.Content == nil {
		t.Fatalf("last message = %+v", last)
	}
	for _, want := range []string{"explain these", "Referenced file: /x/y.go", "Contents of /x/z.go:", "package z"} {
		if !strings.Contains(*last.Content, want) {
			t.Fatalf("user message missing %q: %q", want, *last.Content)
		}
	}
}

func TestACP_PromptRejectsImageBlock(t *testing.T) {
	rec := &recordingProvider{inner: providers.NewFauxProvider(nil)}
	h := startACP(t, newACPTestConfig(rec))
	sid := h.newSession(t.TempDir())
	frames := h.prompt(sid, map[string]any{"type": "image", "data": "AAAA", "mimeType": "image/png"})
	if got := dig(lastFrame(frames), "error", "code"); got != float64(rpcInvalidParams) {
		t.Fatalf("response = %v", lastFrame(frames))
	}
	h.close()
	if len(rec.reqs) != 0 {
		t.Fatalf("provider called %d times", len(rec.reqs))
	}
}

func TestACP_PromptUnknownSessionErrors(t *testing.T) {
	h := startACP(t, newACPTestConfig(providers.NewFauxProvider(nil)))
	frames := h.prompt("no-such-session", textBlock("hi"))
	if got := dig(lastFrame(frames), "error", "code"); got != float64(rpcInvalidParams) {
		t.Fatalf("response = %v", lastFrame(frames))
	}
}

func TestACP_CancelRespondsCancelled(t *testing.T) {
	bp := newBlockingProvider()
	h := startACP(t, newACPTestConfig(bp))
	sid := h.newSession(t.TempDir())
	h.sendJSON(map[string]any{"jsonrpc": "2.0", "id": 100, "method": "session/prompt",
		"params": map[string]any{"sessionId": sid, "prompt": []any{textBlock("go")}}})
	bp.waitStarted(t)
	h.sendJSON(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": sid}})
	frames := h.until(float64(100))
	if got := dig(lastFrame(frames), "result", "stopReason"); got != "cancelled" {
		t.Fatalf("response = %v", lastFrame(frames))
	}
}

func TestACP_ConcurrentPromptOnBusySessionRejected(t *testing.T) {
	bp := newBlockingProvider()
	h := startACP(t, newACPTestConfig(bp))
	sid := h.newSession(t.TempDir())
	prompt := map[string]any{"sessionId": sid, "prompt": []any{textBlock("go")}}
	h.sendJSON(map[string]any{"jsonrpc": "2.0", "id": 100, "method": "session/prompt", "params": prompt})
	bp.waitStarted(t)
	h.sendJSON(map[string]any{"jsonrpc": "2.0", "id": 101, "method": "session/prompt", "params": prompt})
	frames := h.until(float64(101))
	if got := dig(lastFrame(frames), "error", "code"); got != float64(rpcBusy) {
		t.Fatalf("second prompt response = %v", lastFrame(frames))
	}
	h.sendJSON(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": sid}})
	frames = h.until(float64(100))
	if got := dig(lastFrame(frames), "result", "stopReason"); got != "cancelled" {
		t.Fatalf("first prompt response = %v", lastFrame(frames))
	}
}

func TestACP_MaxIterationsMapsToMaxTurnRequests(t *testing.T) {
	ls := ai.ChatResponse{ToolCalls: []ai.ToolCall{{ID: "c", Name: "LS", Arguments: `{"path":"."}`}}}
	cfg := newACPTestConfig(providers.NewFauxProvider([]ai.ChatResponse{ls, ls, ls}))
	cfg.maxIterations = 2
	h := startACP(t, cfg)
	sid := h.newSession(t.TempDir())
	frames := h.prompt(sid, textBlock("loop"))
	if got := dig(lastFrame(frames), "result", "stopReason"); got != "max_turn_requests" {
		t.Fatalf("response = %v", lastFrame(frames))
	}
}

func TestACP_ProviderErrorReturnsInternalError(t *testing.T) {
	h := startACP(t, newACPTestConfig(providers.NewFauxProvider(nil)))
	sid := h.newSession(t.TempDir())
	frames := h.prompt(sid, textBlock("hi"))
	resp := lastFrame(frames)
	if got := dig(resp, "error", "code"); got != float64(rpcInternalError) {
		t.Fatalf("response = %v", resp)
	}
	if msg, _ := dig(resp, "error", "message").(string); !strings.Contains(msg, "exhausted") {
		t.Fatalf("message = %q", msg)
	}
}

func TestACP_LoadSessionReplaysHistory(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "a.txt"), []byte("hello file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := newACPTestConfig(providers.NewFauxProvider([]ai.ChatResponse{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "Read", Arguments: `{"path":"a.txt"}`}}},
		{Content: strptr("first answer")},
	}))
	cfg.noSession = false
	cfg.sessionRoot = root
	h := startACP(t, cfg)
	sid := h.newSession(cwd)
	h.prompt(sid, textBlock("first question"))
	h.close()

	rec := &recordingProvider{inner: providers.NewFauxProvider([]ai.ChatResponse{{Content: strptr("second answer")}})}
	cfg.provider = rec
	h2 := startACP(t, cfg)
	frames := h2.request("session/load", map[string]any{"sessionId": sid, "cwd": cwd, "mcpServers": []any{}})
	ups := acpUpdatesOf(frames)
	want := []string{"user_message_chunk", "tool_call", "tool_call_update", "agent_message_chunk"}
	if got := acpKinds(ups); !reflect.DeepEqual(got, want) {
		t.Fatalf("replay = %v want %v", got, want)
	}
	if dig(ups[0], "content", "text") != "first question" || dig(ups[3], "content", "text") != "first answer" {
		t.Fatalf("replay = %v", ups)
	}
	if res, ok := lastFrame(frames)["result"].(map[string]any); !ok || len(res) != 0 {
		t.Fatalf("load response = %v", lastFrame(frames))
	}

	frames = h2.prompt(sid, textBlock("follow up"))
	if got := dig(lastFrame(frames), "result", "stopReason"); got != "end_turn" {
		t.Fatalf("response = %v", lastFrame(frames))
	}
	var seen []string
	for _, m := range rec.reqs[0].Messages {
		if m.Content != nil {
			seen = append(seen, *m.Content)
		}
	}
	joined := strings.Join(seen, "\n")
	for _, w := range []string{"first question", "first answer", "follow up"} {
		if !strings.Contains(joined, w) {
			t.Fatalf("request missing %q: %q", w, joined)
		}
	}
}

func TestACP_LoadSessionUnavailableWhenNoSession(t *testing.T) {
	frames := runACPTest(t, newACPTestConfig(providers.NewFauxProvider(nil)),
		`{"jsonrpc":"2.0","id":1,"method":"session/load","params":{"sessionId":"x","cwd":"/tmp","mcpServers":[]}}`)
	if got := dig(frames[0], "error", "code"); got != float64(rpcMethodNotFound) {
		t.Fatalf("frame = %v", frames[0])
	}
}

func TestACP_EOFCancelsRunningPromptAndExits(t *testing.T) {
	bp := newBlockingProvider()
	h := startACP(t, newACPTestConfig(bp))
	sid := h.newSession(t.TempDir())
	h.sendJSON(map[string]any{"jsonrpc": "2.0", "id": 100, "method": "session/prompt",
		"params": map[string]any{"sessionId": sid, "prompt": []any{textBlock("go")}}})
	bp.waitStarted(t)
	frames, code := h.close()
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(frames) == 0 || dig(lastFrame(frames), "result", "stopReason") != "cancelled" {
		t.Fatalf("frames = %v", frames)
	}
}

func TestACPToolKind(t *testing.T) {
	cases := map[string]string{
		"Read":            "read",
		"Write":           "edit",
		"Edit":            "edit",
		"LS":              "search",
		"Glob":            "search",
		"Grep":            "search",
		"Bash":            "execute",
		"WebSearch":       "fetch",
		"Agent":           "other",
		"Skill":           "other",
		"yolium_complete": "other",
	}
	for name, want := range cases {
		if got := acpToolKind(name); got != want {
			t.Errorf("acpToolKind(%q) = %q want %q", name, got, want)
		}
	}
}

func TestACPToolTitle(t *testing.T) {
	cases := []struct {
		call ai.ToolCall
		want string
	}{
		{ai.ToolCall{Name: "Skill", Arguments: `{"name":"plan"}`}, "Skill plan"},
		{ai.ToolCall{Name: "Read", Arguments: `{"path":"src/a.go","offset":10}`}, "Read src/a.go"},
		{ai.ToolCall{Name: "Grep", Arguments: `{"pattern":"TODO","path":"internal"}`}, "Grep TODO"},
		{ai.ToolCall{Name: "Bash", Arguments: `{"command":"docker run -p 8000:8000 app"}`}, "`docker run -p 8000:8000 app`"},
		{ai.ToolCall{Name: "Bash", Arguments: `{"command":"go vet ./...\ngo test ./..."}`}, "`go vet ./... go test ./...`"},
		{ai.ToolCall{Name: "LS", Arguments: `{}`}, "LS"},
		{ai.ToolCall{Name: "Read", Arguments: `{"path":`}, "Read"},
		{ai.ToolCall{Name: "yolium_complete", Arguments: `{"summary":"done"}`}, "yolium_complete"},
	}
	for _, tc := range cases {
		if got := acpToolTitle(tc.call); got != tc.want {
			t.Errorf("acpToolTitle(%s %s) = %q want %q", tc.call.Name, tc.call.Arguments, got, tc.want)
		}
	}
}

func TestACPLocations(t *testing.T) {
	cases := []struct {
		name string
		call ai.ToolCall
		want []acpLocation
	}{
		{"relative joined to cwd", ai.ToolCall{Name: "Read", Arguments: `{"path":"src/a.go"}`}, []acpLocation{{Path: "/work/src/a.go"}}},
		{"absolute kept", ai.ToolCall{Name: "Edit", Arguments: `{"path":"/etc/hosts"}`}, []acpLocation{{Path: "/etc/hosts"}}},
		{"write", ai.ToolCall{Name: "Write", Arguments: `{"path":"b.txt","content":"x"}`}, []acpLocation{{Path: "/work/b.txt"}}},
		{"non-file tool", ai.ToolCall{Name: "Bash", Arguments: `{"command":"ls"}`}, nil},
		{"bad json", ai.ToolCall{Name: "Read", Arguments: `{"path":`}, nil},
		{"missing path", ai.ToolCall{Name: "Read", Arguments: `{}`}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acpLocations(tc.call, "/work"); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestACPPromptText(t *testing.T) {
	cases := []struct {
		name    string
		blocks  []acpContentBlock
		want    string
		wantErr bool
	}{
		{"text joined with blank lines", []acpContentBlock{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}}, "a\n\nb", false},
		{"non-file link kept raw", []acpContentBlock{{Type: "resource_link", URI: "https://example.com/x"}}, "Referenced file: https://example.com/x", false},
		{"blob-only resource is a reference", []acpContentBlock{{Type: "resource", Resource: &acpEmbeddedResource{URI: "file:///p/img.bin", Blob: "AAAA"}}}, "Referenced file: /p/img.bin", false},
		{"audio rejected", []acpContentBlock{{Type: "audio"}}, "", true},
		{"unknown rejected", []acpContentBlock{{Type: "bogus"}}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := acpPromptText(tc.blocks)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestACPUpdatesForMessage_TruncatesLongToolOutput(t *testing.T) {
	body := strings.Repeat("x", acpMaxToolOutput+500)
	ups := acpUpdatesForMessage(ai.Message{Role: ai.RoleTool, ToolCallID: "c1", Content: &body}, "/work")
	if len(ups) != 1 {
		t.Fatalf("updates = %v", ups)
	}
	text := ups[0].Content.([]acpToolCallContent)[0].Content.Text
	if want := strings.Repeat("x", acpMaxToolOutput) + "…(truncated)"; text != want {
		t.Fatalf("text has len %d, want truncated to %d plus suffix", len(text), acpMaxToolOutput)
	}
}

func TestACPUpdatesForMessage_InvalidArgsOmitRawInput(t *testing.T) {
	m := ai.Message{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "Bash", Arguments: `{"command":`}}}
	ups := acpUpdatesForMessage(m, "/work")
	if len(ups) != 1 || ups[0].RawInput != nil {
		t.Fatalf("updates = %+v", ups)
	}
	b, err := json.Marshal(ups[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "rawInput") {
		t.Fatalf("rawInput present: %s", b)
	}
}

func TestACPUpdatesForMessage_SystemMessageEmitsNothing(t *testing.T) {
	sys := "system"
	if ups := acpUpdatesForMessage(ai.Message{Role: ai.RoleSystem, Content: &sys}, "/work"); len(ups) != 0 {
		t.Fatalf("updates = %v", ups)
	}
}
