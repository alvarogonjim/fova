package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const s2Body = `{
  "data": [
    {"title": "Sequence Design", "year": 2020, "abstract": "Abstract one.",
     "externalIds": {"DOI": "10.1/s2a"}},
    {"title": "Inverse Folding", "year": 2021, "abstract": "Abstract two.",
     "externalIds": {"DOI": "10.1/s2b"}}
  ]
}`

func TestS2Execute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(s2Body))
	}))
	defer srv.Close()

	res := NewResults()
	tool := NewS2(res)
	tool.BaseURL = srv.URL

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"design","limit":10}`))
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
	if p.ID != "10.1/s2a" {
		t.Errorf("papers[0].ID = %q, want 10.1/s2a", p.ID)
	}
	if p.Title != "Sequence Design" {
		t.Errorf("papers[0].Title = %q", p.Title)
	}
	if p.Year != 2020 {
		t.Errorf("papers[0].Year = %d, want 2020", p.Year)
	}
	if p.Abstract != "Abstract one." {
		t.Errorf("papers[0].Abstract = %q", p.Abstract)
	}
	if p.Source != "s2" {
		t.Errorf("papers[0].Source = %q, want s2", p.Source)
	}
	if _, ok := res.Get(parsed.ResultsID); !ok {
		t.Fatalf("results_id %q not in cache", parsed.ResultsID)
	}
}

func TestS2EmptyQuery(t *testing.T) {
	tool := NewS2(NewResults())
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":""}`)); err == nil {
		t.Fatal("expected error for empty query")
	}
}
