# `knowledge.pdb_search` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a new fova agent tool, `knowledge.pdb_search`, that resolves free-text queries (e.g. "PD-L1") to candidate PDB IDs via the RCSB search API, enriched with title/method/resolution via a batched GraphQL call — fixing the hallucination case documented in `docs/KNOWN-ISSUES-2026-05-21.md` §5.

**Architecture:** One tool file in `internal/tools/knowledge/`, mirroring the shape of `pdb.go`. Each `Execute` performs two POSTs: phase 1 against `https://search.rcsb.org/rcsbsearch/v2/query` for `{identifier, score}` hits + `total_count`, phase 2 against `https://data.rcsb.org/graphql` for batched title/method/resolution/year. A new `postJSON` helper is added to `client.go` (no other knowledge tool POSTs JSON yet).

**Tech Stack:** Go, `net/http` (stdlib), `net/http/httptest` for tests, `encoding/json`, RCSB search v2 + RCSB data GraphQL. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-23-knowledge-pdb-search-design.md`

**Branch / worktree:** `feat/knowledge-pdb-search` in `/home/gonjim/Projects/proteus-pdbsearch` (already checked out).

---

## File Structure

| Path | Action | Responsibility |
|------|--------|----------------|
| `internal/tools/knowledge/client.go` | Modify | Add `postJSON` helper. ~25 new lines. |
| `internal/tools/knowledge/client_test.go` | Modify | Add a `TestPostJSON_*` block covering 2xx, 204, non-2xx. |
| `internal/tools/knowledge/pdb_search.go` | Create | The tool itself: `PDBSearch` struct, `Tool` interface methods, search/enrich logic. |
| `internal/tools/knowledge/pdb_search_test.go` | Create | All five table-driven tests from the spec + interface assertion. |
| `cmd/fova/main.go` | Modify | One new `registry.Register(knowledge.NewPDBSearch())` line. |

Conventions to follow (cribbed from `pdb.go` / `europepmc.go`):
- Tool struct exposes a `BaseURL` (or two URLs here) field overridable for tests.
- HTTP helper lives in `client.go`, shared, with a `User-Agent` header and 8 MiB body cap.
- Tests use `httptest.NewServer`, fixtures are inline string constants.
- Tool interface methods are short and not pointer-receiver unless they need state.

---

## Task 1: Add `postJSON` helper to `client.go`

**Files:**
- Modify: `internal/tools/knowledge/client.go`
- Test: `internal/tools/knowledge/client_test.go`

- [ ] **Step 1: Read `client_test.go` to learn the existing test style**

Run: `cat internal/tools/knowledge/client_test.go`
Expected: read the file to see what test helpers / patterns already exist.

- [ ] **Step 2: Write the failing tests**

Append to `internal/tools/knowledge/client_test.go`:

```go
func TestPostJSON_OK(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("User-Agent missing")
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var out struct {
		OK bool `json:"ok"`
	}
	if err := postJSON(context.Background(), srv.URL, map[string]any{"q": "x"}, &out); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	if !out.OK {
		t.Error("did not decode body")
	}
	if got["q"] != "x" {
		t.Errorf("request body not received: %v", got)
	}
}

func TestPostJSON_NoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	var out struct {
		X int `json:"x"`
	}
	out.X = 7 // sentinel; must remain untouched
	if err := postJSON(context.Background(), srv.URL, map[string]any{}, &out); err != nil {
		t.Fatalf("postJSON 204: %v", err)
	}
	if out.X != 7 {
		t.Errorf("out mutated on 204: X = %d, want 7", out.X)
	}
}

