package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"strings"

	"yoli/internal/ai"
)

// OpenRouterID is the canonical id string for the OpenRouter provider.
const OpenRouterID = "openrouter"

const (
	openRouterModelPrefix    = OpenRouterID + ":"
	openRouterDefaultBaseURL = "https://openrouter.ai/api/v1"
)

// OpenRouterOptions configures a new OpenRouterProvider. All fields are
// optional; an empty struct is valid as long as OPENROUTER_API_KEY is set
// in the environment.
type OpenRouterOptions struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	Referer    string
	Title      string
}

// OpenRouterProvider speaks the OpenAI-compatible API exposed by
// openrouter.ai/api/v1.
type OpenRouterProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
	referer string
	title   string
}

// NewOpenRouterProvider validates options and returns a ready provider.
// Returns an error if no API key is available via opts or environment.
func NewOpenRouterProvider(opts OpenRouterOptions) (*OpenRouterProvider, error) {
	key := opts.APIKey
	if key == "" {
		key = os.Getenv("OPENROUTER_API_KEY")
	}
	if key == "" {
		return nil, errors.New(
			"OpenRouter API key missing — set the OPENROUTER_API_KEY env var or pass opts.APIKey",
		)
	}
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = openRouterDefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &OpenRouterProvider{
		apiKey:  key,
		baseURL: baseURL,
		client:  client,
		referer: opts.Referer,
		title:   opts.Title,
	}, nil
}

// Chat performs a non-streaming completion.
func (p *OpenRouterProvider) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	resp, err := p.send(ctx, req, false)
	if err != nil {
		return ai.ChatResponse{}, err
	}
	defer resp.Body.Close()

	var wire wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return ai.ChatResponse{}, fmt.Errorf("openrouter: decode response: %w", err)
	}

	var content *string
	var toolCalls []ai.ToolCall
	var usage *ai.Usage
	if len(wire.Choices) > 0 && wire.Choices[0].Message != nil {
		m := wire.Choices[0].Message
		content = m.Content
		if len(m.ToolCalls) > 0 {
			toolCalls = make([]ai.ToolCall, len(m.ToolCalls))
			for i, c := range m.ToolCalls {
				toolCalls[i] = ai.ToolCall{
					ID:        c.ID,
					Name:      c.Function.Name,
					Arguments: c.Function.Arguments,
				}
			}
		}
	}
	if wire.Usage != nil {
		cost := 0.0
		if wire.Usage.Cost != nil {
			cost = *wire.Usage.Cost
		}
		usage = &ai.Usage{
			PromptTokens:     wire.Usage.PromptTokens,
			CompletionTokens: wire.Usage.CompletionTokens,
			TotalTokens:      wire.Usage.TotalTokens,
			Cost:             cost,
		}
	}
	return ai.ChatResponse{Content: content, ToolCalls: toolCalls, Usage: usage}, nil
}

// ChatStream performs a streaming completion. The outer error covers
// transport / non-2xx failures; mid-stream issues surface through the
// iterator's error channel.
func (p *OpenRouterProvider) ChatStream(
	ctx context.Context, req ai.ChatRequest,
) (iter.Seq2[ai.ChatStreamChunk, error], error) {
	resp, err := p.send(ctx, req, true)
	if err != nil {
		return nil, err
	}
	seq := func(yield func(ai.ChatStreamChunk, error) bool) {
		defer resp.Body.Close()
		acc := make(map[int]*ToolCallAccumulator)
		for raw, ierr := range IterSSE(resp.Body) {
			if ierr != nil {
				yield(ai.ChatStreamChunk{}, ierr)
				return
			}
			var chunk wireStreamChunk
			if err := json.Unmarshal(raw, &chunk); err != nil {
				// Skip malformed frames, matching the TS behaviour.
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]
			if choice.Delta != nil {
				if choice.Delta.Content != "" {
					if !yield(ai.ChatStreamChunk{
						Type:  ai.ChunkContent,
						Delta: choice.Delta.Content,
					}, nil) {
						return
					}
				}
				for _, tc := range choice.Delta.ToolCalls {
					if !yield(MergeToolCallDeltas(acc, tc), nil) {
						return
					}
				}
			}
			if choice.FinishReason != "" {
				if !yield(ai.ChatStreamChunk{
					Type:   ai.ChunkFinish,
					Reason: choice.FinishReason,
				}, nil) {
					return
				}
			}
		}
	}
	return seq, nil
}

