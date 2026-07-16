package cli

import (
	"fmt"
	"sort"
	"strings"

	"yoli/internal/agent"
	"yoli/internal/ai/providers"
)

// selectProviderProfile resolves the active provider profile with
// precedence --provider flag > default_provider config key. A profile is
// the only way to configure an endpoint, so failing to resolve one is an
// error.
func selectProviderProfile(cfg Config, profiles ProviderProfiles, flagProvider string) (ProviderProfile, string, error) {
	name := flagProvider
	if name == "" {
		name = cfg["default_provider"]
	}
	if len(profiles) == 0 {
		return ProviderProfile{}, "", fmt.Errorf(
			"no provider profiles defined — add a \"providers\" object to %s",
			ConfigPath(PathOptionsFromEnv()))
	}
	if name == "" {
		return ProviderProfile{}, "", fmt.Errorf(
			"no provider selected — set \"default_provider\" in %s or pass --provider (available: %s)",
			ConfigPath(PathOptionsFromEnv()), strings.Join(profileNames(profiles), ", "))
	}
	p, ok := profiles[name]
	if !ok {
		return ProviderProfile{}, "", fmt.Errorf(
			"unknown provider profile %q (available: %s)",
			name, strings.Join(profileNames(profiles), ", "))
	}
	return p, name, nil
}

// newProviderFromProfile builds the OpenAI-compatible client from a
// profile. Missing base_url or api_key surface as constructor errors.
func newProviderFromProfile(p ProviderProfile, title string) (*providers.OpenAICompatProvider, error) {
	return providers.NewOpenAICompatProvider(providers.OpenAICompatOptions{
		APIKey:  p.APIKey,
		BaseURL: p.BaseURL,
		Referer: "https://github.com/yolium/yoli",
		Title:   title,
	})
}

// contextLimits returns the profile's total context window and per-turn
// output cap, falling back to the agent defaults when unset.
func contextLimits(p ProviderProfile) (window, maxTokens int) {
	window = p.ContextWindow
	if window <= 0 {
		window = agent.DefaultContextBudget
	}
	maxTokens = p.MaxTokens
	if maxTokens <= 0 {
		maxTokens = agent.DefaultMaxOutputTokens
	}
	return window, maxTokens
}

func profileNames(profiles ProviderProfiles) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