func TestPostJSON_NotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream broken"))
	}))
	defer srv.Close()

	var out map[string]any
	err := postJSON(context.Background(), srv.URL, map[string]any{}, &out)
	if err == nil {
		t.Fatal("expected error for 502")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error does not mention status: %v", err)
	}
}
```

Add any imports missing from the test file (`context`, `encoding/json`, `net/http`, `net/http/httptest`, `strings`, `testing`) — most should already be present.

- [ ] **Step 3: Run tests and verify they fail**

Run: `go test ./internal/tools/knowledge/ -run TestPostJSON -v`
Expected: FAIL with `undefined: postJSON`.

- [ ] **Step 4: Add `postJSON` to `client.go`**

Insert immediately after `getJSON` (around line 35) in `internal/tools/knowledge/client.go`:

```go
// postJSON sends body as JSON and decodes a JSON response into out. Non-2xx
// is an error. 204 No Content is treated as success and leaves out untouched.
func postJSON(ctx context.Context, url string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode body for %s: %w", url, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %d: %s", url, resp.StatusCode,
			strings.TrimSpace(string(respBody)))
	}
	if len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}
```

Add `"bytes"` to the import block (the existing imports already cover everything else).

- [ ] **Step 5: Run tests and verify they pass**

Run: `go test ./internal/tools/knowledge/ -run TestPostJSON -v`
Expected: PASS, all three subtests green.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/knowledge/client.go internal/tools/knowledge/client_test.go
git commit -m "feat(knowledge): add postJSON helper for RCSB search/GraphQL"
```

---

## Task 2: Scaffold `PDBSearch` (interface methods + empty-query guard)

**Files:**
- Create: `internal/tools/knowledge/pdb_search.go`
- Create: `internal/tools/knowledge/pdb_search_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tools/knowledge/pdb_search_test.go`:

```go
package knowledge

import (
	"context"
	"encoding/json"
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
```

- [ ] **Step 2: Run and verify the test fails to compile**

Run: `go test ./internal/tools/knowledge/ -run TestPDBSearchExecuteEmptyQuery`
Expected: FAIL — `undefined: PDBSearch` / `undefined: NewPDBSearch`.

- [ ] **Step 3: Create `pdb_search.go` with the scaffold**

Create `internal/tools/knowledge/pdb_search.go`:

```go
package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

const (
	pdbSearchURL  = "https://search.rcsb.org/rcsbsearch/v2/query"
	pdbGraphQLURL = "https://data.rcsb.org/graphql"

	pdbSearchDefaultLimit = 10
	pdbSearchMaxLimit     = 25

	// RCSB attribute paths used by the text-service filter nodes.
	attrOrganism      = "rcsb_entity_source_organism.ncbi_scientific_name"
	attrMethod        = "exptl.method"
	attrMaxResolution = "rcsb_entry_info.resolution_combined"
)

// PDBSearch implements knowledge.pdb_search: free-text + light-filter search
// over the RCSB PDB, enriched with title/method/resolution.
type PDBSearch struct {
	SearchURL  string // overridable for tests
	GraphQLURL string // overridable for tests
}

// NewPDBSearch returns the knowledge.pdb_search tool with live RCSB endpoints.
func NewPDBSearch() *PDBSearch {
	return &PDBSearch{SearchURL: pdbSearchURL, GraphQLURL: pdbGraphQLURL}
}

func (*PDBSearch) Name() string { return "knowledge.pdb_search" }
func (*PDBSearch) Description() string {
	return "Search the RCSB PDB by free text and optional filters; returns candidate PDB IDs with title, method, and resolution. Use this before knowledge.pdb when you do not already know the entry ID."
}
func (*PDBSearch) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":          map[string]any{"type": "string", "description": "Free-text search (target name, ligand, etc.)."},
			"organism":       map[string]any{"type": "string", "description": "Optional scientific name, e.g. \"Homo sapiens\"."},
			"method":         map[string]any{"type": "string", "description": "Optional experimental method, e.g. \"X-RAY DIFFRACTION\"."},
			"max_resolution": map[string]any{"type": "number", "description": "Optional resolution upper bound, in Å."},
			"limit":          map[string]any{"type": "integer", "description": "Max results (default 10, max 25)."},
		},
		"required": []string{"query"},
	}
}
func (*PDBSearch) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*PDBSearch) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*PDBSearch) EstimatedDuration(json.RawMessage) time.Duration { return 4 * time.Second }

type pdbSearchInput struct {
	Query         string  `json:"query"`
	Organism      string  `json:"organism"`
	Method        string  `json:"method"`
	MaxResolution float64 `json:"max_resolution"`
	Limit         int     `json:"limit"`
}

type pdbSearchHit struct {
	PDBID      string  `json:"pdb_id"`
	Score      float64 `json:"score"`
	Title      string  `json:"title"`
	Method     string  `json:"method"`
	Resolution float64 `json:"resolution"`
	Year       int     `json:"year"`
}

type pdbSearchOutput struct {
	Count     int            `json:"count"`
	TotalHits int            `json:"total_hits"`
	Results   []pdbSearchHit `json:"results"`
}

// Execute runs phase 1 (search) and phase 2 (GraphQL enrich) against RCSB.
func (t *PDBSearch) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in pdbSearchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.Query == "" {
		return tools.Result{}, fmt.Errorf("knowledge.pdb_search: query is required")
	}
	return tools.Result{}, fmt.Errorf("knowledge.pdb_search: not yet implemented")
}
```

