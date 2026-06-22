package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"yoli/internal/agent"
	"yoli/internal/agent/tools"
	"yoli/internal/ai"
)

type capTool struct {
	def ai.ToolDefinition
	run func(context.Context, json.RawMessage) (string, error)
}

func (c capTool) Definition() ai.ToolDefinition { return c.def }
func (c capTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return c.run(ctx, args)
}

func TestWithOutputCap_TruncatesOversizedOutput(t *testing.T) {
	wrapped := tools.WithOutputCap(capTool{
		def: ai.ToolDefinition{Name: "big"},
		run: func(context.Context, json.RawMessage) (string, error) {
			return strings.Repeat("x", 200*1024), nil
		},
	}, agent.DefaultToolOutputBytes)

	got, err := wrapped.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) > agent.DefaultToolOutputBytes+128 {
		t.Fatalf("len(got) = %d, want about cap %d", len(got), agent.DefaultToolOutputBytes)
	}
	if !strings.Contains(got, "[truncated: ") {
		t.Fatalf("missing truncation marker")
	}
}

func TestWithOutputCap_PassesThroughSmallOutput(t *testing.T) {
	wrapped := tools.WithOutputCap(capTool{
		def: ai.ToolDefinition{Name: "small"},
		run: func(context.Context, json.RawMessage) (string, error) {
			return "small output", nil
		},
	}, agent.DefaultToolOutputBytes)

	got, err := wrapped.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "small output" {
		t.Fatalf("output = %q", got)
	}
}

func TestWithOutputCap_PreservesErrors(t *testing.T) {
	wantErr := errors.New("boom")
	wrapped := tools.WithOutputCap(capTool{
		def: ai.ToolDefinition{Name: "err"},
		run: func(context.Context, json.RawMessage) (string, error) {
			return strings.Repeat("x", 200*1024), wantErr
		},
	}, agent.DefaultToolOutputBytes)

	got, err := wrapped.Run(context.Background(), nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if len(got) != 200*1024 {
		t.Fatalf("output was rewritten")
	}
}

func TestWithOutputCap_DelegatesDefinition(t *testing.T) {
	def := ai.ToolDefinition{
		Name:        "same",
		Description: "same def",
		Parameters:  map[string]any{"type": "object"},
	}
	wrapped := tools.WithOutputCap(capTool{def: def}, agent.DefaultToolOutputBytes)
	got := wrapped.Definition()
	if got.Name != def.Name || got.Description != def.Description {
		t.Fatalf("Definition = %+v, want %+v", got, def)
	}
}
