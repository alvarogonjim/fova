package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

const openAlexEndpoint = "https://api.openalex.org/works"

// OpenAlex implements knowledge.openalex: free scholarly-works search.
type OpenAlex struct {
	BaseURL string
	Mailto  string
	results *Results
}

// NewOpenAlex builds the knowledge.openalex tool. mailto, when non-empty, is
// sent to the OpenAlex polite pool.
func NewOpenAlex(r *Results, mailto string) *OpenAlex {
	return &OpenAlex{BaseURL: openAlexEndpoint, Mailto: mailto, results: r}
}

func (*OpenAlex) Name() string { return "knowledge.openalex" }
func (*OpenAlex) Description() string {
	return "Search scholarly works via the free OpenAlex API."
}
func (*OpenAlex) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
			"limit": map[string]any{"type": "integer", "description": "Max results (default 25, max 100)"},
		},
		"required": []string{"query"},
	}
}
func (*OpenAlex) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*OpenAlex) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*OpenAlex) EstimatedDuration(json.RawMessage) time.Duration { return 3 * time.Second }

func (t *OpenAlex) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.Query == "" {
		return tools.Result{}, fmt.Errorf("knowledge.openalex: query is required")
	}
	limit := clampLimit(in.Limit)
	q := url.Values{}
	q.Set("search", in.Query)
	q.Set("per-page", strconv.Itoa(limit))
	if t.Mailto != "" {
		q.Set("mailto", t.Mailto)
	}

	var raw struct {
		Results []struct {
			DOI             string `json:"doi"`
			Title           string `json:"title"`
			PublicationYear int    `json:"publication_year"`
		} `json:"results"`
	}
	if err := getJSON(ctx, t.BaseURL+"?"+q.Encode(), &raw); err != nil {
		return tools.Result{}, err
	}
	papers := make([]Paper, 0, len(raw.Results))
	for _, r := range raw.Results {
		papers = append(papers, Paper{
			ID: r.DOI, Title: r.Title, Year: r.PublicationYear, Source: "openalex",
		})
	}
	resultsID := t.results.Put("openalex", papers)
	out, _ := json.Marshal(map[string]any{
		"results_id": resultsID, "count": len(papers), "papers": papers,
	})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("openalex: %d papers (results_id %s)", len(papers), resultsID),
		Provenance: domain.NewToolCallRef("knowledge.openalex", input),
	}, nil
}
