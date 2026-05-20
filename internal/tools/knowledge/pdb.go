package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// pdbBaseURL is the public RCSB PDB core-entry endpoint (SPECS §7.2.5).
const pdbBaseURL = "https://data.rcsb.org/rest/v1/core/entry"

// PDB implements the knowledge.pdb lookup tool.
type PDB struct {
	BaseURL string // overridable for tests; no trailing slash
}

// NewPDB returns the knowledge.pdb tool.
func NewPDB() *PDB { return &PDB{BaseURL: pdbBaseURL} }

func (*PDB) Name() string { return "knowledge.pdb" }
func (*PDB) Description() string {
	return "Look up an RCSB PDB entry by ID and return its title, experimental method, and resolution."
}
func (*PDB) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pdb_id": map[string]any{
				"type":        "string",
				"description": "4-character PDB entry ID, e.g. 2LZM",
			},
		},
		"required": []string{"pdb_id"},
	}
}
func (*PDB) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*PDB) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*PDB) EstimatedDuration(json.RawMessage) time.Duration { return 2 * time.Second }

// pdbEntry mirrors the parts of the RCSB core-entry JSON response we use.
type pdbEntry struct {
	Struct struct {
		Title string `json:"title"`
	} `json:"struct"`
	Exptl []struct {
		Method string `json:"method"`
	} `json:"exptl"`
	RCSBEntryInfo struct {
		ResolutionCombined []float64 `json:"resolution_combined"`
	} `json:"rcsb_entry_info"`
}

type pdbOutput struct {
	PDBID      string  `json:"pdb_id"`
	Title      string  `json:"title"`
	Method     string  `json:"method"`
	Resolution float64 `json:"resolution"`
}

// Execute fetches an RCSB PDB entry and returns its normalized fields.
func (p *PDB) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		PDBID string `json:"pdb_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.PDBID == "" {
		return tools.Result{}, fmt.Errorf("knowledge.pdb: pdb_id is required")
	}

	url := p.BaseURL + "/" + url.PathEscape(in.PDBID)
	var entry pdbEntry
	if err := getJSON(ctx, url, &entry); err != nil {
		return tools.Result{}, err
	}

	out := pdbOutput{PDBID: in.PDBID, Title: entry.Struct.Title}
	if len(entry.Exptl) > 0 {
		out.Method = entry.Exptl[0].Method
	}
	if len(entry.RCSBEntryInfo.ResolutionCombined) > 0 {
		out.Resolution = entry.RCSBEntryInfo.ResolutionCombined[0]
	}
	outJSON, _ := json.Marshal(out)
	return tools.Result{
		Output: outJSON,
		Display: fmt.Sprintf("%s — %s (%s, %.2f Å)",
			out.PDBID, out.Title, out.Method, out.Resolution),
		Provenance: domain.NewToolCallRef("knowledge.pdb", input),
	}, nil
}
