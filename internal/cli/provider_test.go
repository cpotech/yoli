package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// clearProviderEnv blanks every env var the profile machinery reads or
// writes so tests are hermetic. t.Setenv registers restoration.
func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"YOLI_PROVIDER", "YOLI_API_KEY", "YOLI_BASE_URL",
		"YOLI_MODEL", "YOLI_CONTEXT_WINDOW", "YOLI_MAX_TOKENS",
	} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
}

func TestResolveProviderName_FlagBeatsEnvBeatsConfig(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("YOLI_PROVIDER", "from-env")
	cfg := Config{"YOLI_PROVIDER": "from-file", "default_provider": "from-default"}
	if name, explicit := resolveProviderName(cfg, "from-flag"); name != "from-flag" || !explicit {
		t.Fatalf("flag should win: %q explicit=%v", name, explicit)
	}
	if name, explicit := resolveProviderName(cfg, ""); name != "from-env" || !explicit {
		t.Fatalf("env should beat config: %q explicit=%v", name, explicit)
	}
}

func TestResolveProviderName_FileKeysAreSoft(t *testing.T) {
	clearProviderEnv(t)
	cfg := Config{"YOLI_PROVIDER": "from-file", "default_provider": "from-default"}
	if name, explicit := resolveProviderName(cfg, ""); name != "from-file" || explicit {
		t.Fatalf("file YOLI_PROVIDER should win softly: %q explicit=%v", name, explicit)
	}
	if name, explicit := resolveProviderName(Config{"default_provider": "d"}, ""); name != "d" || explicit {
		t.Fatalf("default_provider fallback: %q explicit=%v", name, explicit)
	}
	if name, _ := resolveProviderName(Config{}, ""); name != "" {
		t.Fatalf("nothing set should be implicit mode: %q", name)
	}
}

func TestApplyProfileEnvDefaults_SetsMissingAndKeepsExisting(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("YOLI_API_KEY", "shell-key")
	applyProfileEnvDefaults(ProviderProfile{
		BaseURL: "https://p/v1", APIKey: "profile-key", Model: "pm",
		ContextWindow: 1234, MaxTokens: 55,
	})
	if got := os.Getenv("YOLI_API_KEY"); got != "shell-key" {
		t.Fatalf("shell env should win: %q", got)
	}
	if got := os.Getenv("YOLI_BASE_URL"); got != "https://p/v1" {
		t.Fatalf("base_url not exported: %q", got)
	}
	if got := os.Getenv("YOLI_MODEL"); got != "pm" {
		t.Fatalf("model not exported: %q", got)
	}
	if got := os.Getenv("YOLI_CONTEXT_WINDOW"); got != "1234" {
		t.Fatalf("context window not exported: %q", got)
	}
	if got := os.Getenv("YOLI_MAX_TOKENS"); got != "55" {
		t.Fatalf("max tokens not exported: %q", got)
	}
}

func TestSelectProviderProfile_AppliesProfileAndReturnsName(t *testing.T) {
	clearProviderEnv(t)
	profiles := ProviderProfiles{"runpod": {BaseURL: "https://pod/v1", APIKey: "k", Model: "m"}}
	var warn bytes.Buffer
	name, err := selectProviderProfile(Config{}, profiles, "runpod", &warn)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if name != "runpod" {
		t.Fatalf("name = %q", name)
	}
	if os.Getenv("YOLI_BASE_URL") != "https://pod/v1" || os.Getenv("YOLI_API_KEY") != "k" {
		t.Fatalf("profile fields not exported")
	}
}

func TestSelectProviderProfile_ExplicitUnknownNameErrorsWithList(t *testing.T) {
	clearProviderEnv(t)
	profiles := ProviderProfiles{
		"beta":  {BaseURL: "https://b/v1"},
		"alpha": {BaseURL: "https://a/v1"},
	}
	_, err := selectProviderProfile(Config{}, profiles, "nope", &bytes.Buffer{})
	if err == nil {
		t.Fatalf("want error")
	}
	if !strings.Contains(err.Error(), "alpha, beta") {
		t.Fatalf("error should list sorted profiles: %v", err)
	}
}

func TestSelectProviderProfile_ExplicitNameWithNoProfilesErrors(t *testing.T) {
	clearProviderEnv(t)
	_, err := selectProviderProfile(Config{}, ProviderProfiles{}, "nope", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no profiles defined") {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectProviderProfile_DefaultUnknownNameWarnsAndFallsBack(t *testing.T) {
	clearProviderEnv(t)
	var warn bytes.Buffer
	name, err := selectProviderProfile(
		Config{"default_provider": "gone"}, ProviderProfiles{}, "", &warn)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if name != "" {
		t.Fatalf("name = %q", name)
	}
	if !strings.Contains(warn.String(), "gone") {
		t.Fatalf("warning missing: %q", warn.String())
	}
}

func TestSelectProviderProfile_EmptySelectionIsNoOp(t *testing.T) {
	clearProviderEnv(t)
	name, err := selectProviderProfile(Config{}, ProviderProfiles{}, "", &bytes.Buffer{})
	if err != nil || name != "" {
		t.Fatalf("name=%q err=%v", name, err)
	}
	if os.Getenv("YOLI_BASE_URL") != "" {
		t.Fatalf("env should be untouched")
	}
}

func TestNewProviderFromProfile_FallsBackToEnvForUnsetFields(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("YOLI_API_KEY", "env-key")
	p, err := newProviderFromProfile(ProviderProfile{BaseURL: "https://pod/v1"}, "T")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p == nil {
		t.Fatalf("nil provider")
	}
}

func TestNewProviderFromProfile_MissingEverythingErrors(t *testing.T) {
	clearProviderEnv(t)
	if _, err := newProviderFromProfile(ProviderProfile{}, "T"); err == nil {
		t.Fatalf("want error for empty profile with empty env")
	}
}
