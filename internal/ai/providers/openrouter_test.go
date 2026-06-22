package providers

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoli/internal/ai"
)

// --- helpers ---

type recordedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

// stubServer returns an httptest.Server that records the most recent
// request and responds with the given handler.
func stubServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.Method = r.Method
		rec.Path = r.URL.Path
		rec.Headers = r.Header.Clone()
		rec.Body = body
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func newProvider(t *testing.T, srv *httptest.Server, opts OpenRouterOptions) *OpenRouterProvider {
	t.Helper()
	if opts.APIKey == "" {
		opts.APIKey = "k"
	}
	if opts.BaseURL == "" {
		opts.BaseURL = srv.URL
	}
	p, err := NewOpenRouterProvider(opts)
	if err != nil {
		t.Fatalf("NewOpenRouterProvider: %v", err)
	}
	return p
}

func userReq(content string) ai.ChatRequest {
	c := content
	return ai.ChatRequest{
		Model:    "m",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: &c}},
	}
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func collectStream(
	t *testing.T,
	seq iter.Seq2[ai.ChatStreamChunk, error],
) []ai.ChatStreamChunk {
	t.Helper()
	var out []ai.ChatStreamChunk
	for chunk, err := range seq {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		out = append(out, chunk)
	}
	return out
}

// jsonChoicesResponse writes a minimal /chat/completions JSON body.
func jsonChoicesResponse(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// --- exports & identity ---

func TestOpenRouterID(t *testing.T) {
	if OpenRouterID != "openrouter" {
		t.Fatalf("OpenRouterID = %q, want %q", OpenRouterID, "openrouter")
	}
}

// --- constructor ---

func TestNewOpenRouterProvider_MissingAPIKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	_, err := NewOpenRouterProvider(OpenRouterOptions{})
	if err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("err = %v, want one mentioning OPENROUTER_API_KEY", err)
	}
}

func TestNewOpenRouterProvider_ReadsAPIKeyFromEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "env-key")
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	})
	p, err := NewOpenRouterProvider(OpenRouterOptions{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}
	if _, err := p.Chat(context.Background(), userReq("x")); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := rec.Headers.Get("Authorization"); got != "Bearer env-key" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer env-key")
	}
}

func TestNewOpenRouterProvider_OptionAPIKeyBeatsEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "env-key")
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	})
	p := newProvider(t, srv, OpenRouterOptions{APIKey: "opt-key"})
	if _, err := p.Chat(context.Background(), userReq("x")); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := rec.Headers.Get("Authorization"); got != "Bearer opt-key" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer opt-key")
	}
}

// --- Chat ---

func TestChat_PostsCompletionsWithBearerAuthAndJSONBody(t *testing.T) {
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": "hi"}},
			},
		})
	})
	p := newProvider(t, srv, OpenRouterOptions{APIKey: "sk-test"})

	if _, err := p.Chat(context.Background(), ai.ChatRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: strPtr("hello")}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if rec.Method != "POST" {
		t.Fatalf("Method = %q, want POST", rec.Method)
	}
	if rec.Path != "/chat/completions" {
		t.Fatalf("Path = %q, want /chat/completions", rec.Path)
	}
	if got := rec.Headers.Get("Authorization"); got != "Bearer sk-test" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := rec.Headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["model"] != "openai/gpt-4o-mini" {
		t.Fatalf("model = %v", body["model"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d", len(msgs))
	}
	m0, _ := msgs[0].(map[string]any)
	if m0["role"] != "user" || m0["content"] != "hello" {
		t.Fatalf("messages[0] = %v", m0)
	}
	if _, ok := body["stream"]; ok {
		t.Fatalf("stream should be omitted, got %v", body["stream"])
	}
}

func TestChat_SendsMaxTokensWhenSet(t *testing.T) {
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	})
	p := newProvider(t, srv, OpenRouterOptions{})

	req := userReq("x")
	req.MaxTokens = 4096
	if _, err := p.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got := int(body["max_tokens"].(float64)); got != 4096 {
		t.Fatalf("max_tokens = %d, want 4096", got)
	}
}

