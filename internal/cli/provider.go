package cli

import (
	"fmt"
	"io"
	"os"

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
