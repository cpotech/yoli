package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yoli/internal/agent"
	"yoli/internal/ai"
)

func TestSelectProviderProfile_FlagBeatsDefaultProvider(t *testing.T) {
	profiles := ProviderProfiles{
		"a": {BaseURL: "https://a/v1", APIKey: "ka"},
		"b": {BaseURL: "https://b/v1", APIKey: "kb"},
	}
	cfg := Config{"default_provider": "a"}
	p, name, err := selectProviderProfile(cfg, profiles, "b")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if name != "b" || p.BaseURL != "https://b/v1" {
		t.Fatalf("flag should win: name=%q p=%+v", name, p)
	}
	p, name, err = selectProviderProfile(cfg, profiles, "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if name != "a" || p.BaseURL != "https://a/v1" {
		t.Fatalf("default_provider fallback: name=%q p=%+v", name, p)
	}
}

func TestSelectProviderProfile_IgnoresEnvironment(t *testing.T) {
	t.Setenv("YOLI_PROVIDER", "b")
	profiles := ProviderProfiles{
		"a": {BaseURL: "https://a/v1", APIKey: "ka"},
		"b": {BaseURL: "https://b/v1", APIKey: "kb"},
	}
	_, name, err := selectProviderProfile(Config{"default_provider": "a"}, profiles, "")
	if err != nil || name != "a" {
		t.Fatalf("env var must not select a provider: name=%q err=%v", name, err)
	}
}

func TestSelectProviderProfile_NoProfilesErrors(t *testing.T) {
	_, _, err := selectProviderProfile(Config{}, ProviderProfiles{}, "")
	if err == nil || !strings.Contains(err.Error(), "no provider profiles defined") {
		t.Fatalf("err = %v", err)
	}
}

func TestSelectProviderProfile_NoSelectionErrorsWithList(t *testing.T) {
	profiles := ProviderProfiles{
		"beta":  {BaseURL: "https://b/v1"},
		"alpha": {BaseURL: "https://a/v1"},
	}
	_, _, err := selectProviderProfile(Config{}, profiles, "")
	if err == nil || !strings.Contains(err.Error(), "no provider selected") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "alpha, beta") {
		t.Fatalf("error should list sorted profiles: %v", err)
	}
}

func TestSelectProviderProfile_UnknownNameErrorsWithList(t *testing.T) {
	profiles := ProviderProfiles{
		"beta":  {BaseURL: "https://b/v1"},
		"alpha": {BaseURL: "https://a/v1"},
	}
	_, _, err := selectProviderProfile(Config{"default_provider": "gone"}, profiles, "")
	if err == nil || !strings.Contains(err.Error(), `unknown provider profile "gone"`) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "alpha, beta") {
		t.Fatalf("error should list sorted profiles: %v", err)
	}
}

func TestNewProviderFromProfile_RequiresKeyAndURL(t *testing.T) {
	if _, err := newProviderFromProfile(ProviderProfile{BaseURL: "https://a/v1"}, "T"); err == nil {
		t.Fatalf("want error for missing api_key")
	}
	if _, err := newProviderFromProfile(ProviderProfile{APIKey: "k"}, "T"); err == nil {
		t.Fatalf("want error for missing base_url")
	}
	p, err := newProviderFromProfile(ProviderProfile{BaseURL: "https://a/v1", APIKey: "k"}, "T")
	if err != nil || p == nil {
		t.Fatalf("complete profile should build: p=%v err=%v", p, err)
	}
}

func TestContextLimits_ProfileValuesAndDefaults(t *testing.T) {
	w, m := contextLimits(ProviderProfile{ContextWindow: 32768, MaxTokens: 4096})
	if w != 32768 || m != 4096 {
		t.Fatalf("w/m = %d/%d, want 32768/4096", w, m)
	}
	w, m = contextLimits(ProviderProfile{})
	if w != agent.DefaultContextBudget || m != agent.DefaultMaxOutputTokens {
		t.Fatalf("w/m = %d/%d, want defaults", w, m)
	}
	w, m = contextLimits(ProviderProfile{ContextWindow: -1, MaxTokens: -1})
	if w != agent.DefaultContextBudget || m != agent.DefaultMaxOutputTokens {
		t.Fatalf("non-positive values should fall back to defaults: %d/%d", w, m)
	}
}

func TestNewProviderFromProfile_ForwardsIncludeReasoning(t *testing.T) {
	// The profile flag must reach the provider: without it the backend
	// never reports chain-of-thought and the CLI can render nothing.
	// Assert on the observable request body rather than private fields.
	on := newReasoningRecorder(t)
	p, err := newProviderFromProfile(ProviderProfile{
		BaseURL: on.URL, APIKey: "k", IncludeReasoning: true,
	}, "T")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := p.Chat(context.Background(), ai.ChatRequest{Model: "m"}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !on.sawIncludeReasoning {
		t.Fatalf("include_reasoning not sent when the profile enables it")
	}

	off := newReasoningRecorder(t)
	p2, err := newProviderFromProfile(ProviderProfile{BaseURL: off.URL, APIKey: "k"}, "T")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := p2.Chat(context.Background(), ai.ChatRequest{Model: "m"}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if off.sawIncludeReasoning {
		t.Fatalf("include_reasoning sent when the profile omits it")
	}
}

// reasoningRecorder serves one /chat/completions request and records
// whether the body carried include_reasoning: true.
type reasoningRecorder struct {
	URL                 string
	sawIncludeReasoning bool
}

func newReasoningRecorder(t *testing.T) *reasoningRecorder {
	t.Helper()
	rec := &reasoningRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		rec.sawIncludeReasoning = parsed["include_reasoning"] == true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	t.Cleanup(srv.Close)
	rec.URL = srv.URL
	return rec
}
