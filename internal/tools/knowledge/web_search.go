package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// tavilySearchURL is the default Tavily search endpoint (SPECS §7.2.5).
const tavilySearchURL = "https://api.tavily.com/search"

var _ tools.Tool = (*WebSearch)(nil)

// WebSearch implements the knowledge.web_search tool backed by the Tavily API.
// It degrades gracefully when no API key is configured.
type WebSearch struct {
	APIKey  string // Tavily API key; settable in tests
	BaseURL string // Tavily search endpoint; overridable in tests
}

// NewWebSearch returns the knowledge.web_search tool, reading TAVILY_API_KEY
// from the environment.
func NewWebSearch() *WebSearch {
	return &WebSearch{
		APIKey:  os.Getenv("TAVILY_API_KEY"),
		BaseURL: tavilySearchURL,
	}
}

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

type webSearchOutput struct {
	Configured bool              `json:"configured"`
	Results    []webSearchResult `json:"results"`
}

func (*WebSearch) Name() string { return "knowledge.web_search" }
func (*WebSearch) Description() string {
	return "Search the web for recent information using the Tavily API. " +
		"Requires the TAVILY_API_KEY environment variable to be set."
}
func (*WebSearch) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query",
			},
		},
		"required": []string{"query"},
	}
}
func (*WebSearch) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*WebSearch) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*WebSearch) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

// Execute runs a Tavily search, or returns a not-configured result when no
// API key is available.
func (s *WebSearch) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	if s.APIKey == "" {
		out := webSearchOutput{Configured: false, Results: []webSearchResult{}}
		outJSON, _ := json.Marshal(out)
		return tools.Result{
			Output:     outJSON,
			Display:    "knowledge.web_search is not configured — set TAVILY_API_KEY to enable it",
			Provenance: domain.NewToolCallRef("knowledge.web_search", input),
		}, nil
	}

	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.Query == "" {
		return tools.Result{}, fmt.Errorf("knowledge.web_search: query is required")
	}

	reqBody, _ := json.Marshal(map[string]any{
		"api_key":     s.APIKey,
		"query":       in.Query,
		"max_results": 5,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL,
		bytes.NewReader(reqBody))
	if err != nil {
		return tools.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return tools.Result{}, fmt.Errorf("tavily request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tools.Result{}, fmt.Errorf("tavily returned %d", resp.StatusCode)
	}

	var tavily struct {
		Results []webSearchResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tavily); err != nil {
		return tools.Result{}, fmt.Errorf("decode tavily response: %w", err)
	}

	results := make([]webSearchResult, 0, len(tavily.Results))
	for _, r := range tavily.Results {
		results = append(results, webSearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Content,
		})
	}
	out := webSearchOutput{Configured: true, Results: results}
	outJSON, _ := json.Marshal(out)
	return tools.Result{
		Output:     outJSON,
		Display:    fmt.Sprintf("web_search: %d results", len(results)),
		Provenance: domain.NewToolCallRef("knowledge.web_search", input),
	}, nil
}
