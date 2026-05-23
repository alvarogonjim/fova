package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alvarogonjim/fova/internal/tools"
)

var _ tools.Tool = (*PDBSearch)(nil)

func TestPDBSearchExecuteEmptyQuery(t *testing.T) {
	tool := NewPDBSearch()
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"query":""}`)); err == nil {
		t.Fatal("expected error for empty query")
	}
}

const pdbSearchBody = `{
  "result_set": [
    {"identifier": "5O45", "score": 0.94},
    {"identifier": "5N2D", "score": 0.81}
  ],
  "total_count": 213
}`

const pdbGraphQLBody = `{
  "data": {
    "entries": [
      {
        "rcsb_id": "5O45",
        "struct": {"title": "Crystal structure of human PD-L1"},
        "exptl": [{"method": "X-RAY DIFFRACTION"}],
        "rcsb_entry_info": {"resolution_combined": [2.20], "initial_release_date": "2017-08-23T00:00:00Z"}
      },
      {
        "rcsb_id": "5N2D",
        "struct": {"title": "PD-1/PD-L1 complex"},
        "exptl": [{"method": "X-RAY DIFFRACTION"}],
        "rcsb_entry_info": {"resolution_combined": [2.45], "initial_release_date": "2017-03-15T00:00:00Z"}
      }
    ]
  }
}`

func TestPDBSearchExecute(t *testing.T) {
	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pdbSearchBody))
	}))
	defer searchSrv.Close()

	graphqlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pdbGraphQLBody))
	}))
	defer graphqlSrv.Close()

	tool := NewPDBSearch()
	tool.SearchURL = searchSrv.URL
	tool.GraphQLURL = graphqlSrv.URL

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"PD-L1"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var out pdbSearchOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Count != 2 {
		t.Errorf("count = %d, want 2", out.Count)
	}
	if out.TotalHits != 213 {
		t.Errorf("total_hits = %d, want 213", out.TotalHits)
	}
	if len(out.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(out.Results))
	}
	first := out.Results[0]
	if first.PDBID != "5O45" || first.Score != 0.94 {
		t.Errorf("first hit = %+v, want 5O45 / 0.94", first)
	}
	if first.Title != "Crystal structure of human PD-L1" {
		t.Errorf("first title = %q", first.Title)
	}
	if first.Method != "X-RAY DIFFRACTION" {
		t.Errorf("first method = %q", first.Method)
	}
	if first.Resolution != 2.20 {
		t.Errorf("first resolution = %v", first.Resolution)
	}
	if first.Year != 2017 {
		t.Errorf("first year = %d", first.Year)
	}
	if res.Display == "" {
		t.Error("Display is empty")
	}
	if res.Provenance.Tool != "knowledge.pdb_search" {
		t.Errorf("Provenance.Tool = %q, want knowledge.pdb_search", res.Provenance.Tool)
	}
}
