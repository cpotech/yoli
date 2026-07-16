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

func TestReadConfigFile_SkipsStructuredValues(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.json")
	writeFile(t, p, `{"YOLI_MODEL":"m","providers":{"x":{"base_url":"u"}},"list":[1,2]}`)
	got, err := ReadConfigFile(p)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got["YOLI_MODEL"] != "m" {
		t.Fatalf("got %+v", got)
	}
	if _, ok := got["providers"]; ok {
		t.Fatalf("providers object leaked into flat config: %+v", got)
	}
	if _, ok := got["list"]; ok {
		t.Fatalf("array leaked into flat config: %+v", got)
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

func TestLoadConfig_ProjectBeatsUserBeatsDefault(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".yolirc.json"),
		`{"default_provider":"faux","BRAVE_API_KEY":"project-brave"}`)
	if err := SetConfigValue("default_provider", "openrouter",
		PathOptions{Home: home, XDGConfigHome: ""}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := SetConfigValue("BRAVE_API_KEY", "user-brave",
		PathOptions{Home: home, XDGConfigHome: ""}); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptions{Home: home, XDGConfigHome: ""},
		Cwd:         cwd,
		Warnings:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg["default_provider"] != "faux" {
		t.Fatalf("project should win: %q", cfg["default_provider"])
	}
	if cfg["BRAVE_API_KEY"] != "project-brave" {
		t.Fatalf("project should beat user: %q", cfg["BRAVE_API_KEY"])
	}
}

func TestLoadConfig_IgnoresEnvironment(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("BRAVE_API_KEY", "env-key")
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptions{Home: home, XDGConfigHome: ""},
		Cwd:         cwd,
		Warnings:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := cfg["BRAVE_API_KEY"]; got != "" {
		t.Fatalf("env var must not reach config, got %q", got)
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

func TestLoadConfig_HintsProfileOnlyKeys(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".yolirc.json"),
		`{"YOLI_API_KEY":"stale-key","base_url":"https://stale/v1"}`)
	var warns bytes.Buffer
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptions{Home: home},
		Cwd:         cwd,
		Warnings:    &warns,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cfg) != 0 {
		t.Fatalf("retired keys should be dropped: %+v", cfg)
	}
	w := warns.String()
	if !strings.Contains(w, "YOLI_API_KEY") || !strings.Contains(w, `"api_key"`) {
		t.Fatalf("warnings = %q, want provider-profile hint for YOLI_API_KEY", w)
	}
	if !strings.Contains(w, `"base_url"`) {
		t.Fatalf("warnings = %q, want provider-profile hint for base_url", w)
	}
}

func TestGetEffectiveConfig_AnnotatesSources(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := SetConfigValue("BRAVE_API_KEY", "user-brave",
		PathOptions{Home: home, XDGConfigHome: ""}); err != nil {
		t.Fatalf("set: %v", err)
	}
	writeFile(t, filepath.Join(cwd, ".yolirc.json"), `{"default_provider":"faux"}`)
	t.Setenv("BRAVE_API_KEY", "env-key")
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
	if byKey["BRAVE_API_KEY"].Source != SourceUser || byKey["BRAVE_API_KEY"].Value != "user-brave" {
		t.Fatalf("BRAVE_API_KEY: %+v", byKey["BRAVE_API_KEY"])
	}
	if byKey["default_provider"].Source != SourceProject || byKey["default_provider"].Value != "faux" {
		t.Fatalf("default_provider: %+v", byKey["default_provider"])
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
		"BRAVE_API_KEY",
	}
	if !reflect.DeepEqual(ConfigKeys, want) {
		t.Fatalf("ConfigKeys mismatch:\n got: %v\nwant: %v", ConfigKeys, want)
	}
}

func TestConfigKeys_YoliProviderMigratesToDefaultProvider(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".yolirc.json"), `{"YOLI_PROVIDER":"openrouter"}`)
	var warnings bytes.Buffer
	cfg, err := LoadConfig(LoadOptions{
		PathOptions: PathOptions{Home: home, XDGConfigHome: ""},
		Cwd:         cwd,
		Warnings:    &warnings,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg["default_provider"] != "openrouter" {
		t.Fatalf("YOLI_PROVIDER should migrate to default_provider: %+v", cfg)
	}
	if !strings.Contains(warnings.String(), "renamed") {
		t.Fatalf("expected rename warning, got: %q", warnings.String())
	}
}
