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

// uniprotBaseURL is the public UniProtKB single-entry endpoint (SPECS §7.2.5).
const uniprotBaseURL = "https://rest.uniprot.org/uniprotkb"

// UniProt implements the knowledge.uniprot lookup tool.
type UniProt struct {
	BaseURL string // overridable for tests; no trailing slash, no ".json"
}

// NewUniProt returns the knowledge.uniprot tool.
func NewUniProt() *UniProt { return &UniProt{BaseURL: uniprotBaseURL} }

func (*UniProt) Name() string       { return "knowledge.uniprot" }
func (*UniProt) Concurrent() bool { return true }
func (*UniProt) Description() string {
	return "Look up a UniProtKB entry by accession and return its name, organism, and sequence."
}
func (*UniProt) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"accession": map[string]any{
				"type":        "string",
				"description": "UniProtKB accession, e.g. P0DTC2",
			},
		},
		"required": []string{"accession"},
	}
}
func (*UniProt) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*UniProt) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*UniProt) EstimatedDuration(json.RawMessage) time.Duration { return 2 * time.Second }

// uniprotEntry mirrors the parts of the UniProtKB JSON response we use.
type uniprotEntry struct {
	PrimaryAccession   string `json:"primaryAccession"`
	ProteinDescription struct {
		RecommendedName struct {
			FullName struct {
				Value string `json:"value"`
			} `json:"fullName"`
		} `json:"recommendedName"`
	} `json:"proteinDescription"`
	Organism struct {
		ScientificName string `json:"scientificName"`
	} `json:"organism"`
	Sequence struct {
		Value  string `json:"value"`
		Length int    `json:"length"`
	} `json:"sequence"`
}

type uniprotOutput struct {
	Accession string `json:"accession"`
	Name      string `json:"name"`
	Organism  string `json:"organism"`
	Sequence  string `json:"sequence"`
	Length    int    `json:"length"`
}

// Execute fetches a UniProtKB entry and returns its normalized fields.
func (u *UniProt) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Accession string `json:"accession"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.Accession == "" {
		return tools.Result{}, fmt.Errorf("knowledge.uniprot: accession is required")
	}

	url := u.BaseURL + "/" + url.PathEscape(in.Accession) + ".json"
	var entry uniprotEntry
	if err := getJSON(ctx, url, &entry); err != nil {
		return tools.Result{}, err
	}

	out := uniprotOutput{
		Accession: entry.PrimaryAccession,
		Name:      entry.ProteinDescription.RecommendedName.FullName.Value,
		Organism:  entry.Organism.ScientificName,
		Sequence:  entry.Sequence.Value,
		Length:    entry.Sequence.Length,
	}
	if out.Accession == "" {
		out.Accession = in.Accession
	}
	outJSON, _ := json.Marshal(out)
	return tools.Result{
		Output: outJSON,
		Display: fmt.Sprintf("%s — %s (%s, %d aa)",
			out.Accession, out.Name, out.Organism, out.Length),
		Provenance: domain.NewToolCallRef("knowledge.uniprot", input),
	}, nil
}
