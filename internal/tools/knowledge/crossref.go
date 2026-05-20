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

const crossrefEndpoint = "https://api.crossref.org/works"

// Crossref implements knowledge.crossref: free DOI-registry metadata search.
type Crossref struct {
	BaseURL string
	results *Results
}

// NewCrossref builds the knowledge.crossref tool.
func NewCrossref(r *Results) *Crossref {
	return &Crossref{BaseURL: crossrefEndpoint, results: r}
}

func (*Crossref) Name() string { return "knowledge.crossref" }
func (*Crossref) Description() string {
	return "Search scholarly metadata via the free Crossref REST API."
}
func (*Crossref) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
			"limit": map[string]any{"type": "integer", "description": "Max results (default 25, max 100)"},
		},
		"required": []string{"query"},
	}
}
func (*Crossref) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*Crossref) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*Crossref) EstimatedDuration(json.RawMessage) time.Duration { return 3 * time.Second }

func (t *Crossref) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.Query == "" {
		return tools.Result{}, fmt.Errorf("knowledge.crossref: query is required")
	}
	limit := clampLimit(in.Limit)
	q := url.Values{}
	q.Set("query", in.Query)
	q.Set("rows", strconv.Itoa(limit))

	var raw struct {
		Message struct {
			Items []struct {
				DOI       string   `json:"DOI"`
				Title     []string `json:"title"`
				Published struct {
					DateParts [][]int `json:"date-parts"`
				} `json:"published"`
			} `json:"items"`
		} `json:"message"`
	}
	if err := getJSON(ctx, t.BaseURL+"?"+q.Encode(), &raw); err != nil {
		return tools.Result{}, err
	}
	papers := make([]Paper, 0, len(raw.Message.Items))
	for _, it := range raw.Message.Items {
		var title string
		if len(it.Title) > 0 {
			title = it.Title[0]
		}
		var year int
		if len(it.Published.DateParts) > 0 && len(it.Published.DateParts[0]) > 0 {
			year = it.Published.DateParts[0][0]
		}
		papers = append(papers, Paper{
			ID: it.DOI, Title: title, Year: year, Source: "crossref",
		})
	}
	resultsID := t.results.Put("crossref", papers)
	out, _ := json.Marshal(map[string]any{
		"results_id": resultsID, "count": len(papers), "papers": papers,
	})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("crossref: %d papers (results_id %s)", len(papers), resultsID),
		Provenance: domain.NewToolCallRef("knowledge.crossref", input),
	}, nil
}
