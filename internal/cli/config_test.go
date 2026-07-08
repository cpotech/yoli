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
	t.Setenv("YOLI_API_KEY", "")
	t.Setenv("YOLI_BASE_URL", "")
	t.Setenv("YOLI_MODEL", "")
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
	if err := SetConfigValue("YOLI_MODEL", "user-model",
		PathOptions{Home: home, XDGConfigHome: ""}); err != nil {
		t.Fatalf("set: %v", err)
	}
	t.Setenv("YOLI_API_KEY", "env-key")
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptions{Home: home, XDGConfigHome: ""},
		Cwd:         cwd,
		Warnings:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg["YOLI_API_KEY"] != "env-key" {
		t.Fatalf("env should win: %q", cfg["YOLI_API_KEY"])
	}
	if cfg["default_provider"] != "faux" {
		t.Fatalf("project should win: %q", cfg["default_provider"])
	}
	if cfg["YOLI_MODEL"] != "project-model" {
		t.Fatalf("project should beat user: %q", cfg["YOLI_MODEL"])
	}
}

func TestApplyEnvDefaults_NeverOverwritesAndSetsMissing(t *testing.T) {
	t.Setenv("YOLI_API_KEY", "shell-key")
	ApplyEnvDefaults(Config{
		"YOLI_API_KEY": "config-key",
	})
	if got := os.Getenv("YOLI_API_KEY"); got != "shell-key" {
		t.Fatalf("YOLI_API_KEY = %q", got)
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

func TestLoadConfig_HintsRenamedOpenRouterAPIKey(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".yolirc.json"),
		`{"openrouter_api_key":"stale-key"}`)
	var warns bytes.Buffer
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptions{Home: home},
		Cwd:         cwd,
		Warnings:    &warns,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
		if _, ok := cfg["openrouter_api_key"]; ok {
		t.Fatalf("retired key should be dropped: %+v", cfg)
	}
	if cfg["YOLI_API_KEY"] != "stale-key" {
		t.Fatalf("value should be auto-migrated to YOLI_API_KEY: %+v", cfg)
	}
	w := warns.String()
	if !strings.Contains(w, "openrouter_api_key") ||
		!strings.Contains(w, "YOLI_API_KEY") {
		t.Fatalf("warnings = %q, want rename hint", w)
	}
}

func TestGetEffectiveConfig_AnnotatesSources(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := SetConfigValue("YOLI_MODEL", "user-model",
		PathOptions{Home: home, XDGConfigHome: ""}); err != nil {
		t.Fatalf("set: %v", err)
	}
	writeFile(t, filepath.Join(cwd, ".yolirc.json"), `{"default_provider":"faux"}`)
	t.Setenv("YOLI_API_KEY", "env-key")
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
	if byKey["YOLI_MODEL"].Source != SourceUser || byKey["YOLI_MODEL"].Value != "user-model" {
		t.Fatalf("YOLI_MODEL: %+v", byKey["YOLI_MODEL"])
	}
	if byKey["default_provider"].Source != SourceProject || byKey["default_provider"].Value != "faux" {
		t.Fatalf("default_provider: %+v", byKey["default_provider"])
	}
	if byKey["YOLI_API_KEY"].Source != SourceEnv || byKey["YOLI_API_KEY"].Value != "env-key" {
		t.Fatalf("YOLI_API_KEY: %+v", byKey["YOLI_API_KEY"])
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
		"YOLI_MODEL",
		"default_role",
		"YOLI_BASE_URL",
		"YOLI_API_KEY",
		"BRAVE_API_KEY",
		"subagent_max_depth",
		"YOLI_CONTEXT_WINDOW",
		"YOLI_MAX_TOKENS",
	}
	if !reflect.DeepEqual(ConfigKeys, want) {
		t.Fatalf("ConfigKeys mismatch:\n got: %v\nwant: %v", ConfigKeys, want)
	}
}

func TestConfigKeys_ContainsBraveAPIKey(t *testing.T) {
	if !IsConfigKey("BRAVE_API_KEY") {
		t.Fatalf("BRAVE_API_KEY should be a known config key")
	}
}

func TestConfigKeys_ContainsContextLimitKeys(t *testing.T) {
	if !IsConfigKey("YOLI_CONTEXT_WINDOW") {
		t.Fatalf("YOLI_CONTEXT_WINDOW should be a known config key")
	}
	if !IsConfigKey("YOLI_MAX_TOKENS") {
		t.Fatalf("YOLI_MAX_TOKENS should be a known config key")
	}
}

func TestSetConfigValue_ContextWindowRoundTrips(t *testing.T) {
	home := t.TempDir()
	opts := PathOptions{Home: home}
	if err := SetConfigValue("YOLI_CONTEXT_WINDOW", "32768", opts); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := ReadConfigFile(ConfigPath(opts))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cfg["YOLI_CONTEXT_WINDOW"] != "32768" {
		t.Fatalf("YOLI_CONTEXT_WINDOW = %q, want 32768", cfg["YOLI_CONTEXT_WINDOW"])
	}
}

func TestGetEffectiveConfig_ContextLimitSources(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("YOLI_CONTEXT_WINDOW", "")
	t.Setenv("YOLI_MAX_TOKENS", "4096")
	writeFile(t, filepath.Join(cwd, ".yolirc.json"),
		`{"YOLI_CONTEXT_WINDOW": "32768"}`)

	entries, err := GetEffectiveConfig(LoadOptions{
		PathOptions: PathOptions{Home: home},
		Cwd:         cwd,
	})
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	byKey := map[string]EffectiveEntry{}
	for _, e := range entries {
		byKey[e.Key] = e
	}
	if byKey["YOLI_CONTEXT_WINDOW"].Source != SourceProject || byKey["YOLI_CONTEXT_WINDOW"].Value != "32768" {
		t.Fatalf("YOLI_CONTEXT_WINDOW: %+v", byKey["YOLI_CONTEXT_WINDOW"])
	}
	if byKey["YOLI_MAX_TOKENS"].Source != SourceEnv || byKey["YOLI_MAX_TOKENS"].Value != "4096" {
		t.Fatalf("YOLI_MAX_TOKENS: %+v", byKey["YOLI_MAX_TOKENS"])
	}
}

func TestApplyEnvDefaults_ExportsBraveAPIKeyWithoutOverwrite(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	ApplyEnvDefaults(Config{"BRAVE_API_KEY": "from-config"})
	if got := os.Getenv("BRAVE_API_KEY"); got != "from-config" {
		t.Fatalf("BRAVE_API_KEY = %q, want from-config", got)
	}

	t.Setenv("BRAVE_API_KEY", "from-shell")
	ApplyEnvDefaults(Config{"BRAVE_API_KEY": "from-config"})
	if got := os.Getenv("BRAVE_API_KEY"); got != "from-shell" {
		t.Fatalf("BRAVE_API_KEY overwritten to %q", got)
	}
}

func TestGetEffectiveConfig_BraveAPIKeyEnvSource(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("YOLI_API_KEY", "")
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
		if e.Key == "BRAVE_API_KEY" {
			found = true
			if e.Source != SourceEnv || e.Value != "env-brave" {
				t.Fatalf("BRAVE_API_KEY: %+v", e)
			}
		}
	}
	if !found {
		t.Fatalf("BRAVE_API_KEY missing from effective config")
	}
}
