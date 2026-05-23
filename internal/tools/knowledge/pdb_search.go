package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
