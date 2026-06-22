package providers

import (
	"context"
	"errors"
	"testing"

	"yoli/internal/ai"
)

func strPtr(s string) *string { return &s }

func baseRequest() ai.ChatRequest {
	return ai.ChatRequest{
		Model:    "faux",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: strPtr("hi")}},
	}
}

var (
	textTurn = ai.ChatResponse{
		Content:   strPtr("hello"),
		ToolCalls: nil,
	}
	toolTurn = ai.ChatResponse{
		Content: nil,
		ToolCalls: []ai.ToolCall{
			{ID: "call_1", Name: "lookup", Arguments: `{"q":"yoli"}`},
		},
	}
)

func TestFauxProvider_ReplaysSingleTextResponse(t *testing.T) {
	p := NewFauxProvider([]ai.ChatResponse{textTurn})
	resp, err := p.Chat(context.Background(), baseRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content == nil || *resp.Content != "hello" {
		t.Fatalf("Content = %v, want %q", resp.Content, "hello")
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("ToolCalls = %v, want empty", resp.ToolCalls)
	}
}

func TestFauxProvider_ReplaysToolCallResponse(t *testing.T) {
	p := NewFauxProvider([]ai.ChatResponse{toolTurn})
	resp, err := p.Chat(context.Background(), baseRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []ai.ToolCall{{ID: "call_1", Name: "lookup", Arguments: `{"q":"yoli"}`}}
	if len(resp.ToolCalls) != len(want) || resp.ToolCalls[0] != want[0] {
		t.Fatalf("ToolCalls = %#v, want %#v", resp.ToolCalls, want)
	}
}

func TestFauxProvider_ReturnsTurnsInOrder(t *testing.T) {
	p := NewFauxProvider([]ai.ChatResponse{textTurn, toolTurn})
	first, err := p.Chat(context.Background(), baseRequest())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := p.Chat(context.Background(), baseRequest())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first.Content == nil || *first.Content != "hello" {
		t.Fatalf("first.Content = %v, want %q", first.Content, "hello")
	}
	if len(second.ToolCalls) == 0 || second.ToolCalls[0].ID != "call_1" {
		t.Fatalf("second.ToolCalls = %#v, want first ID call_1", second.ToolCalls)
	}
}

func TestFauxProvider_ExhaustedScriptReturnsError(t *testing.T) {
	p := NewFauxProvider([]ai.ChatResponse{textTurn})
	if _, err := p.Chat(context.Background(), baseRequest()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	_, err := p.Chat(context.Background(), baseRequest())
	var exhausted *ScriptExhaustedError
	if !errors.As(err, &exhausted) {
		t.Fatalf("err = %T %v, want *ScriptExhaustedError", err, err)
	}
	if exhausted.ScriptLength != 1 {
		t.Fatalf("ScriptLength = %d, want 1", exhausted.ScriptLength)
	}
}

func TestFauxProvider_IgnoresRequestMessageContent(t *testing.T) {
	p := NewFauxProvider([]ai.ChatResponse{textTurn})
	resp, err := p.Chat(context.Background(), ai.ChatRequest{
		Model:    "faux",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: strPtr("something else entirely")}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content == nil || *resp.Content != "hello" {
		t.Fatalf("Content = %v, want %q", resp.Content, "hello")
	}
}