func TestChat_OmitsMaxTokensWhenZero(t *testing.T) {
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	})
	p := newProvider(t, srv, OpenRouterOptions{})

	if _, err := p.Chat(context.Background(), userReq("x")); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("max_tokens should be omitted, got %v", body["max_tokens"])
	}
}

func TestChat_StripsOpenRouterPrefixFromModelID(t *testing.T) {
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	})
	p := newProvider(t, srv, OpenRouterOptions{})
	if _, err := p.Chat(context.Background(), ai.ChatRequest{
		Model:    "openrouter:openai/gpt-4o-mini",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: strPtr("x")}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body, &body)
	if body["model"] != "openai/gpt-4o-mini" {
		t.Fatalf("model = %v, want openai/gpt-4o-mini", body["model"])
	}
}

func TestChat_DecodesToolCallsToCamelCase(t *testing.T) {
	srv, _ := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"role":    "assistant",
						"content": nil,
						"tool_calls": []any{
							map[string]any{
								"id":   "call_42",
								"type": "function",
								"function": map[string]any{
									"name":      "LS",
									"arguments": `{"path":"."}`,
								},
							},
						},
					},
				},
			},
		})
	})
	p := newProvider(t, srv, OpenRouterOptions{})
	res, err := p.Chat(context.Background(), userReq("list"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Content != nil {
		t.Fatalf("Content = %v, want nil", res.Content)
	}
	want := []ai.ToolCall{{ID: "call_42", Name: "LS", Arguments: `{"path":"."}`}}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0] != want[0] {
		t.Fatalf("ToolCalls = %#v, want %#v", res.ToolCalls, want)
	}
}

func TestChat_SerialisesAssistantToolCallsAndToolRepliesSnakeCase(t *testing.T) {
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	})
	p := newProvider(t, srv, OpenRouterOptions{})

	if _, err := p.Chat(context.Background(), ai.ChatRequest{
		Model: "m",
		Messages: []ai.Message{
			{Role: ai.RoleUser, Content: strPtr("go")},
			{
				Role:    ai.RoleAssistant,
				Content: nil,
				ToolCalls: []ai.ToolCall{
					{ID: "call_1", Name: "Read", Arguments: `{"path":"a"}`},
				},
			},
			{Role: ai.RoleTool, ToolCallID: "call_1", Content: strPtr("contents")},
		},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages len = %d", len(msgs))
	}

	assistant, _ := msgs[1].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Fatalf("assistant role = %v", assistant["role"])
	}
	if assistant["content"] != nil {
		t.Fatalf("assistant content = %v, want nil", assistant["content"])
	}
	tcs, _ := assistant["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("tool_calls len = %d", len(tcs))
	}
	tc0, _ := tcs[0].(map[string]any)
	if tc0["id"] != "call_1" || tc0["type"] != "function" {
		t.Fatalf("tool_calls[0] = %v", tc0)
	}
	fn, _ := tc0["function"].(map[string]any)
	if fn["name"] != "Read" || fn["arguments"] != `{"path":"a"}` {
		t.Fatalf("function = %v", fn)
	}

	tool, _ := msgs[2].(map[string]any)
	if tool["role"] != "tool" ||
		tool["tool_call_id"] != "call_1" ||
		tool["content"] != "contents" {
		t.Fatalf("tool msg = %v", tool)
	}
}

func TestChat_ErrorOnNon2xxIncludesStatus(t *testing.T) {
	srv, _ := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("nope"))
	})
	p := newProvider(t, srv, OpenRouterOptions{APIKey: "bad"})
	_, err := p.Chat(context.Background(), userReq("x"))
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want one containing 401", err)
	}
}

