package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"yoli/internal/ai"
)

type cappedOutputTool struct {
	tool     Tool
	maxBytes int
}

func WithOutputCap(t Tool, maxBytes int) Tool {
	return cappedOutputTool{tool: t, maxBytes: maxBytes}
}

func (c cappedOutputTool) Definition() ai.ToolDefinition {
	return c.tool.Definition()
}

func (c cappedOutputTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	out, err := c.tool.Run(ctx, args)
	if err != nil {
		return out, err
	}
	return truncateOutput(out, c.maxBytes), nil
}

func truncateOutput(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	if maxBytes <= 0 {
		return fmt.Sprintf("[truncated: %d bytes elided]", len(s))
	}
	prefixLen := maxBytes
	for {
		if prefixLen < 0 {
			prefixLen = 0
		}
		elided := len(s) - prefixLen
		marker := fmt.Sprintf("\n[truncated: %d bytes elided]", elided)
		if prefixLen == 0 && len(marker) > maxBytes {
			return marker[1:]
		}
		if prefixLen+len(marker) <= maxBytes {
			return s[:prefixLen] + marker
		}
		prefixLen--
	}
}
