package cli

import (
	"fmt"
	"io"
)

const providerUsage = `Usage: yoli provider <subcommand>
Subcommands:
  list         List provider profiles from the "providers" object
`

// runProvider dispatches the `yoli provider <subcommand>` family.
// Returns the desired process exit code.
func runProvider(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, providerUsage)
		return 1
	}
	sub := args[0]
	switch sub {
	case "list":
		return runProviderList(stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Unknown provider subcommand: %s\n%s", sub, providerUsage)
		return 1
	}
}

// runProviderList lists the named provider profiles. API keys are
// deliberately never printed. A `*` marks the profile selected by the
// "default_provider" config key. Profiles are edited by hand in the
// JSON config file; there is no set/remove subcommand.
func runProviderList(stdout, stderr io.Writer) int {
	profiles, err := LoadProviderProfiles(LoadOptions{
		PathOptions: PathOptionsFromEnv(),
		Warnings:    stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(profiles) == 0 {
		fmt.Fprintln(stdout, "(no provider profiles defined)")
		return 0
	}
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptionsFromEnv(),
		Warnings:    stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	active := cfg["default_provider"]
	for _, name := range profileNames(profiles) {
		p := profiles[name]
		model := p.Model
		if model == "" {
			model = "(unset)"
		}
		marker := ""
		if name == active {
			marker = " *"
		}
		fmt.Fprintf(stdout, "%s: base_url=%s model=%s%s\n", name, p.BaseURL, model, marker)
	}
	return 0
}
