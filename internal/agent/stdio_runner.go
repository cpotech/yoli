package agent

import (
	"context"
	"io"

	"yoli/internal/ai"
)

// RunStdioOptions configures RunStdio.
type RunStdioOptions struct {
	Provider ai.Provider
	Model    string
	Role     string
	Stdin    io.Reader
	Stdout   io.Writer
}

// RunStdio reads all of opts.Stdin as the user prompt, dispatches a
// single non-streaming chat call to opts.Provider with the role prompt as
// the system message, and writes the response content followed by a
// trailing newline to opts.Stdout.
//
// If the role is unknown, RunStdio returns the error from GetRolePrompt
// without touching the streams. Provider errors are returned as-is.
func RunStdio(ctx context.Context, opts RunStdioOptions) error {
	system, err := GetRolePrompt(opts.Role)
	if err != nil {
		return err
	}
	userBytes, err := io.ReadAll(opts.Stdin)
	if err != nil {
		return err
	}
	user := string(userBytes)
	resp, err := opts.Provider.Chat(ctx, ai.ChatRequest{
		Model: opts.Model,
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: &system},
			{Role: ai.RoleUser, Content: &user},
		},
	})
	if err != nil {
		return err
	}
	content := ""
	if resp.Content != nil {
		content = *resp.Content
	}
	if _, err := opts.Stdout.Write([]byte(content + "\n")); err != nil {
		return err
	}
	return nil
}
