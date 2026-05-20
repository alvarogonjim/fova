package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const crossrefBody = `{
  "message": {
    "items": [
      {"DOI": "10.1/cr1", "title": ["Protein Stability"],
       "published": {"date-parts": [[2019, 5]]}},
      {"DOI": "10.1/cr2", "title": ["Mutational Scanning"],
       "published": {"date-parts": [[2022]]}}
    ]
  }
}`

func TestCrossrefExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(crossrefBody))
	}))
	defer srv.Close()

	res := NewResults()
	tool := NewCrossref(res)
	tool.BaseURL = srv.URL

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"stability"}`))
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
	if p.ID != "10.1/cr1" {
		t.Errorf("papers[0].ID = %q, want 10.1/cr1", p.ID)
	}
	if p.Title != "Protein Stability" {
		t.Errorf("papers[0].Title = %q", p.Title)
	}
	if p.Year != 2019 {
		t.Errorf("papers[0].Year = %d, want 2019", p.Year)
	}
	if p.Source != "crossref" {
		t.Errorf("papers[0].Source = %q, want crossref", p.Source)
	}
	if parsed.Papers[1].Year != 2022 {
		t.Errorf("papers[1].Year = %d, want 2022", parsed.Papers[1].Year)
	}
	if _, ok := res.Get(parsed.ResultsID); !ok {
		t.Fatalf("results_id %q not in cache", parsed.ResultsID)
	}
}

func TestCrossrefEmptyQuery(t *testing.T) {
	tool := NewCrossref(NewResults())
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":""}`)); err == nil {
		t.Fatal("expected error for empty query")
	}
}