- [ ] **Step 4: Run the test and verify it passes**

Run: `go test ./internal/tools/knowledge/ -run TestPDBSearchExecuteEmptyQuery -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/knowledge/pdb_search.go internal/tools/knowledge/pdb_search_test.go
git commit -m "feat(knowledge): scaffold pdb_search tool (interface + empty-query guard)"
```

---

## Task 3: Happy-path search + GraphQL enrich

**Files:**
- Modify: `internal/tools/knowledge/pdb_search.go`
- Modify: `internal/tools/knowledge/pdb_search_test.go`

- [ ] **Step 1: Append the happy-path test**

Add to `internal/tools/knowledge/pdb_search_test.go` (and add `net/http`, `net/http/httptest` to its imports):

```go
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
```

- [ ] **Step 2: Run and verify the test fails**

Run: `go test ./internal/tools/knowledge/ -run TestPDBSearchExecute$ -v`
Expected: FAIL — `Execute` still returns the "not yet implemented" stub.

- [ ] **Step 3: Implement the search request builder and Execute body**

Replace the stub `Execute` and add the helpers below in `pdb_search.go`. Final state of the file's logic section:

```go
func (t *PDBSearch) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in pdbSearchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.Query == "" {
		return tools.Result{}, fmt.Errorf("knowledge.pdb_search: query is required")
	}
	limit := clampPDBSearchLimit(in.Limit)

	searchReq := buildPDBSearchRequest(in, limit)
	var searchResp struct {
		ResultSet []struct {
			Identifier string  `json:"identifier"`
			Score      float64 `json:"score"`
		} `json:"result_set"`
		TotalCount int `json:"total_count"`
	}
	if err := postJSON(ctx, t.SearchURL, searchReq, &searchResp); err != nil {
		return tools.Result{}, err
	}

	hits := make([]pdbSearchHit, 0, len(searchResp.ResultSet))
	ids := make([]string, 0, len(searchResp.ResultSet))
	for _, r := range searchResp.ResultSet {
		hits = append(hits, pdbSearchHit{PDBID: r.Identifier, Score: r.Score})
		ids = append(ids, r.Identifier)
	}

	if len(ids) > 0 {
		enriched, err := t.enrich(ctx, ids)
		if err != nil {
			return tools.Result{}, err
		}
		for i := range hits {
			if e, ok := enriched[hits[i].PDBID]; ok {
				hits[i].Title = e.Title
				hits[i].Method = e.Method
				hits[i].Resolution = e.Resolution
				hits[i].Year = e.Year
			}
		}
	}

	out := pdbSearchOutput{
		Count:     len(hits),
		TotalHits: searchResp.TotalCount,
		Results:   hits,
	}
	outJSON, _ := json.Marshal(out)
	return tools.Result{
		Output:     outJSON,
		Display:    pdbSearchDisplay(in.Query, out),
		Provenance: domain.NewToolCallRef("knowledge.pdb_search", input),
	}, nil
}

func clampPDBSearchLimit(n int) int {
	if n <= 0 {
		return pdbSearchDefaultLimit
	}
	if n > pdbSearchMaxLimit {
		return pdbSearchMaxLimit
	}
	return n
}

// buildPDBSearchRequest assembles the RCSB v2 search payload. A `group` query
// is always emitted, even with one node — RCSB accepts single-node groups, and
// it keeps the builder simple.
func buildPDBSearchRequest(in pdbSearchInput, limit int) map[string]any {
	nodes := []map[string]any{
		{
			"type":    "terminal",
			"service": "full_text",
			"parameters": map[string]any{
				"value": in.Query,
			},
		},
	}
	if in.Organism != "" {
		nodes = append(nodes, textTerminal(attrOrganism, "exact_match", in.Organism))
	}
	if in.Method != "" {
		nodes = append(nodes, textTerminal(attrMethod, "exact_match", in.Method))
	}
	if in.MaxResolution > 0 {
		nodes = append(nodes, textTerminal(attrMaxResolution, "less_or_equal", in.MaxResolution))
	}
	return map[string]any{
		"query": map[string]any{
			"type":             "group",
			"logical_operator": "and",
			"nodes":            nodes,
		},
		"return_type": "entry",
		"request_options": map[string]any{
			"paginate": map[string]any{"start": 0, "rows": limit},
			"sort":     []map[string]any{{"sort_by": "score", "direction": "desc"}},
		},
	}
}

func textTerminal(attribute, operator string, value any) map[string]any {
	return map[string]any{
		"type":    "terminal",
		"service": "text",
		"parameters": map[string]any{
			"attribute": attribute,
			"operator":  operator,
			"value":     value,
		},
	}
}

type pdbEnriched struct {
	Title      string
	Method     string
	Resolution float64
	Year       int
}

// enrich fetches title/method/resolution/year for ids in a single GraphQL POST.
// Missing entries are simply absent from the returned map.
func (t *PDBSearch) enrich(ctx context.Context, ids []string) (map[string]pdbEnriched, error) {
	query := `query($ids:[String!]!){entries(entry_ids:$ids){rcsb_id struct{title} exptl{method} rcsb_entry_info{resolution_combined initial_release_date}}}`
	body := map[string]any{
		"query":     query,
		"variables": map[string]any{"ids": ids},
	}
	var resp struct {
		Data struct {
			Entries []*struct {
				RCSBID string `json:"rcsb_id"`
				Struct struct {
					Title string `json:"title"`
				} `json:"struct"`
				Exptl []struct {
					Method string `json:"method"`
				} `json:"exptl"`
				RCSBEntryInfo struct {
					ResolutionCombined []float64 `json:"resolution_combined"`
					InitialReleaseDate string    `json:"initial_release_date"`
				} `json:"rcsb_entry_info"`
			} `json:"entries"`
		} `json:"data"`
	}
	if err := postJSON(ctx, t.GraphQLURL, body, &resp); err != nil {
		return nil, err
	}
	out := make(map[string]pdbEnriched, len(resp.Data.Entries))
	for _, e := range resp.Data.Entries {
		if e == nil || e.RCSBID == "" {
			continue
		}
		enriched := pdbEnriched{Title: e.Struct.Title}
		if len(e.Exptl) > 0 {
			enriched.Method = e.Exptl[0].Method
		}
		if len(e.RCSBEntryInfo.ResolutionCombined) > 0 {
			enriched.Resolution = e.RCSBEntryInfo.ResolutionCombined[0]
		}
		if len(e.RCSBEntryInfo.InitialReleaseDate) >= 4 {
			if y, err := time.Parse("2006-01-02T15:04:05Z", e.RCSBEntryInfo.InitialReleaseDate); err == nil {
				enriched.Year = y.Year()
			}
		}
		out[e.RCSBID] = enriched
	}
	return out, nil
}

func pdbSearchDisplay(query string, out pdbSearchOutput) string {
	if out.Count == 0 {
		return fmt.Sprintf("pdb_search %q: 0 of %d hits", query, out.TotalHits)
	}
	top := out.Results[0]
	title := top.Title
	if len(title) > 40 {
		title = title[:37] + "..."
	}
	return fmt.Sprintf("pdb_search %q: %d of %d hits (top: %s %s %.2f Å)",
		query, out.Count, out.TotalHits, top.PDBID, title, top.Resolution)
}
```