func TestChat_StripsTrailingSlashFromBaseURL(t *testing.T) {
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	})
	p := newProvider(t, srv, OpenRouterOptions{BaseURL: srv.URL + "/"})
	if _, err := p.Chat(context.Background(), userReq("x")); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if rec.Path != "/chat/completions" {
		t.Fatalf("Path = %q, want /chat/completions (trailing slash should be stripped)", rec.Path)
	}
}

func TestChat_AddsRefererAndTitleHeaders(t *testing.T) {
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	})
	p := newProvider(t, srv, OpenRouterOptions{
		Referer: "https://yoli.dev",
		Title:   "Yoli",
	})
	if _, err := p.Chat(context.Background(), userReq("x")); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := rec.Headers.Get("HTTP-Referer"); got != "https://yoli.dev" {
		t.Fatalf("HTTP-Referer = %q", got)
	}
	if got := rec.Headers.Get("X-Title"); got != "Yoli" {
		t.Fatalf("X-Title = %q", got)
	}
}

// --- ChatStream ---

func TestChatStream_SendsStreamTrueAndAcceptHeader(t *testing.T) {
	body := loadFixture(t, "openrouter-content.sse")
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	})
	p := newProvider(t, srv, OpenRouterOptions{})

	seq, err := p.ChatStream(context.Background(), userReq("hi"))
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	collectStream(t, seq)

	if got := rec.Headers.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q", got)
	}
	var parsed map[string]any
	if err := json.Unmarshal(rec.Body, &parsed); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if parsed["stream"] != true {
		t.Fatalf("stream = %v, want true", parsed["stream"])
	}
}

func TestChatStream_YieldsContentChunksInOrder(t *testing.T) {
	body := loadFixture(t, "openrouter-content.sse")
	srv, _ := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	})
	p := newProvider(t, srv, OpenRouterOptions{})

	seq, err := p.ChatStream(context.Background(), userReq("hi"))
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	chunks := collectStream(t, seq)

	var deltas []string
	for _, c := range chunks {
		if c.Type == ai.ChunkContent {
			deltas = append(deltas, c.Delta)
		}
	}
	want := []string{"Hel", "lo ", "world"}
	if len(deltas) != len(want) {
		t.Fatalf("deltas = %v, want %v", deltas, want)
	}
	for i := range want {
		if deltas[i] != want[i] {
			t.Fatalf("deltas[%d] = %q, want %q", i, deltas[i], want[i])
		}
	}
}

func TestChatStream_YieldsMergedToolCallChunks(t *testing.T) {
	body := loadFixture(t, "openrouter-tool-call.sse")
	srv, _ := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	})
	p := newProvider(t, srv, OpenRouterOptions{})

	seq, err := p.ChatStream(context.Background(), userReq("go"))
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	chunks := collectStream(t, seq)

	var tools []ai.ChatStreamChunk
	for _, c := range chunks {
		if c.Type == ai.ChunkToolCall {
			tools = append(tools, c)
		}
	}
	if len(tools) != 3 {
		t.Fatalf("tool chunks = %d, want 3", len(tools))
	}
	if tools[0].Index != 0 ||
		tools[0].ID != "call_42" ||
		tools[0].Name != "Read" ||
		tools[0].ArgumentsDelta != "" {
		t.Fatalf("tools[0] = %+v", tools[0])
	}
	if tools[1].ArgumentsDelta != `{"pa` {
		t.Fatalf("tools[1].ArgumentsDelta = %q", tools[1].ArgumentsDelta)
	}
	if tools[2].ArgumentsDelta != `th":"a.txt"}` {
		t.Fatalf("tools[2].ArgumentsDelta = %q", tools[2].ArgumentsDelta)
	}
	merged := tools[0].ArgumentsDelta + tools[1].ArgumentsDelta + tools[2].ArgumentsDelta
	if merged != `{"path":"a.txt"}` {
		t.Fatalf("merged = %q", merged)
	}
}

