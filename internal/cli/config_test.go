package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestConfigPath_XDGOverridesHome(t *testing.T) {
	got := ConfigPath(PathOptions{Home: "/h", XDGConfigHome: "/x"})
	if got != filepath.Join("/x", "yoli", "config.json") {
		t.Fatalf("got %q", got)
	}
}

func TestConfigPath_FallsBackToHomeWhenXDGUnset(t *testing.T) {
	got := ConfigPath(PathOptions{Home: "/h", XDGConfigHome: ""})
	if got != filepath.Join("/h", ".config", "yoli", "config.json") {
		t.Fatalf("got %q", got)
	}
}

func TestReadConfigFile_ParsesObject(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	writeFile(t, p, `{"default_provider":"openrouter","default_model":"gpt"}`)
	got, err := ReadConfigFile(p)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got["default_provider"] != "openrouter" || got["default_model"] != "gpt" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadConfigFile_MalformedReturnsParseError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "broken.json")
	writeFile(t, p, "{ this is not json")
	_, err := ReadConfigFile(p)
	if err == nil {
		t.Fatalf("want error")
	}
	var pe *ConfigParseError
	if !errors.As(err, &pe) {
		t.Fatalf("want *ConfigParseError, got %T", err)
	}
	if !strings.Contains(err.Error(), p) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadConfig_EmptyWhenNothingSet(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "")
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptions{Home: home, XDGConfigHome: ""},
		Cwd:         cwd,
		Warnings:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cfg) != 0 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadConfig_EnvBeatsProjectBeatsUserBeatsDefault(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".yolirc.json"),
		`{"default_provider":"faux","default_model":"project-model"}`)
	if err := SetConfigValue("default_provider", "openrouter",
		PathOptions{Home: home, XDGConfigHome: ""}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetConfigValue("default_model", "user-model",
		PathOptions{Home: home, XDGConfigHome: ""}); err != nil {
		t.Fatalf("set: %v", err)
	}
	t.Setenv("OPENROUTER_API_KEY", "env-key")
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptions{Home: home, XDGConfigHome: ""},
		Cwd:         cwd,
		Warnings:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg["openrouter_api_key"] != "env-key" {
		t.Fatalf("env should win: %q", cfg["openrouter_api_key"])
	}
	if cfg["default_provider"] != "faux" {
		t.Fatalf("project should win: %q", cfg["default_provider"])
	}
	if cfg["default_model"] != "project-model" {
		t.Fatalf("project should beat user: %q", cfg["default_model"])
	}
}

func TestApplyEnvDefaults_NeverOverwritesAndSetsMissing(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "shell-key")
	ApplyEnvDefaults(Config{
		"openrouter_api_key": "config-key",
	})
	if got := os.Getenv("OPENROUTER_API_KEY"); got != "shell-key" {
		t.Fatalf("OPENROUTER_API_KEY = %q", got)
	}
}

func TestSetConfigValue_CreatesDirsWritesIndentedAndTrailingNewline(t *testing.T) {
	home := t.TempDir()
	if err := SetConfigValue("default_provider", "openrouter",
		PathOptions{Home: home, XDGConfigHome: ""}); err != nil {
		t.Fatalf("set: %v", err)
	}
	path := ConfigPath(PathOptions{Home: home})
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasSuffix(string(body), "\n") {
		t.Fatalf("no trailing newline: %q", body)
	}
	if !strings.Contains(string(body), `  "default_provider"`) {
		t.Fatalf("expected 2-space indent: %q", body)
	}
	var parsed map[string]string
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["default_provider"] != "openrouter" {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestSetConfigValue_RejectsUnknownKey(t *testing.T) {
	home := t.TempDir()
	err := SetConfigValue("not_a_key", "x", PathOptions{Home: home})
	if err == nil {
		t.Fatalf("want error")
	}
	if !strings.Contains(err.Error(), "not_a_key") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(ConfigPath(PathOptions{Home: home})); err == nil {
		t.Fatalf("file should not exist on rejected key")
	}
}

func TestLoadConfig_WarnsOnUnknownKeys(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".yolirc.json"),
		`{"default_provider":"faux","mystery_key":"x","another_bogus":"y"}`)
	var warns bytes.Buffer
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptions{Home: home},
		Cwd:         cwd,
		Warnings:    &warns,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg["default_provider"] != "faux" {
		t.Fatalf("provider = %q", cfg["default_provider"])
	}
	w := warns.String()
	if !strings.Contains(w, "mystery_key") || !strings.Contains(w, "another_bogus") {
		t.Fatalf("warnings = %q", w)
	}
}