- [ ] **Step 4: Run the test and verify it passes**

Run: `go test ./internal/tools/knowledge/ -run TestPDBSearchExecute$ -v`
Expected: PASS.

- [ ] **Step 5: Run the whole knowledge package to confirm nothing regressed**

Run: `go test ./internal/tools/knowledge/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/knowledge/pdb_search.go internal/tools/knowledge/pdb_search_test.go
git commit -m "feat(knowledge): implement pdb_search happy path (search + GraphQL enrich)"
```

---

## Task 4: Filters reach the wire

**Files:**
- Modify: `internal/tools/knowledge/pdb_search_test.go`

- [ ] **Step 1: Append the filters test**

Add to `pdb_search_test.go`:

```go
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
```

- [ ] **Step 2: Run and verify the test passes (no implementation change needed)**

Run: `go test ./internal/tools/knowledge/ -run TestPDBSearchExecute_WithFilters -v`
Expected: PASS — Task 3 already builds the right request body. (If it fails, the builder is wrong; fix `buildPDBSearchRequest` and re-run.)

- [ ] **Step 3: Commit**

```bash
git add internal/tools/knowledge/pdb_search_test.go
git commit -m "test(knowledge): assert pdb_search filters reach the RCSB request body"
```

---

## Task 5: Zero-hits (204) short-circuits enrichment

