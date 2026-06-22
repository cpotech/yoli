// Package ai defines the provider-agnostic chat types and Provider interface
// that every LLM backend in Yoli implements.
package ai

import (
	"context"
	"iter"
)

// Role identifies the speaker of a Message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is a single function-call invocation produced by an assistant.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message is a single turn in a chat conversation.
//
// Which fields are populated depends on Role:
//
//	system / user: Content
//	assistant:     Content (may be nil), ToolCalls
//	tool:          Content, ToolCallID
type Message struct {
	Role       Role       `json:"role"`
	Content    *string    `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string     `json:"toolCallId,omitempty"`
}

// ToolDefinition describes a tool exposed to the model.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ChatRequest is the input to Provider.Chat.
type ChatRequest struct {
	Model    string
	Messages []Message
	Tools    []ToolDefinition
	// MaxTokens caps provider output tokens. Zero means unset; values
	// greater than zero should be forwarded as provider max output tokens.
	MaxTokens int
}

// Usage holds token and cost information returned by a provider.
type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost,omitempty"`
}

// ChatResponse is a non-streaming reply from a Provider.
type ChatResponse struct {
	Content   *string
	ToolCalls []ToolCall
	Usage     *Usage
}

// ChunkType discriminates ChatStreamChunk.
type ChunkType string

const (
	ChunkContent  ChunkType = "content"
	ChunkToolCall ChunkType = "tool_call"
	ChunkFinish   ChunkType = "finish"
)

// ChatStreamChunk is a single event from a streaming provider.
//
// Which fields are populated depends on Type:
//
//	ChunkContent:  Delta
//	ChunkToolCall: Index, ID, Name, ArgumentsDelta
//	ChunkFinish:   Reason
type ChatStreamChunk struct {
	Type           ChunkType
	Delta          string
	Index          int
	ID             string
	Name           string
	ArgumentsDelta string
	Reason         string
}

// Provider is the minimal interface every backend implements.
//
// Streaming support is exposed via the optional StreamingProvider
// interface; callers should type-assert to detect it.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// StreamingProvider is implemented by providers that can produce a token
// stream in addition to the buffered Chat response.
//
// The returned iterator yields chunks in order until the stream completes
// or a mid-stream error is reported. The outer error covers failures that
// happen before the stream starts (transport, auth, etc.).
type StreamingProvider interface {
	Provider
	ChatStream(ctx context.Context, req ChatRequest) (iter.Seq2[ChatStreamChunk, error], error)
}
