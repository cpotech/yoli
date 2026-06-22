package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"yoli/internal/ai"
)

const (
	braveDefaultEndpoint = "https://api.search.brave.com/res/v1/web/search"
	webSearchDefaultN    = 5
	webSearchMaxN        = 20
	webSearchBodyExcerpt = 512
)

// WebSearchTool issues a GET against the Brave Search REST API and
// returns a numbered title/URL/snippet list. It honors ctx cancellation.
type WebSearchTool struct {
	httpClient *http.Client
	endpoint   string
}

// NewWebSearchTool constructs a WebSearchTool backed by http.DefaultClient
// and the public Brave Search endpoint.
func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{httpClient: http.DefaultClient, endpoint: braveDefaultEndpoint}
}

// newWebSearchToolWithClient builds a WebSearchTool with an injected
// HTTP client and endpoint, used by tests against httptest servers.
func newWebSearchToolWithClient(endpoint string, client *http.Client) *WebSearchTool {
	if client == nil {
		client = http.DefaultClient
	}
	return &WebSearchTool{httpClient: client, endpoint: endpoint}
}

// Definition returns the JSON-schema description for the model.
func (t *WebSearchTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name: "WebSearch",
		Description: "Search the web via the Brave Search API. " +
			"Returns a numbered list of (title, URL, snippet). Requires BRAVE_API_KEY.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query.",
				},
				"count": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Max results to return (1–%d, default %d).", webSearchMaxN, webSearchDefaultN),
				},
			},
			"required": []string{"query"},
		},
	}
}

type webSearchArgs struct {
	Query string `json:"query"`
	Count int    `json:"count"`
}

type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

// Run dispatches the search and formats results. Missing API key,
// invalid arguments, and request/transport failures surface as Go
// errors; HTTP non-2xx responses are returned as tool-output strings so
// the agent loop can keep going.
func (t *WebSearchTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args webSearchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("web_search: invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", errors.New("web_search: query is required")
	}
	count := args.Count
	if count <= 0 {
		count = webSearchDefaultN
	}
	if count > webSearchMaxN {
		count = webSearchMaxN
	}
	apiKey := os.Getenv("BRAVE_API_KEY")
	if apiKey == "" {
		return "", errors.New("web_search: BRAVE_API_KEY is not set")
	}

	u, err := url.Parse(t.endpoint)
	if err != nil {
		return "", fmt.Errorf("web_search: invalid endpoint: %w", err)
	}
	q := u.Query()
	q.Set("q", args.Query)
	q.Set("count", strconv.Itoa(count))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("web_search: build request: %w", err)
	}
	req.Header.Set("X-Subscription-Token", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_search: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("web_search: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt := string(body)
		if len(excerpt) > webSearchBodyExcerpt {
			excerpt = excerpt[:webSearchBodyExcerpt] + "…"
		}
		return fmt.Sprintf("web_search: HTTP %d %s\n%s", resp.StatusCode, http.StatusText(resp.StatusCode), excerpt), nil
	}

	var parsed braveResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("web_search: decode response: %w", err)
	}
	results := parsed.Web.Results
	if len(results) > count {
		results = results[:count]
	}
	if len(results) == 0 {
		return "No results.", nil
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n", i+1, r.Title, r.URL, r.Description)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

var _ Tool = (*WebSearchTool)(nil)
