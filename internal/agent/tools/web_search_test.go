package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebSearch_Definition(t *testing.T) {
	tl := NewWebSearchTool()
	def := tl.Definition()
	if def.Name != "WebSearch" {
		t.Fatalf("name = %q, want WebSearch", def.Name)
	}
	props, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing/wrong type: %+v", def.Parameters)
	}
	q, ok := props["query"].(map[string]any)
	if !ok {
		t.Fatalf("query missing: %+v", props)
	}
	if q["type"] != "string" {
		t.Fatalf("query type = %v, want string", q["type"])
	}
	c, ok := props["count"].(map[string]any)
	if !ok {
		t.Fatalf("count missing: %+v", props)
	}
	if c["type"] != "integer" {
		t.Fatalf("count type = %v, want integer", c["type"])
	}
	req, ok := def.Parameters["required"].([]string)
	if !ok {
		t.Fatalf("required missing/wrong type: %+v", def.Parameters)
	}
	var hasQuery bool
	for _, r := range req {
		if r == "query" {
			hasQuery = true
		}
		if r == "count" {
			t.Fatalf("count must not be required")
		}
	}
	if !hasQuery {
		t.Fatalf("query must be required")
	}
}

func TestWebSearch_MissingAPIKey(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "")
	tl := NewWebSearchTool()
	_, err := tl.Run(context.Background(), json.RawMessage(`{"query":"go"}`))
	if err == nil {
		t.Fatalf("want error when BRAVE_API_KEY missing")
	}
	if !strings.Contains(err.Error(), "BRAVE_API_KEY") {
		t.Fatalf("err should mention BRAVE_API_KEY: %v", err)
	}
}

func TestWebSearch_InvalidJSON(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "k")
	tl := NewWebSearchTool()
	_, err := tl.Run(context.Background(), json.RawMessage(`not-json`))
	if err == nil {
		t.Fatalf("want error on invalid JSON")
	}
}

func TestWebSearch_SendsHeaderAndQuery(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "secret-token")
	var gotToken, gotAccept, gotQ, gotCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Subscription-Token")
		gotAccept = r.Header.Get("Accept")
		gotQ = r.URL.Query().Get("q")
		gotCount = r.URL.Query().Get("count")
		_, _ = w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()
	tl := newWebSearchToolWithClient(srv.URL, srv.Client())
	_, err := tl.Run(context.Background(), json.RawMessage(`{"query":"golang test","count":3}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotToken != "secret-token" {
		t.Fatalf("token = %q", gotToken)
	}
	if !strings.Contains(gotAccept, "application/json") {
		t.Fatalf("accept = %q", gotAccept)
	}
	if gotQ != "golang test" {
		t.Fatalf("q = %q", gotQ)
	}
	if gotCount != "3" {
		t.Fatalf("count = %q", gotCount)
	}
}

func TestWebSearch_FormatsResults(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "k")
	body := `{"web":{"results":[
		{"title":"Go","url":"https://go.dev","description":"The Go programming language."},
		{"title":"Golang Blog","url":"https://go.dev/blog","description":"Updates from the team."}
	]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	tl := newWebSearchToolWithClient(srv.URL, srv.Client())
	out, err := tl.Run(context.Background(), json.RawMessage(`{"query":"go"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, want := range []string{
		"1.", "Go", "https://go.dev", "The Go programming language.",
		"2.", "Golang Blog", "https://go.dev/blog", "Updates from the team.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestWebSearch_TruncatesToCount(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "k")
	body := `{"web":{"results":[
		{"title":"A","url":"https://a","description":"a"},
		{"title":"B","url":"https://b","description":"b"},
		{"title":"C","url":"https://c","description":"c"},
		{"title":"D","url":"https://d","description":"d"}
	]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	tl := newWebSearchToolWithClient(srv.URL, srv.Client())
	out, err := tl.Run(context.Background(), json.RawMessage(`{"query":"x","count":2}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "1.") || !strings.Contains(out, "2.") {
		t.Fatalf("expected entries 1 and 2: %s", out)
	}
	if strings.Contains(out, "3.") || strings.Contains(out, "https://c") {
		t.Fatalf("expected truncation past count=2:\n%s", out)
	}
}

func TestWebSearch_DefaultCountIsFive(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "k")
	var gotCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCount = r.URL.Query().Get("count")
		_, _ = w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()
	tl := newWebSearchToolWithClient(srv.URL, srv.Client())
	_, err := tl.Run(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotCount != "5" {
		t.Fatalf("default count = %q, want 5", gotCount)
	}
}

func TestWebSearch_NoResultsMessage(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()
	tl := newWebSearchToolWithClient(srv.URL, srv.Client())
	out, err := tl.Run(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "No results.") {
		t.Fatalf("expected 'No results.': %q", out)
	}
}

func TestWebSearch_NonOKReturnsToolOutput(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()
	tl := newWebSearchToolWithClient(srv.URL, srv.Client())
	out, err := tl.Run(context.Background(), json.RawMessage(`{"query":"x"}`))
	if err != nil {
		t.Fatalf("non-2xx should not return Go error, got: %v", err)
	}
	if !strings.Contains(out, "429") {
		t.Fatalf("output should mention status 429: %q", out)
	}
	if !strings.Contains(out, "rate limited") {
		t.Fatalf("output should include body excerpt: %q", out)
	}
}

func TestWebSearch_ContextCancellation(t *testing.T) {
	t.Setenv("BRAVE_API_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	tl := newWebSearchToolWithClient(srv.URL, srv.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tl.Run(ctx, json.RawMessage(`{"query":"x"}`))
	if err == nil {
		t.Fatalf("want error on cancelled context")
	}
}
