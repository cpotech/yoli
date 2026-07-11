package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"yoli/internal/agent"
)

type runFlags struct {
	Role     string
	Provider string
}

func parseRunFlags(args []string) (runFlags, error) {
	var f runFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--role":
			if i+1 >= len(args) {
				return f, fmt.Errorf("--role requires a value")
			}
			f.Role = args[i+1]
			i++
		case strings.HasPrefix(arg, "--role="):
			f.Role = strings.TrimPrefix(arg, "--role=")
		case arg == "--provider":
			if i+1 >= len(args) {
				return f, fmt.Errorf("--provider requires a value")
			}
			f.Provider = args[i+1]
			i++
		case strings.HasPrefix(arg, "--provider="):
			f.Provider = strings.TrimPrefix(arg, "--provider=")
		default:
			return f, fmt.Errorf("Unknown flag for run: %s", arg)
		}
	}
	if f.Role == "" {
		return f, fmt.Errorf(
			"Missing required flag --role <role>. Known roles: %s",
			strings.Join(agent.ListRoles(), ", "),
		)
	}
	return f, nil
}

// runRun implements the `yoli run --role <role>` subcommand: read all
// of stdin as the user prompt, dispatch a single non-streaming chat to
// OpenRouter with the role's system prompt, and write the response to
// stdout.
func runRun(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags, err := parseRunFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	role := flags.Role
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
	profiles, err := LoadProviderProfiles(LoadOptions{
		PathOptions: PathOptionsFromEnv(),
		Warnings:    stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := selectProviderProfile(cfg, profiles, flags.Provider, stderr); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ApplyEnvDefaults(cfg)
	if !requireAPIKey(stderr) {
		return 1
	}
	model := os.Getenv("YOLI_MODEL")
	provider, err := newProviderFromEnv("Yoli")
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
