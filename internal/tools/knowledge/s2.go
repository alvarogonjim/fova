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

const s2Endpoint = "https://api.semanticscholar.org/graph/v1/paper/search"

// S2 implements knowledge.s2: free Semantic Scholar paper search.
type S2 struct {
	BaseURL string
	results *Results
}

// NewS2 builds the knowledge.s2 tool.
func NewS2(r *Results) *S2 {
	return &S2{BaseURL: s2Endpoint, results: r}
}

func (*S2) Name() string     { return "knowledge.s2" }
func (*S2) Concurrent() bool { return true }
func (*S2) Description() string {
	return "Search papers via the free Semantic Scholar Graph API."
}
func (*S2) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
			"limit": map[string]any{"type": "integer", "description": "Max results (default 25, max 100)"},
		},
		"required": []string{"query"},
	}
}
func (*S2) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*S2) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*S2) EstimatedDuration(json.RawMessage) time.Duration { return 3 * time.Second }

func (t *S2) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.Query == "" {
		return tools.Result{}, fmt.Errorf("knowledge.s2: query is required")
	}
	limit := clampLimit(in.Limit)
	q := url.Values{}
	q.Set("query", in.Query)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("fields", "title,year,abstract,externalIds")

	var raw struct {
		Data []struct {
			Title       string `json:"title"`
			Year        int    `json:"year"`
			Abstract    string `json:"abstract"`
			ExternalIDs struct {
				DOI string `json:"DOI"`
			} `json:"externalIds"`
		} `json:"data"`
	}
	if err := getJSON(ctx, t.BaseURL+"?"+q.Encode(), &raw); err != nil {
		return tools.Result{}, err
	}
	papers := make([]Paper, 0, len(raw.Data))
	for _, r := range raw.Data {
		papers = append(papers, Paper{
			ID: r.ExternalIDs.DOI, Title: r.Title, Year: r.Year,
			Abstract: r.Abstract, Source: "s2",
		})
	}
	resultsID := t.results.Put("s2", papers)
	out, _ := json.Marshal(map[string]any{
		"results_id": resultsID, "count": len(papers), "papers": papers,
	})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("s2: %d papers (results_id %s)", len(papers), resultsID),
		Provenance: domain.NewToolCallRef("knowledge.s2", input),
	}, nil
}
