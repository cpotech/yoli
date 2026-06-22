// Package cli implements the yoli command-line interface: subcommand
// dispatch, configuration file management, and wiring between agent
// runners and providers.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ConfigKeys is the canonical list of known configuration keys, in the
// order they should appear in `yoli config list` output.
var ConfigKeys = []string{
	"default_provider",
	"default_model",
	"default_role",
	"openrouter_api_key",
	"brave_api_key",
	"subagent_max_depth",
}

// envBindings maps a config key to the env var it can populate via
// ApplyEnvDefaults. Keys missing from the map are file-only.
var envBindings = map[string]string{
	"openrouter_api_key": "OPENROUTER_API_KEY",
	"brave_api_key":      "BRAVE_API_KEY",
	"default_model":      "OPENROUTER_MODEL",
}

// Config is the merged, in-memory form of a yoli configuration. A
// missing key means "unset"; an empty value means "set to empty string".
type Config map[string]string

// ConfigSource labels where an effective config value came from.
type ConfigSource string

const (
	SourceEnv     ConfigSource = "env"
	SourceProject ConfigSource = "project"
	SourceUser    ConfigSource = "user"
	SourceDefault ConfigSource = "default"
)

// EffectiveEntry is one row of `yoli config list` output.
type EffectiveEntry struct {
	Key    string
	Value  string // empty when unset
	Source ConfigSource
}

// ConfigParseError is returned by ReadConfigFile when JSON parsing
// fails. Its Error() embeds the offending file path.
type ConfigParseError struct {
	Path string
	Err  error
}

func (e *ConfigParseError) Error() string {
	return fmt.Sprintf("Failed to parse config file at %s: %s", e.Path, e.Err.Error())
}

func (e *ConfigParseError) Unwrap() error { return e.Err }

// PathOptions selects which directories yoli inspects for the user
// config file. Both fields are interpreted literally; an empty value
// means "not set". Use PathOptionsFromEnv to derive these from the
// process environment.
type PathOptions struct {
	Home          string
	XDGConfigHome string
}

// PathOptionsFromEnv reads HOME and XDG_CONFIG_HOME from the current
// process environment.
func PathOptionsFromEnv() PathOptions {
	return PathOptions{
		Home:          os.Getenv("HOME"),
		XDGConfigHome: os.Getenv("XDG_CONFIG_HOME"),
	}
}

// ConfigPath returns the location of the user-level config file given
// the supplied path options.
func ConfigPath(opts PathOptions) string {
	if opts.XDGConfigHome != "" {
		return filepath.Join(opts.XDGConfigHome, "yoli", "config.json")
	}
	return filepath.Join(opts.Home, ".config", "yoli", "config.json")
}

// LoadOptions configures LoadConfig and GetEffectiveConfig.
type LoadOptions struct {
	PathOptions
	// Cwd is the directory whose .yolirc.json is the "project"
	// override. Empty means os.Getwd().
	Cwd string
	// Warnings receives "ignoring unknown config keys" messages. Nil
	// means os.Stderr.
	Warnings io.Writer
}

// IsConfigKey reports whether s is a known configuration key.
func IsConfigKey(s string) bool {
	for _, k := range ConfigKeys {
		if k == s {
			return true
		}
	}
	return false
}

// ReadConfigFile loads a single JSON config file from disk. Returns a
// *ConfigParseError on malformed JSON or non-object root.
func ReadConfigFile(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, &ConfigParseError{Path: path, Err: err}
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return nil, &ConfigParseError{Path: path, Err: errors.New("expected a JSON object")}
	}
	out := Config{}
	for k, v := range obj {
		switch val := v.(type) {
		case string:
			out[k] = val
		case nil:
			// skip explicit nulls
		default:
			out[k] = fmt.Sprint(val)
		}
	}
	return out, nil
}

// readSafely returns an empty config when the file is missing; reports
// errors for everything else.
func readSafely(path string) (Config, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return nil, err
	}
	return ReadConfigFile(path)
}

