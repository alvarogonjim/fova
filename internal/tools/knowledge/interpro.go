package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// interproBaseURL is the public InterPro protein-entry endpoint (SPECS §7.2.5).
const interproBaseURL = "https://www.ebi.ac.uk/interpro/api/entry/InterPro/protein/uniprot"

// InterPro implements the knowledge.interpro lookup tool.
type InterPro struct {
	BaseURL string // overridable for tests; no trailing slash
}

// NewInterPro returns the knowledge.interpro tool.
func NewInterPro() *InterPro { return &InterPro{BaseURL: interproBaseURL} }

func (*InterPro) Name() string { return "knowledge.interpro" }
func (*InterPro) Description() string {
	return "List the InterPro domains and families annotated on a protein by its UniProt accession."
}
func (*InterPro) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"accession": map[string]any{
				"type":        "string",
				"description": "UniProt accession, e.g. P00533",
			},
		},
		"required": []string{"accession"},
	}
}
func (*InterPro) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*InterPro) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*InterPro) EstimatedDuration(json.RawMessage) time.Duration { return 3 * time.Second }

// interproResponse mirrors the parts of the InterPro JSON response we use.
type interproResponse struct {
	Results []struct {
		Metadata struct {
			Accession string `json:"accession"`
			Name      string `json:"name"`
			Type      string `json:"type"`
		} `json:"metadata"`
	} `json:"results"`
}

type interproDomain struct {
	InterProID string `json:"interpro_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
}

type interproOutput struct {
	Accession string           `json:"accession"`
	Domains   []interproDomain `json:"domains"`
}

// Execute fetches the InterPro domain annotations for a UniProt accession.
func (i *InterPro) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Accession string `json:"accession"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.Accession == "" {
		return tools.Result{}, fmt.Errorf("knowledge.interpro: accession is required")
	}

	url := i.BaseURL + "/" + url.PathEscape(in.Accession)
	var resp interproResponse
	if err := getJSON(ctx, url, &resp); err != nil {
		return tools.Result{}, err
	}

	domains := make([]interproDomain, 0, len(resp.Results))
	for _, r := range resp.Results {
		domains = append(domains, interproDomain{
			InterProID: r.Metadata.Accession,
			Name:       r.Metadata.Name,
			Type:       r.Metadata.Type,
		})
	}
	out := interproOutput{Accession: in.Accession, Domains: domains}
	outJSON, _ := json.Marshal(out)
	return tools.Result{
		Output:     outJSON,
		Display:    fmt.Sprintf("%s — %d InterPro entries", in.Accession, len(domains)),
		Provenance: domain.NewToolCallRef("knowledge.interpro", input),
	}, nil
}
