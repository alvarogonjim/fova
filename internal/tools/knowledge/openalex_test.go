package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const openAlexBody = `{
  "results": [
    {"doi": "https://doi.org/10.1/oa1", "title": "Latent Diffusion for Proteins",
     "publication_year": 2023},
    {"doi": "https://doi.org/10.1/oa2", "title": "Backbone Generation",
     "publication_year": 2024}
  ]
}`

func TestOpenAlexExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openAlexBody))
	}))
	defer srv.Close()

	res := NewResults()
	tool := NewOpenAlex(res, "")
	tool.BaseURL = srv.URL

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"protein","limit":5}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var parsed struct {
		ResultsID string  `json:"results_id"`
		Count     int     `json:"count"`
		Papers    []Paper `json:"papers"`
	}
	if err := json.Unmarshal(out.Output, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if parsed.Count != 2 {
		t.Fatalf("count = %d, want 2", parsed.Count)
	}
	p := parsed.Papers[0]
	if p.ID != "https://doi.org/10.1/oa1" {
		t.Errorf("papers[0].ID = %q", p.ID)
	}
	if p.Title != "Latent Diffusion for Proteins" {
		t.Errorf("papers[0].Title = %q", p.Title)
	}
	if p.Year != 2023 {
		t.Errorf("papers[0].Year = %d, want 2023", p.Year)
	}
	if p.Source != "openalex" {
		t.Errorf("papers[0].Source = %q, want openalex", p.Source)
	}
	if _, ok := res.Get(parsed.ResultsID); !ok {
		t.Fatalf("results_id %q not in cache", parsed.ResultsID)
	}
}

func TestOpenAlexEmptyQuery(t *testing.T) {
	tool := NewOpenAlex(NewResults(), "")
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":""}`)); err == nil {
		t.Fatal("expected error for empty query")
	}
}