func filterKnown(in Config, sourceLabel string, warnings io.Writer) Config {
	known := Config{}
	var unknown []string
	for k, v := range in {
		if IsConfigKey(k) {
			known[k] = v
		} else {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		if warnings == nil {
			warnings = os.Stderr
		}
		fmt.Fprintf(warnings, "warning: ignoring unknown config keys in %s: %s\n",
			sourceLabel, joinComma(unknown))
	}
	return known
}

func joinComma(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

func readFromEnv() Config {
	out := Config{}
	for _, key := range ConfigKeys {
		envName, ok := envBindings[key]
		if !ok {
			continue
		}
		if v := os.Getenv(envName); v != "" {
			out[key] = v
		}
	}
	return out
}

// LoadConfig merges all configured sources with precedence env >
// project (<cwd>/.yolirc.json) > user (~/.config/yoli/config.json) >
// defaults. Unknown keys in either file generate a warning but do not
// fail the load.
func LoadConfig(opts LoadOptions) (Config, error) {
	cwd := opts.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	userPath := ConfigPath(opts.PathOptions)
	userRaw, err := readSafely(userPath)
	if err != nil {
		return nil, err
	}
	userCfg := filterKnown(userRaw, userPath, opts.Warnings)

	projectPath := filepath.Join(cwd, ".yolirc.json")
	projectRaw, err := readSafely(projectPath)
	if err != nil {
		return nil, err
	}
	projectCfg := filterKnown(projectRaw, projectPath, opts.Warnings)

	envCfg := readFromEnv()

	out := Config{}
	for k, v := range userCfg {
		out[k] = v
	}
	for k, v := range projectCfg {
		out[k] = v
	}
	for k, v := range envCfg {
		out[k] = v
	}
	return out, nil
}

// GetEffectiveConfig returns one EffectiveEntry per known key, in the
// order defined by ConfigKeys, annotated with which source provided
// the value (env|project|user|default).
func GetEffectiveConfig(opts LoadOptions) ([]EffectiveEntry, error) {
	cwd := opts.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	userPath := ConfigPath(opts.PathOptions)
	userRaw, err := readSafely(userPath)
	if err != nil {
		return nil, err
	}
	userCfg := filterKnown(userRaw, userPath, opts.Warnings)

	projectPath := filepath.Join(cwd, ".yolirc.json")
	projectRaw, err := readSafely(projectPath)
	if err != nil {
		return nil, err
	}
	projectCfg := filterKnown(projectRaw, projectPath, opts.Warnings)

	envCfg := readFromEnv()

	out := make([]EffectiveEntry, 0, len(ConfigKeys))
	for _, key := range ConfigKeys {
		if v, ok := envCfg[key]; ok {
			out = append(out, EffectiveEntry{Key: key, Value: v, Source: SourceEnv})
			continue
		}
		if v, ok := projectCfg[key]; ok {
			out = append(out, EffectiveEntry{Key: key, Value: v, Source: SourceProject})
			continue
		}
		if v, ok := userCfg[key]; ok {
			out = append(out, EffectiveEntry{Key: key, Value: v, Source: SourceUser})
			continue
		}
		out = append(out, EffectiveEntry{Key: key, Value: "", Source: SourceDefault})
	}
	return out, nil
}

// ApplyEnvDefaults exports config values to the process environment
// for keys with an env binding, without ever overwriting a value
// already set in the environment.
func ApplyEnvDefaults(cfg Config) {
	for key, envName := range envBindings {
		if os.Getenv(envName) != "" {
			continue
		}
		if v, ok := cfg[key]; ok {
			_ = os.Setenv(envName, v)
		}
	}
}

// SetConfigValue persists key=value to the user-level config file,
// creating intermediate directories as needed. Returns an error for
// unknown keys and never creates the file in that case.
func SetConfigValue(key, value string, opts PathOptions) error {
	if !IsConfigKey(key) {
		return fmt.Errorf("Unknown config key: %s", key)
	}
	path := ConfigPath(opts)
	existing := Config{}
	if _, err := os.Stat(path); err == nil {
		loaded, err := ReadConfigFile(path)
		if err != nil {
			return err
		}
		existing = loaded
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	existing[key] = value
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}
