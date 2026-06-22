package cli

import (
	"fmt"
	"io"
)

const configUsage = `Usage: yoli config <subcommand>
Subcommands:
  path                       Print the user config file path
  get <key>                  Print the effective value for <key>
  set <key> <value>          Persist <key>=<value> to the user config file
  list                       Print every known key with its value and source
`

// runConfig dispatches the `yoli config <subcommand>` family. Returns
// the desired process exit code.
func runConfig(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, configUsage)
		return 1
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "path":
		fmt.Fprintln(stdout, ConfigPath(PathOptionsFromEnv()))
		return 0
	case "get":
		return runConfigGet(rest, stdout, stderr)
	case "set":
		return runConfigSet(rest, stdout, stderr)
	case "list":
		return runConfigList(stdout, stderr)
	default:
		fmt.Fprint(stderr, configUsage)
		return 1
	}
}

func runConfigGet(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, "Usage: yoli config get <key>\n")
		return 1
	}
	key := args[0]
	if !IsConfigKey(key) {
		fmt.Fprintf(stderr, "Unknown config key: %s\n", key)
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
	if v, ok := cfg[key]; ok && v != "" {
		fmt.Fprintln(stdout, v)
	}
	return 0
}

func runConfigSet(args []string, stdout, stderr io.Writer) int {
	_ = stdout
	if len(args) < 2 {
		fmt.Fprint(stderr, "Usage: yoli config set <key> <value>\n")
		return 1
	}
	key, value := args[0], args[1]
	if !IsConfigKey(key) {
		fmt.Fprintf(stderr, "Unknown config key: %s\n", key)
		return 1
	}
	if err := SetConfigValue(key, value, PathOptionsFromEnv()); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runConfigList(stdout, stderr io.Writer) int {
	entries, err := GetEffectiveConfig(LoadOptions{
		PathOptions: PathOptionsFromEnv(),
		Warnings:    stderr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, e := range entries {
		fmt.Fprintf(stdout, "%s = %s (%s)\n", e.Key, e.Value, e.Source)
	}
	return 0
}
