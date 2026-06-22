package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"yoli/internal/agent"
	"yoli/internal/ai/providers"
)

func parseRunFlags(args []string) (string, error) {
	var role string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--role":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--role requires a value")
			}
			role = args[i+1]
			i++
		case strings.HasPrefix(arg, "--role="):
			role = strings.TrimPrefix(arg, "--role=")
		default:
			return "", fmt.Errorf("Unknown flag for run: %s", arg)
		}
	}
	if role == "" {
		return "", fmt.Errorf(
			"Missing required flag --role <role>. Known roles: %s",
			strings.Join(agent.ListRoles(), ", "),
		)
	}
	return role, nil
}

// runRun implements the `yoli run --role <role>` subcommand: read all
// of stdin as the user prompt, dispatch a single non-streaming chat to
// OpenRouter with the role's system prompt, and write the response to
// stdout.
func runRun(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	role, err := parseRunFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := agent.GetRolePrompt(role); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptionsFromEnv(),
		Warnings:    stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ApplyEnvDefaults(cfg)
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		fmt.Fprint(stderr, "Error: OPENROUTER_API_KEY is not set\n")
		return 1
	}
	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		model = defaultModel
	}
	provider, err := providers.NewOpenRouterProvider(providers.OpenRouterOptions{
		APIKey:  os.Getenv("OPENROUTER_API_KEY"),
		Referer: "https://github.com/yolium/yoli",
		Title:   "Yoli",
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := agent.RunStdio(context.Background(), agent.RunStdioOptions{
		Provider: provider,
		Model:    model,
		Role:     role,
		Stdin:    stdin,
		Stdout:   stdout,
	}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
