// Package score provides the agent's design-scoring and shortlisting tools.
package score

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// Default shortlist thresholds (SPECS §8.2, filter-thresholds.md).
const (
	defaultMinIPSAE    = 0.50
	defaultMinPLDDT    = 80.0
	defaultMinPLDDTMin = 60.0
)

// FilterTool implements score.filter: shortlist persisted designs by threshold,
// ranked by ipSAE first.
type FilterTool struct{ store *store.Store }

// NewFilterTool builds the score.filter tool.
func NewFilterTool(st *store.Store) *FilterTool { return &FilterTool{store: st} }

func (*FilterTool) Name() string { return "score.filter" }
func (*FilterTool) Description() string {
	return "Shortlist designs that pass quality thresholds, ranked by ipSAE."
}
func (*FilterTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"filters": map[string]any{
				"type":        "object",
				"description": "FilterConfig overrides (min_ipsae, min_plddt, ...)",
			},
		},
	}
}
func (*FilterTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*FilterTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*FilterTool) EstimatedDuration(json.RawMessage) time.Duration { return 100 * time.Millisecond }

func (t *FilterTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Filters domain.FilterConfig `json:"filters"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &in); err != nil {
			return tools.Result{}, err
		}
	}
	f := withDefaults(in.Filters)

	designs, err := t.store.ListDesigns(store.DefaultProjectID)
	if err != nil {
		return tools.Result{}, err
	}
	var passed []domain.Design
	for _, d := range designs {
		if passesFilter(d, f) {
			passed = append(passed, d)
		}
	}
	// Rank by ipSAE descending.
	sort.SliceStable(passed, func(i, j int) bool {
		return passed[i].Scores["ipsae"] > passed[j].Scores["ipsae"]
	})

	out, _ := json.Marshal(map[string]any{"shortlist": passed})
	return tools.Result{
		Output: out,
		Display: fmt.Sprintf("shortlist: %d of %d designs pass (ranked by ipSAE)",
			len(passed), len(designs)),
		Provenance: domain.NewToolCallRef("score.filter", input),
	}, nil
}

// withDefaults fills any zero-valued threshold with the SPECS §8.2 default.
func withDefaults(f domain.FilterConfig) domain.FilterConfig {
	if f.MinIPSAE == 0 {
		f.MinIPSAE = defaultMinIPSAE
	}
	if f.MinPLDDT == 0 {
		f.MinPLDDT = defaultMinPLDDT
	}
	if f.MinPLDDTMin == 0 {
		f.MinPLDDTMin = defaultMinPLDDTMin
	}
	return f
}

// passesFilter reports whether a design clears the required thresholds.
func passesFilter(d domain.Design, f domain.FilterConfig) bool {
	if d.Scores["ipsae"] < f.MinIPSAE {
		return false
	}
	if pl, ok := d.Scores["plddt_mean"]; ok && pl < f.MinPLDDT {
		return false
	}
	if plm, ok := d.Scores["plddt_min"]; ok && plm < f.MinPLDDTMin {
		return false
	}
	return true
}
