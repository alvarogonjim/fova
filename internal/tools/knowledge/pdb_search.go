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

func (*PDBSearch) Name() string     { return "knowledge.pdb_search" }
func (*PDBSearch) Concurrent() bool { return true }
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
		if d := e.RCSBEntryInfo.InitialReleaseDate; len(d) >= 4 {
			if y, err := time.Parse(time.RFC3339, d); err == nil {
				enriched.Year = y.Year()
			} else if y, err := time.Parse(time.DateOnly, d); err == nil {
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
	if r := []rune(title); len(r) > 40 {
		title = string(r[:37]) + "..."
	}
	if top.Resolution > 0 {
		return fmt.Sprintf("pdb_search %q: %d of %d hits (top: %s %s %.2f Å)",
			query, out.Count, out.TotalHits, top.PDBID, title, top.Resolution)
	}
	return fmt.Sprintf("pdb_search %q: %d of %d hits (top: %s %s)",
		query, out.Count, out.TotalHits, top.PDBID, title)
}