func (p *OpenRouterProvider) send(
	ctx context.Context, req ai.ChatRequest, stream bool,
) (*http.Response, error) {
	model := strings.TrimPrefix(req.Model, openRouterModelPrefix)

	wireMsgs := make([]any, len(req.Messages))
	for i, m := range req.Messages {
		wireMsgs[i] = toWireMessage(m)
	}

	body := wireRequest{Model: model, Messages: wireMsgs}
	if len(req.Tools) > 0 {
		body.Tools = make([]wireTool, len(req.Tools))
		for i, t := range req.Tools {
			body.Tools[i] = wireTool{Type: "function", Function: t}
		}
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	body.Usage = &wireRequestUsage{Include: true}
	if stream {
		body.Stream = true
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openrouter: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		p.baseURL+"/chat/completions",
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("openrouter: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if p.referer != "" {
		httpReq.Header.Set("HTTP-Referer", p.referer)
	}
	if p.title != "" {
		httpReq.Header.Set("X-Title", p.title)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter: request failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		msg := fmt.Sprintf(
			"openrouter: request failed: %d %s",
			resp.StatusCode, http.StatusText(resp.StatusCode),
		)
		if len(text) > 0 {
			msg += " — " + string(text)
		}
		return nil, errors.New(msg)
	}
	return resp, nil
}

// --- wire types ---

type wireToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function wireToolCallFn `json:"function"`
}

type wireToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireResponseMessage struct {
	Role      string         `json:"role"`
	Content   *string        `json:"content"`
	ToolCalls []wireToolCall `json:"tool_calls,omitempty"`
}

type wireResponseUsage struct {
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	TotalTokens      int      `json:"total_tokens"`
	Cost             *float64 `json:"cost,omitempty"`
}

type wireResponse struct {
	Choices []struct {
		Message *wireResponseMessage `json:"message"`
	} `json:"choices"`
	Usage *wireResponseUsage `json:"usage,omitempty"`
}

type wireStreamDelta struct {
	Content   string              `json:"content,omitempty"`
	ToolCalls []WireToolCallDelta `json:"tool_calls,omitempty"`
}

type wireStreamChunk struct {
	Choices []struct {
		Delta        *wireStreamDelta `json:"delta"`
		FinishReason string           `json:"finish_reason"`
	} `json:"choices"`
}

type wireRequestUsage struct {
	Include bool `json:"include"`
}

type wireRequest struct {
	Model     string            `json:"model"`
	Messages  []any             `json:"messages"`
	Tools     []wireTool        `json:"tools,omitempty"`
	MaxTokens int               `json:"max_tokens,omitempty"`
	Stream    bool              `json:"stream,omitempty"`
	Usage     *wireRequestUsage `json:"usage,omitempty"`
}

type wireTool struct {
	Type     string            `json:"type"`
	Function ai.ToolDefinition `json:"function"`
}

// toWireMessage converts an ai.Message to the JSON-marshalable shape
// expected by the OpenAI-compatible /chat/completions endpoint. The
// shape varies by role, so we use map[string]any to control which
// fields appear.
func toWireMessage(m ai.Message) any {
	switch m.Role {
	case ai.RoleSystem, ai.RoleUser:
		return map[string]any{
			"role":    string(m.Role),
			"content": derefString(m.Content),
		}
	case ai.RoleAssistant:
		out := map[string]any{
			"role":    "assistant",
			"content": m.Content, // *string → string or null
		}
		if len(m.ToolCalls) > 0 {
			tcs := make([]wireToolCall, len(m.ToolCalls))
			for i, c := range m.ToolCalls {
				tcs[i] = wireToolCall{
					ID:   c.ID,
					Type: "function",
					Function: wireToolCallFn{
						Name:      c.Name,
						Arguments: c.Arguments,
					},
				}
			}
			out["tool_calls"] = tcs
		}
		return out
	case ai.RoleTool:
		return map[string]any{
			"role":         "tool",
			"tool_call_id": m.ToolCallID,
			"content":      derefString(m.Content),
		}
	}
	return nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Compile-time interface checks.
var (
	_ ai.Provider          = (*OpenRouterProvider)(nil)
	_ ai.StreamingProvider = (*OpenRouterProvider)(nil)
)
