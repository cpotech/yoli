package cli

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestReadProviderProfiles_MissingFileYieldsEmptyMap(t *testing.T) {
	got, err := ReadProviderProfiles(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestReadProviderProfiles_NoProvidersKeyYieldsEmptyMap(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	writeFile(t, p, `{"YOLI_MODEL":"m","YOLI_API_KEY":"k"}`)
	got, err := ReadProviderProfiles(p)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestReadProviderProfiles_ParsesAllFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	writeFile(t, p, `{"providers":{"runpod":{
		"base_url":"https://pod/v1","api_key":"k1","model":"m1",
		"context_window":57344,"max_tokens":8192}}}`)
	got, err := ReadProviderProfiles(p)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := ProviderProfile{
		BaseURL: "https://pod/v1", APIKey: "k1", Model: "m1",
		ContextWindow: 57344, MaxTokens: 8192,
	}
	if got["runpod"] != want {
		t.Fatalf("got %+v want %+v", got["runpod"], want)
	}
}

func TestReadProviderProfiles_OptionalFieldsDefaultToZero(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	writeFile(t, p, `{"providers":{"min":{"base_url":"https://x/v1","api_key":"k"}}}`)
	got, err := ReadProviderProfiles(p)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	prof := got["min"]
	if prof.Model != "" || prof.ContextWindow != 0 || prof.MaxTokens != 0 {
		t.Fatalf("got %+v", prof)
	}
}

func TestReadProviderProfiles_MalformedJSONReturnsParseError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	writeFile(t, p, `{"providers":{"bad": []}}`)
	_, err := ReadProviderProfiles(p)
	var pe *ConfigParseError
	if !errors.As(err, &pe) {
		t.Fatalf("want *ConfigParseError, got %v", err)
	}
}

func TestReadProviderProfiles_NullProvidersYieldsEmptyMap(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfg.json")
	writeFile(t, p, `{"providers":null}`)
	got, err := ReadProviderProfiles(p)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadProviderProfiles_ProjectBeatsUserAndMergesRest(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "yoli", "config.json"),
		`{"providers":{
			"shared":{"base_url":"https://user/v1","api_key":"uk"},
			"user-only":{"base_url":"https://user-only/v1","api_key":"uk2"}}}`)
	writeFile(t, filepath.Join(cwd, ".yolirc.json"),
		`{"providers":{"shared":{"base_url":"https://project/v1","api_key":"pk"}}}`)
	got, err := LoadProviderProfiles(LoadOptions{
		PathOptions: PathOptions{Home: home, XDGConfigHome: ""},
		Cwd:         cwd,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got["shared"].BaseURL != "https://project/v1" {
		t.Fatalf("project should win: %+v", got["shared"])
	}
	if got["user-only"].BaseURL != "https://user-only/v1" {
		t.Fatalf("user-only profile lost: %+v", got)
	}
}
