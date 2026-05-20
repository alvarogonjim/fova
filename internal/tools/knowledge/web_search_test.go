package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebSearchNotConfigured(t *testing.T) {
	tool := NewWebSearch()
	tool.APIKey = ""

	input, _ := json.Marshal(map[string]string{"query": "protein folding"})
	res, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error %v", err)
	}
	if res.Display != "knowledge.web_search is not configured — set TAVILY_API_KEY to enable it" {
		t.Errorf("Display = %q", res.Display)
	}

	var out struct {
		Configured bool              `json:"configured"`
		Results    []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Configured {
		t.Errorf("configured = true, want false")
	}
	if len(out.Results) != 0 {
		t.Errorf("results = %v, want empty", out.Results)
	}
}

// TestWebSearchNotConfiguredShortCircuits proves the no-key branch runs before
// input validation: with no API key, even input that lacks the required
// "query" field still yields a successful configured:false result.
func TestWebSearchNotConfiguredShortCircuits(t *testing.T) {
	tool := NewWebSearch()
	tool.APIKey = ""

	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: unexpected error %v", err)
	}

	var out struct {
		Configured bool              `json:"configured"`
		Results    []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Configured {
		t.Errorf("configured = true, want false")
	}
	if len(out.Results) != 0 {
		t.Errorf("results = %v, want empty", out.Results)
	}
}

func TestWebSearchMapsResults(t *testing.T) {
	const tavilyBody = `{
		"results": [
			{"title": "Paper A", "url": "https://a.example/1", "content": "alpha content"},
			{"title": "Paper B", "url": "https://b.example/2", "content": "beta content"}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var body struct {
			APIKey     string `json:"api_key"`
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.APIKey != "test" {
			t.Errorf("api_key = %q, want test", body.APIKey)
		}
		if body.Query != "protein folding" {
			t.Errorf("query = %q", body.Query)
		}
		if body.MaxResults != 5 {
			t.Errorf("max_results = %d, want 5", body.MaxResults)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(tavilyBody))
	}))
	defer srv.Close()

	tool := NewWebSearch()
	tool.APIKey = "test"
	tool.BaseURL = srv.URL

	input, _ := json.Marshal(map[string]string{"query": "protein folding"})
	res, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var out struct {
		Configured bool `json:"configured"`
		Results    []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !out.Configured {
		t.Errorf("configured = false, want true")
	}
	if len(out.Results) != 2 {
		t.Fatalf("results count = %d, want 2", len(out.Results))
	}
	if out.Results[0].Title != "Paper A" || out.Results[0].URL != "https://a.example/1" || out.Results[0].Content != "alpha content" {
		t.Errorf("result[0] = %+v", out.Results[0])
	}
	if out.Results[1].Title != "Paper B" {
		t.Errorf("result[1].Title = %q", out.Results[1].Title)
	}
	if res.Display != "web_search: 2 results" {
		t.Errorf("Display = %q", res.Display)
	}
}