**Files:**
- Modify: `internal/tools/knowledge/pdb_search_test.go`

- [ ] **Step 1: Append the zero-hits test**

```go
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
```

- [ ] **Step 2: Run and verify it passes (no implementation change needed)**

Run: `go test ./internal/tools/knowledge/ -run TestPDBSearchExecute_ZeroHits -v`
Expected: PASS — `postJSON`'s 204 handling + the `len(ids) > 0` guard in `Execute` already cover this. If it fails, double-check both.

- [ ] **Step 3: Commit**

```bash
git add internal/tools/knowledge/pdb_search_test.go
git commit -m "test(knowledge): pdb_search short-circuits enrichment on RCSB 204"
```

---

## Task 6: Partial enrichment (GraphQL returns null for one entry)

**Files:**
- Modify: `internal/tools/knowledge/pdb_search_test.go`

- [ ] **Step 1: Append the partial-enrichment test**

```go
func TestPDBSearchExecute_PartialEnrichment(t *testing.T) {
	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "result_set": [
		    {"identifier": "5O45", "score": 0.94},
		    {"identifier": "9XXX", "score": 0.50}
		  ],
		  "total_count": 2
		}`))
	}))
	defer searchSrv.Close()

	graphqlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "data": {
		    "entries": [
		      {
		        "rcsb_id": "5O45",
		        "struct": {"title": "Crystal structure of human PD-L1"},
		        "exptl": [{"method": "X-RAY DIFFRACTION"}],
		        "rcsb_entry_info": {"resolution_combined": [2.20], "initial_release_date": "2017-08-23T00:00:00Z"}
		      },
		      null
		    ]
		  }
		}`))
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
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(out.Results))
	}
	if out.Results[0].Title == "" {
		t.Error("first row missing enrichment")
	}
	missing := out.Results[1]
	if missing.PDBID != "9XXX" || missing.Score != 0.50 {
		t.Errorf("second row lost identity: %+v", missing)
	}
	if missing.Title != "" || missing.Method != "" || missing.Resolution != 0 || missing.Year != 0 {
		t.Errorf("second row should have empty enriched fields: %+v", missing)
	}
}
```