func TestGetEffectiveConfig_AnnotatesSources(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := SetConfigValue("default_model", "user-model",
		PathOptions{Home: home, XDGConfigHome: ""}); err != nil {
		t.Fatalf("set: %v", err)
	}
	writeFile(t, filepath.Join(cwd, ".yolirc.json"), `{"default_provider":"faux"}`)
	t.Setenv("OPENROUTER_API_KEY", "env-key")
	entries, err := GetEffectiveConfig(LoadOptions{
		PathOptions: PathOptions{Home: home, XDGConfigHome: ""},
		Cwd:         cwd,
		Warnings:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	byKey := map[string]EffectiveEntry{}
	for _, e := range entries {
		byKey[e.Key] = e
	}
	if byKey["default_model"].Source != SourceUser || byKey["default_model"].Value != "user-model" {
		t.Fatalf("default_model: %+v", byKey["default_model"])
	}
	if byKey["default_provider"].Source != SourceProject || byKey["default_provider"].Value != "faux" {
		t.Fatalf("default_provider: %+v", byKey["default_provider"])
	}
	if byKey["openrouter_api_key"].Source != SourceEnv || byKey["openrouter_api_key"].Value != "env-key" {
		t.Fatalf("openrouter_api_key: %+v", byKey["openrouter_api_key"])
	}
}

func TestIsConfigKey(t *testing.T) {
	if !IsConfigKey("default_provider") {
		t.Fatalf("known key rejected")
	}
	if IsConfigKey("nope") {
		t.Fatalf("unknown key accepted")
	}
}

func TestConfigKeys_OnlyContainsExpectedKeys(t *testing.T) {
	want := []string{
		"default_provider",
		"default_model",
		"default_role",
		"openrouter_api_key",
		"brave_api_key",
		"subagent_max_depth",
	}
	if !reflect.DeepEqual(ConfigKeys, want) {
		t.Fatalf("ConfigKeys mismatch:\n got: %v\nwant: %v", ConfigKeys, want)
	}
}

func TestEnvBindings_OnlyBindsExpectedKeys(t *testing.T) {
	want := map[string]string{
		"openrouter_api_key": "OPENROUTER_API_KEY",
		"brave_api_key":      "BRAVE_API_KEY",
		"default_model":      "OPENROUTER_MODEL",
	}
	if !reflect.DeepEqual(envBindings, want) {
		t.Fatalf("envBindings mismatch:\n got: %v\nwant: %v", envBindings, want)
	}
}

func TestConfigKeys_ContainsBraveAPIKey(t *testing.T) {
	if !IsConfigKey("brave_api_key") {
		t.Fatalf("brave_api_key should be a known config key")
	}
}

func TestEnvBindings_BraveAPIKeyMapsToBraveEnv(t *testing.T) {
	got, ok := envBindings["brave_api_key"]
	if !ok {
		t.Fatalf("brave_api_key not present in envBindings")
	}
	if got != "BRAVE_API_KEY" {
		t.Fatalf("brave_api_key mapped to %q, want BRAVE_API_KEY", got)
	}
}

func TestApplyEnvDefaults_ExportsBraveAPIKeyWithoutOverwrite(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	ApplyEnvDefaults(Config{"brave_api_key": "from-config"})
	if got := os.Getenv("BRAVE_API_KEY"); got != "from-config" {
		t.Fatalf("BRAVE_API_KEY = %q, want from-config", got)
	}

	t.Setenv("BRAVE_API_KEY", "from-shell")
	ApplyEnvDefaults(Config{"brave_api_key": "from-config"})
	if got := os.Getenv("BRAVE_API_KEY"); got != "from-shell" {
		t.Fatalf("BRAVE_API_KEY overwritten to %q", got)
	}
}

func TestGetEffectiveConfig_BraveAPIKeyEnvSource(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("BRAVE_API_KEY", "env-brave")
	entries, err := GetEffectiveConfig(LoadOptions{
		PathOptions: PathOptions{Home: home, XDGConfigHome: ""},
		Cwd:         cwd,
		Warnings:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Key == "brave_api_key" {
			found = true
			if e.Source != SourceEnv || e.Value != "env-brave" {
				t.Fatalf("brave_api_key: %+v", e)
			}
		}
	}
	if !found {
		t.Fatalf("brave_api_key missing from effective config")
	}
}
