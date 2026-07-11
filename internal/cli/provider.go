package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"yoli/internal/ai/providers"
)

// newProviderFromEnv constructs the OpenAI-compatible provider from the
// process environment: YOLI_API_KEY for auth and YOLI_BASE_URL for the
// endpoint. Callers run ApplyEnvDefaults first so stored config reaches
// the environment.
func newProviderFromEnv(title string) (*providers.OpenAICompatProvider, error) {
	return providers.NewOpenAICompatProvider(providers.OpenAICompatOptions{
		APIKey:  os.Getenv("YOLI_API_KEY"),
		BaseURL: os.Getenv("YOLI_BASE_URL"),
		Referer: "https://github.com/yolium/yoli",
		Title:   title,
	})
}

// resolveProviderName picks the active profile name with precedence
// --provider flag > YOLI_PROVIDER env > YOLI_PROVIDER/default_provider
// from config files. explicit is true for the flag and env forms, which
// must name an existing profile; file-level defaults degrade gracefully.
// An empty name means implicit mode: flat YOLI_* keys as today.
func resolveProviderName(cfg Config, flagProvider string) (name string, explicit bool) {
	if flagProvider != "" {
		return flagProvider, true
	}
	if v := os.Getenv("YOLI_PROVIDER"); v != "" {
		return v, true
	}
	if v := cfg["YOLI_PROVIDER"]; v != "" {
		return v, false
	}
	return cfg["default_provider"], false
}

// selectProviderProfile resolves and activates a provider profile for
// this invocation: it exports the profile's fields to the environment
// (set-if-empty, so shell env still wins) and returns the name in
// effect. Callers must invoke it before ApplyEnvDefaults so profile
// fields outrank flat config keys. Unknown names are an error when
// explicitly selected, a warning plus flat-config fallback otherwise.
func selectProviderProfile(cfg Config, profiles ProviderProfiles, flagProvider string, warnings io.Writer) (string, error) {
	name, explicit := resolveProviderName(cfg, flagProvider)
	if name == "" {
		return "", nil
	}
	p, ok := profiles[name]
	if !ok {
		if explicit {
			if len(profiles) == 0 {
				return "", fmt.Errorf("unknown provider profile %q — no profiles defined under \"providers\" in %s", name, ConfigPath(PathOptionsFromEnv()))
			}
			return "", fmt.Errorf("unknown provider profile %q (available: %s)", name, strings.Join(profileNames(profiles), ", "))
		}
		fmt.Fprintf(warnings, "warning: default provider profile %q not found — falling back to flat YOLI_* config\n", name)
		return "", nil
	}
	applyProfileEnvDefaults(p)
	return name, nil
}

// applyProfileEnvDefaults exports profile fields to the process
// environment without overwriting values already set, mirroring
// ApplyEnvDefaults' env-always-wins rule.
func applyProfileEnvDefaults(p ProviderProfile) {
	setenvIfEmpty("YOLI_API_KEY", p.APIKey)
	setenvIfEmpty("YOLI_BASE_URL", p.BaseURL)
	setenvIfEmpty("YOLI_MODEL", p.Model)
	if p.ContextWindow > 0 {
		setenvIfEmpty("YOLI_CONTEXT_WINDOW", strconv.Itoa(p.ContextWindow))
	}
	if p.MaxTokens > 0 {
		setenvIfEmpty("YOLI_MAX_TOKENS", strconv.Itoa(p.MaxTokens))
	}
}

func setenvIfEmpty(key, val string) {
	if val != "" && os.Getenv(key) == "" {
		_ = os.Setenv(key, val)
	}
}

func profileNames(profiles ProviderProfiles) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// newProviderFromProfile builds a client directly from a profile
// struct, falling back to the environment for unset fields. The TUI's
// /provider switch uses this instead of the env path: after startup,
// ApplyEnvDefaults has already populated the env, so a set-if-empty
// pass could never change endpoints mid-session.
func newProviderFromProfile(p ProviderProfile, title string) (*providers.OpenAICompatProvider, error) {
	apiKey := p.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("YOLI_API_KEY")
	}
	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("YOLI_BASE_URL")
	}
	return providers.NewOpenAICompatProvider(providers.OpenAICompatOptions{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Referer: "https://github.com/yolium/yoli",
		Title:   title,
	})
}

// requireAPIKey reports whether YOLI_API_KEY is set, printing an error —
// and a migration hint when the retired OPENROUTER_API_KEY is still
// exported — when it is not.
func requireAPIKey(stderr io.Writer) bool {
	if os.Getenv("YOLI_API_KEY") != "" {
		return true
	}
	fmt.Fprint(stderr, "Error: YOLI_API_KEY is not set\n")
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		fmt.Fprint(stderr,
			"Note: OPENROUTER_API_KEY is no longer read — set YOLI_API_KEY or run `yoli config set YOLI_API_KEY <value>`\n")
	}
	return false
}
