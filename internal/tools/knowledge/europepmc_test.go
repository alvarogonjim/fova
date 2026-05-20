package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alvarogonjim/fova/internal/tools"
)

var (
	_ tools.Tool = (*EuropePMC)(nil)
	_ tools.Tool = (*OpenAlex)(nil)
	_ tools.Tool = (*S2)(nil)
	_ tools.Tool = (*BioRxiv)(nil)
	_ tools.Tool = (*Crossref)(nil)
)

const europePMCBody = `{
  "resultList": {
    "result": [
      {"doi": "10.1/abc", "pmcid": "PMC1", "title": "Designing Proteins",
       "authorString": "Smith J, Doe A", "pubYear": "2021",
       "abstractText": "An abstract."},
      {"doi": "", "pmcid": "PMC2", "title": "Folding Studies",
       "authorString": "Lee K", "pubYear": "2022", "abstractText": ""}
    ]
  }
}`

func TestEuropePMCExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(europePMCBody))
	}))
	defer srv.Close()

	res := NewResults()
	tool := NewEuropePMC(res)
	tool.BaseURL = srv.URL

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"protein design"}`))
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
	if p.ID != "10.1/abc" {
		t.Errorf("papers[0].ID = %q, want 10.1/abc", p.ID)
	}
	if p.Title != "Designing Proteins" {
		t.Errorf("papers[0].Title = %q", p.Title)
	}
	if p.Year != 2021 {
		t.Errorf("papers[0].Year = %d, want 2021", p.Year)
	}
	if p.Source != "europepmc" {
		t.Errorf("papers[0].Source = %q, want europepmc", p.Source)
	}
	if parsed.Papers[1].ID != "PMC2" {
		t.Errorf("papers[1].ID = %q, want PMC2 (pmcid fallback)", parsed.Papers[1].ID)
	}
	cached, ok := res.Get(parsed.ResultsID)
	if !ok {
		t.Fatalf("results_id %q not in cache", parsed.ResultsID)
	}
	if len(cached) != 2 {
		t.Errorf("cached papers = %d, want 2", len(cached))
	}
}

func TestEuropePMCEmptyQuery(t *testing.T) {
	tool := NewEuropePMC(NewResults())
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":""}`)); err == nil {
		t.Fatal("expected error for empty query")
	}
}