- [ ] **Step 2: Run and verify it passes**

Run: `go test ./internal/tools/knowledge/ -run TestPDBSearchExecute_PartialEnrichment -v`
Expected: PASS — the `enrich` helper skips `nil` and `e.RCSBID == ""` entries, and the merge loop only writes when the map has the ID.

- [ ] **Step 3: Commit**

```bash
git add internal/tools/knowledge/pdb_search_test.go
git commit -m "test(knowledge): pdb_search tolerates null GraphQL entries"
```

---

## Task 7: Register the tool, verify, final commit

**Files:**
- Modify: `cmd/fova/main.go`

- [ ] **Step 1: Add the registration line**

Open `cmd/fova/main.go`, find the existing line `registry.Register(knowledge.NewPDB())` (currently line 251), and insert immediately after it:

```go
	registry.Register(knowledge.NewPDBSearch())
```

- [ ] **Step 2: Build the world**

Run: `go build ./...`
Expected: succeeds, no output.

- [ ] **Step 3: Run package tests**

Run: `go test ./internal/tools/knowledge/ ./cmd/fova/ -count=1`
Expected: PASS for both packages.

- [ ] **Step 4: Format check**

Run: `gofmt -l .`
Expected: no output (every changed file already formatted). If anything is listed, run `gofmt -w <file>` and re-run.

- [ ] **Step 5: Commit**

```bash
git add cmd/fova/main.go
git commit -m "feat(knowledge): register knowledge.pdb_search in fova"
```

- [ ] **Step 6: Confirm with the user before merging to `dev`**

Per the brief's "Stop conditions": **do not merge to `dev` automatically.** Surface the branch state to the user and wait for explicit approval.

Run: `git log --oneline feat/knowledge-pdb-search ^main | head -20`
Expected: ~6 commits from Tasks 1-7.

Then ask the user: "All tasks complete, build + tests + gofmt green. Ready to merge `feat/knowledge-pdb-search` into `dev`?"

---

## Self-review

Checked the plan against the spec:

- **Spec §3 (Tool surface)** → Task 2 (interface methods + input schema), Task 3 (output struct + display line).
- **Spec §4.1 (search phase)** → Task 3 step 3 (`buildPDBSearchRequest`).
- **Spec §4.2 (GraphQL enrich)** → Task 3 step 3 (`enrich`).
- **Spec §4.3 (`postJSON`)** → Task 1.
- **Spec §4.4 (file layout)** → File Structure table + Task 7 (registration).
- **Spec §5 (error handling)** — every row accounted for:
  - empty query → Task 2.
  - limit clamping → Task 3 (`clampPDBSearchLimit`). Not explicitly tested; would only matter if the agent passes huge limits, and the unit-level clamp is straightforward. (Acceptable per YAGNI.)
  - 204 → Task 1 (`postJSON`) + Task 5 (end-to-end).
  - non-2xx → Task 1 covers `postJSON` behavior; the `Execute`-level propagation is exercised implicitly by happy path.
  - GraphQL null → Task 6.
  - ctx cancellation → trivially correct via `http.NewRequestWithContext` (no test).
- **Spec §6 (tests)** → Tasks 2, 3, 4, 5, 6 cover all five tests + the interface assertion.
- **Spec §8 (acceptance)** → Task 7.

Placeholder scan: no TBDs, no "handle errors here", no "similar to Task N". All code blocks are concrete.

Type consistency: `pdbSearchHit`, `pdbSearchOutput`, `pdbEnriched`, `PDBSearch`, `NewPDBSearch`, `SearchURL`, `GraphQLURL`, `clampPDBSearchLimit`, `buildPDBSearchRequest`, `textTerminal`, `(*PDBSearch).enrich`, `pdbSearchDisplay` — all names match across tasks and the test file. Field names (`PDBID`, `Score`, `Title`, `Method`, `Resolution`, `Year`) match the spec's output schema and are used consistently in tests.
