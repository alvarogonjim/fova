package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/tools"
)

const europePMCEndpoint = "https://www.ebi.ac.uk/europepmc/webservices/rest/search"

// EuropePMC implements knowledge.europepmc: free literature search.
type EuropePMC struct {
	BaseURL string
	results *Results
}

// NewEuropePMC builds the knowledge.europepmc tool.
func NewEuropePMC(r *Results) *EuropePMC {
	return &EuropePMC{BaseURL: europePMCEndpoint, results: r}
}

func (*EuropePMC) Name() string { return "knowledge.europepmc" }
func (*EuropePMC) Description() string {
	return "Search the biomedical literature via the free Europe PMC REST API."
}
func (*EuropePMC) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
			"limit": map[string]any{"type": "integer", "description": "Max results (default 25, max 100)"},
		},
		"required": []string{"query"},
	}
}
func (*EuropePMC) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*EuropePMC) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*EuropePMC) EstimatedDuration(json.RawMessage) time.Duration { return 3 * time.Second }

func (t *EuropePMC) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.Query == "" {
		return tools.Result{}, fmt.Errorf("knowledge.europepmc: query is required")
	}
	limit := clampLimit(in.Limit)
	q := url.Values{}
	q.Set("query", in.Query)
	q.Set("format", "json")
	q.Set("pageSize", strconv.Itoa(limit))

	var raw struct {
		ResultList struct {
			Result []struct {
				DOI          string `json:"doi"`
				PMCID        string `json:"pmcid"`
				Title        string `json:"title"`
				AuthorString string `json:"authorString"`
				PubYear      string `json:"pubYear"`
				AbstractText string `json:"abstractText"`
			} `json:"result"`
		} `json:"resultList"`
	}
	if err := getJSON(ctx, t.BaseURL+"?"+q.Encode(), &raw); err != nil {
		return tools.Result{}, err
	}
	papers := make([]Paper, 0, len(raw.ResultList.Result))
	for _, r := range raw.ResultList.Result {
		id := r.DOI
		if id == "" {
			id = r.PMCID
		}
		year, _ := strconv.Atoi(r.PubYear)
		papers = append(papers, Paper{
			ID: id, Title: r.Title, Authors: r.AuthorString,
			Year: year, Source: "europepmc", Abstract: r.AbstractText,
		})
	}
	resultsID := t.results.Put("europepmc", papers)
	out, _ := json.Marshal(map[string]any{
		"results_id": resultsID, "count": len(papers), "papers": papers,
	})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("europepmc: %d papers (results_id %s)", len(papers), resultsID),
		Provenance: domain.NewToolCallRef("knowledge.europepmc", input),
	}, nil
}
