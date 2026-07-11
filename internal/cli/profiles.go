package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// ProviderProfile is one named OpenAI-compatible endpoint definition,
// stored under the "providers" key of a config file. Unset optional
// fields fall back to the flat YOLI_* keys / environment.
type ProviderProfile struct {
	BaseURL       string `json:"base_url"`
	APIKey        string `json:"api_key"`
	Model         string `json:"model,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
	MaxTokens     int    `json:"max_tokens,omitempty"`
}

// ProviderProfiles maps profile name → ProviderProfile.
type ProviderProfiles map[string]ProviderProfile

// ReadProviderProfiles extracts the "providers" object from a JSON
// config file. It parses independently of ReadConfigFile so structured
// data never leaks into the flat string map. A missing file or absent
// "providers" key yields an empty map, not an error.
func ReadProviderProfiles(path string) (ProviderProfiles, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProviderProfiles{}, nil
		}
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, &ConfigParseError{Path: path, Err: err}
	}
	sub, ok := top["providers"]
	if !ok || string(sub) == "null" {
		return ProviderProfiles{}, nil
	}
	out := ProviderProfiles{}
	if err := json.Unmarshal(sub, &out); err != nil {
		return nil, &ConfigParseError{Path: path, Err: err}
	}
	return out, nil
}

// LoadProviderProfiles reads provider profiles from the user config and
// the project .yolirc.json, mirroring LoadConfig's layering: a project
// profile replaces a same-named user profile wholesale.
func LoadProviderProfiles(opts LoadOptions) (ProviderProfiles, error) {
	cwd := opts.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	user, err := ReadProviderProfiles(ConfigPath(opts.PathOptions))
	if err != nil {
		return nil, err
	}
	project, err := ReadProviderProfiles(filepath.Join(cwd, ".yolirc.json"))
	if err != nil {
		return nil, err
	}
	out := ProviderProfiles{}
	for name, p := range user {
		out[name] = p
	}
	for name, p := range project {
		out[name] = p
	}
	return out, nil
}