func TestChatStream_YieldsFinishChunkWithReason(t *testing.T) {
	body := loadFixture(t, "openrouter-tool-call.sse")
	srv, _ := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	})
	p := newProvider(t, srv, OpenRouterOptions{})

	seq, err := p.ChatStream(context.Background(), userReq("go"))
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	chunks := collectStream(t, seq)

	var finish *ai.ChatStreamChunk
	for i := range chunks {
		if chunks[i].Type == ai.ChunkFinish {
			finish = &chunks[i]
			break
		}
	}
	if finish == nil {
		t.Fatalf("no finish chunk found")
	}
	if finish.Reason != "tool_calls" {
		t.Fatalf("finish reason = %q, want tool_calls", finish.Reason)
	}
}

// --- Usage / cost parsing ---

func TestChat_ParsesUsageTokens(t *testing.T) {
	srv, _ := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "openrouter-usage.json"))
	})
	p := newProvider(t, srv, OpenRouterOptions{})
	res, err := p.Chat(context.Background(), userReq("hi"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Usage == nil {
		t.Fatalf("Usage is nil")
	}
	if res.Usage.PromptTokens != 25 {
		t.Fatalf("PromptTokens = %d, want 25", res.Usage.PromptTokens)
	}
	if res.Usage.CompletionTokens != 13 {
		t.Fatalf("CompletionTokens = %d, want 13", res.Usage.CompletionTokens)
	}
	if res.Usage.TotalTokens != 38 {
		t.Fatalf("TotalTokens = %d, want 38", res.Usage.TotalTokens)
	}
}

func TestChat_ParsesUsageCost(t *testing.T) {
	srv, _ := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "openrouter-usage.json"))
	})
	p := newProvider(t, srv, OpenRouterOptions{})
	res, err := p.Chat(context.Background(), userReq("hi"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Usage == nil {
		t.Fatalf("Usage is nil")
	}
	if res.Usage.Cost != 0.000456 {
		t.Fatalf("Cost = %f, want 0.000456", res.Usage.Cost)
	}
}

func TestChat_UsagePopulatedForFreeModelZeroCost(t *testing.T) {
	srv, _ := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "openrouter-usage-free.json"))
	})
	p := newProvider(t, srv, OpenRouterOptions{})
	res, err := p.Chat(context.Background(), userReq("hi"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Usage == nil {
		t.Fatalf("Usage is nil")
	}
	if res.Usage.PromptTokens != 0 || res.Usage.CompletionTokens != 0 || res.Usage.TotalTokens != 0 {
		t.Fatalf("Usage = %+v, want zeros", res.Usage)
	}
	if res.Usage.Cost != 0 {
		t.Fatalf("Cost = %f, want 0", res.Usage.Cost)
	}
}

func TestChat_UsageNilWhenOmitted(t *testing.T) {
	srv, _ := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "openrouter-usage-no-cost.json"))
	})
	p := newProvider(t, srv, OpenRouterOptions{})
	res, err := p.Chat(context.Background(), userReq("hi"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Usage == nil {
		t.Fatalf("Usage should be non-nil even without cost")
	}
	if res.Usage.PromptTokens != 10 {
		t.Fatalf("PromptTokens = %d, want 10", res.Usage.PromptTokens)
	}
}

func TestChat_SendsUsageIncludeInRequestBody(t *testing.T) {
	srv, rec := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		jsonChoicesResponse(w, map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"role": "assistant", "content": ""}},
			},
		})
	})
	p := newProvider(t, srv, OpenRouterOptions{})

	if _, err := p.Chat(context.Background(), userReq("x")); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	usage, ok := body["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage field missing or not an object: %v", body["usage"])
	}
	include, _ := usage["include"].(bool)
	if !include {
		t.Fatalf("usage.include should be true, got %v", usage["include"])
	}
}

func TestChatStream_ErrorOnNon2xxIncludesStatus(t *testing.T) {
	body := loadFixture(t, "openrouter-error.json")
	srv, _ := stubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write(body)
	})
	p := newProvider(t, srv, OpenRouterOptions{APIKey: "bad"})

	_, err := p.ChatStream(context.Background(), userReq("x"))
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want one containing 401", err)
	}
}
