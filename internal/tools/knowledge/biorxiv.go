package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

const bioRxivEndpoint = "https://api.biorxiv.org/details/biorxiv"

// BioRxiv implements knowledge.biorxiv: free preprint listing by date range.
type BioRxiv struct {
	BaseURL    string
	RecentDays int
	results    *Results
}

// NewBioRxiv builds the knowledge.biorxiv tool. recentDays is the default
// look-back window; a value <= 0 falls back to 30.
func NewBioRxiv(r *Results, recentDays int) *BioRxiv {
	return &BioRxiv{BaseURL: bioRxivEndpoint, RecentDays: recentDays, results: r}
}

func (*BioRxiv) Name() string { return "knowledge.biorxiv" }
func (*BioRxiv) Description() string {
	return "List bioRxiv preprints posted within a date range via the free bioRxiv API."
}
func (*BioRxiv) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"from": map[string]any{"type": "string", "description": "Start date YYYY-MM-DD (default 30 days before 'to')"},
			"to":   map[string]any{"type": "string", "description": "End date YYYY-MM-DD (default today)"},
		},
	}
}
func (*BioRxiv) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*BioRxiv) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*BioRxiv) EstimatedDuration(json.RawMessage) time.Duration { return 3 * time.Second }

func (t *BioRxiv) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	to := in.To
	if to == "" {
		to = time.Now().UTC().Format("2006-01-02")
	}
	from := in.From
	if from == "" {
		toTime, err := time.Parse("2006-01-02", to)
		if err != nil {
			return tools.Result{}, fmt.Errorf("knowledge.biorxiv: invalid 'to' date %q: %w", to, err)
		}
		days := t.RecentDays
		if days <= 0 {
			days = 30
		}
		from = toTime.AddDate(0, 0, -days).Format("2006-01-02")
	}

	var raw struct {
		Collection []struct {
			DOI     string `json:"doi"`
			Title   string `json:"title"`
			Authors string `json:"authors"`
			Date    string `json:"date"`
		} `json:"collection"`
	}
	reqURL := t.BaseURL + "/" + from + "/" + to
	if err := getJSON(ctx, reqURL, &raw); err != nil {
		return tools.Result{}, err
	}
	papers := make([]Paper, 0, len(raw.Collection))
	for _, r := range raw.Collection {
		var year int
		if len(r.Date) >= 4 {
			year, _ = strconv.Atoi(r.Date[:4])
		}
		papers = append(papers, Paper{
			ID: r.DOI, Title: r.Title, Authors: r.Authors,
			Year: year, Source: "biorxiv",
		})
	}
	resultsID := t.results.Put("biorxiv", papers)
	out, _ := json.Marshal(map[string]any{
		"results_id": resultsID, "count": len(papers), "papers": papers,
	})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("biorxiv: %d papers (results_id %s)", len(papers), resultsID),
		Provenance: domain.NewToolCallRef("knowledge.biorxiv", input),
	}, nil
}
