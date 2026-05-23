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

func TestPDBSearchExecute_WithFilters(t *testing.T) {
	var captured map[string]any
	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode search body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result_set":[],"total_count":0}`))
	}))
	defer searchSrv.Close()

	// GraphQL must not be hit (no IDs to enrich), but wire a server anyway so a
	// stray request would obviously fail the test.
	graphqlSrv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("GraphQL should not be called with zero IDs")
	}))
	defer graphqlSrv.Close()

	tool := NewPDBSearch()
	tool.SearchURL = searchSrv.URL
	tool.GraphQLURL = graphqlSrv.URL

	in := json.RawMessage(`{"query":"PD-L1","organism":"Homo sapiens","max_resolution":3.0,"method":"X-RAY DIFFRACTION"}`)
	if _, err := tool.Execute(context.Background(), in); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Walk the captured request body and assert all three filter terminals + the
	// full-text terminal are present. We decode rather than string-match so the
	// test does not care about JSON key ordering.
	queryMap, ok := captured["query"].(map[string]any)
	if !ok {
		t.Fatalf("query missing: %v", captured)
	}
	if queryMap["type"] != "group" {
		t.Errorf("query.type = %v, want group", queryMap["type"])
	}
	nodes, ok := queryMap["nodes"].([]any)
	if !ok {
		t.Fatalf("query.nodes missing: %v", queryMap)
	}
	if len(nodes) != 4 {
		t.Fatalf("got %d nodes, want 4 (full_text + 3 filters)", len(nodes))
	}

	seen := map[string]bool{}
	for _, n := range nodes {
		nm := n.(map[string]any)
		params := nm["parameters"].(map[string]any)
		if nm["service"] == "full_text" {
			if params["value"] != "PD-L1" {
				t.Errorf("full_text value = %v", params["value"])
			}
			seen["full_text"] = true
			continue
		}
		if nm["service"] != "text" {
			t.Errorf("unexpected service %v", nm["service"])
			continue
		}
		seen[params["attribute"].(string)] = true
	}
	for _, want := range []string{"full_text", "rcsb_entity_source_organism.ncbi_scientific_name", "exptl.method", "rcsb_entry_info.resolution_combined"} {
		if !seen[want] {
			t.Errorf("missing node %q in captured request: %v", want, captured)
		}
	}
}

func TestPDBSearchExecute_ZeroHits(t *testing.T) {
	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer searchSrv.Close()

	graphqlSrv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("GraphQL must not be called when search returns 204")
	}))
	defer graphqlSrv.Close()

	tool := NewPDBSearch()
	tool.SearchURL = searchSrv.URL
	tool.GraphQLURL = graphqlSrv.URL

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"nonsense xyz"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out pdbSearchOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Count != 0 || out.TotalHits != 0 {
		t.Errorf("output = %+v, want zero counts", out)
	}
	if len(out.Results) != 0 {
		t.Errorf("results not empty: %+v", out.Results)
	}
}
