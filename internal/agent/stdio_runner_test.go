package agent

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"yoli/internal/ai"
	"yoli/internal/ai/providers"
)

func strPtr(s string) *string { return &s }

// captureProvider records the request seen by Chat and replies with a
// fixed content (and optional error).
type captureProvider struct {
	gotReq  ai.ChatRequest
	called  int
	content string
	err     error
}

func (p *captureProvider) Chat(_ context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	p.called++
	p.gotReq = req
	if p.err != nil {
		return ai.ChatResponse{}, p.err
	}
	return ai.ChatResponse{Content: strPtr(p.content)}, nil
}

func findMessage(msgs []ai.Message, role ai.Role) (string, bool) {
	for _, m := range msgs {
		if m.Role == role && m.Content != nil {
			return *m.Content, true
		}
	}
	return "", false
}

func TestRunStdio_PassesStdinAsUserPromptAndWritesResponse(t *testing.T) {
	prov := &captureProvider{content: "response-text"}
	var out bytes.Buffer
	if err := RunStdio(context.Background(), RunStdioOptions{
		Provider: prov,
		Model:    "test-model",
		Role:     "coder",
		Stdin:    strings.NewReader("user-input"),
		Stdout:   &out,
	}); err != nil {
		t.Fatalf("err: %v", err)
	}
	user, ok := findMessage(prov.gotReq.Messages, ai.RoleUser)
	if !ok || user != "user-input" {
		t.Fatalf("user = %q ok=%v", user, ok)
	}
	if !strings.Contains(out.String(), "response-text") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestRunStdio_PassesRolePromptAsSystemMessage(t *testing.T) {
	prov := &captureProvider{content: "ok"}
	var out bytes.Buffer
	if err := RunStdio(context.Background(), RunStdioOptions{
		Provider: prov,
		Model:    "test-model",
		Role:     "planner",
		Stdin:    strings.NewReader("hi"),
		Stdout:   &out,
	}); err != nil {
		t.Fatalf("err: %v", err)
	}
	want, err := GetRolePrompt("planner")
	if err != nil {
		t.Fatalf("get role: %v", err)
	}
	sys, ok := findMessage(prov.gotReq.Messages, ai.RoleSystem)
	if !ok || sys != want {
		t.Fatalf("system = %q, want %q", sys, want)
	}
}

func TestRunStdio_WritesTrailingNewline(t *testing.T) {
	prov := providers.NewFauxProvider([]ai.ChatResponse{{Content: strPtr("abc")}})
	var out bytes.Buffer
	if err := RunStdio(context.Background(), RunStdioOptions{
		Provider: prov,
		Model:    "test-model",
		Role:     "coder",
		Stdin:    strings.NewReader("input"),
		Stdout:   &out,
	}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.String() != "abc\n" {
		t.Fatalf("out = %q", out.String())
	}
}

func TestRunStdio_UnknownRoleErrorsWithoutWriting(t *testing.T) {
	prov := &captureProvider{content: "should-not-be-called"}
	var out bytes.Buffer
	err := RunStdio(context.Background(), RunStdioOptions{
		Provider: prov,
		Model:    "test-model",
		Role:     "nonexistent",
		Stdin:    strings.NewReader("input"),
		Stdout:   &out,
	})
	if err == nil {
		t.Fatalf("want error")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("err = %v", err)
	}
	if prov.called != 0 {
		t.Fatalf("provider called %d times", prov.called)
	}
	if out.Len() != 0 {
		t.Fatalf("wrote to stdout: %q", out.String())
	}
}

func TestRunStdio_SuccessReturnsNil(t *testing.T) {
	prov := providers.NewFauxProvider([]ai.ChatResponse{{Content: strPtr("ok")}})
	var out bytes.Buffer
	if err := RunStdio(context.Background(), RunStdioOptions{
		Provider: prov,
		Model:    "test-model",
		Role:     "coder",
		Stdin:    strings.NewReader("x"),
		Stdout:   &out,
	}); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestRunStdio_PropagatesProviderError(t *testing.T) {
	prov := &captureProvider{err: errors.New("provider-failed-boom")}
	var out bytes.Buffer
	err := RunStdio(context.Background(), RunStdioOptions{
		Provider: prov,
		Model:    "test-model",
		Role:     "coder",
		Stdin:    strings.NewReader("x"),
		Stdout:   &out,
	})
	if err == nil || !strings.Contains(err.Error(), "provider-failed-boom") {
		t.Fatalf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("wrote to stdout on provider error: %q", out.String())
	}
}
