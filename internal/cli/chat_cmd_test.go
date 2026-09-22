package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubCompletionsServer serves a fixed assistant message, optionally
// carrying a reasoning field, and always reports success.
func stubCompletionsServer(t *testing.T, reasoning, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		msg := `{"role":"assistant","content":"` + content + `"`
		if reasoning != "" {
			msg += `,"reasoning":"` + reasoning + `"`
		}
		msg += `}`
		_, _ = w.Write([]byte(`{"choices":[{"message":` + msg + `,"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeProfile(t *testing.T, home, name, baseURL string) {
	t.Helper()
	body := `{"default_provider":"` + name + `","providers":{"` + name + `":` +
		`{"base_url":"` + baseURL + `","api_key":"k","model":"m"}}}`
	writeUserConfig(t, home, body)
}

func TestChat_ThinkingLoggedToStderrWhenBackendReportsReasoning(t *testing.T) {
	// End-to-end: a profile that opts into reasoning, a backend that
	// returns it, and `yoli chat` showing it on stderr — the same stream
	// tool calls already use — without touching the answer on stdout.
	srv := stubCompletionsServer(t, "I should read the file first", "the answer")
	home := t.TempDir()
	writeProfile(t, home, "stub", srv.URL)

	r := runCli(t, []string{"chat", "--no-session", "hello"}, runOpts{home: home})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d stderr=%q", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "the answer") {
		t.Fatalf("missing answer on stdout: %q", r.stdout)
	}
	if !strings.Contains(r.stderr, "I should read the file first") {
		t.Fatalf("thinking not logged to stderr: %q", r.stderr)
	}
	if !strings.Contains(r.stderr, "thinking") {
		t.Fatalf("thinking line not labelled: %q", r.stderr)
	}
}

func TestChat_NoThinkingLineWhenBackendOmitsReasoning(t *testing.T) {
	srv := stubCompletionsServer(t, "", "plain answer")
	home := t.TempDir()
	writeProfile(t, home, "stub", srv.URL)

	r := runCli(t, []string{"chat", "--no-session", "hello"}, runOpts{home: home})
	if r.exitCode != 0 {
		t.Fatalf("exit = %d stderr=%q", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "plain answer") {
		t.Fatalf("missing answer on stdout: %q", r.stdout)
	}
	if strings.Contains(r.stderr, "thinking") {
		t.Fatalf("unexpected thinking line: %q", r.stderr)
	}
}
