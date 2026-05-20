// Package plan provides the agent's design-planning tools.
package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// TODO(spec): SPECS §7 has no plan.* tool table; see §8.2 plan-from-target.md and §20 v0.3 AC1.

// CreateTool implements plan.create: build a DesignPlan from a target and
// persist it for the user to review and approve.
type CreateTool struct{ store *store.Store }

// NewPlanCreateTool builds the plan.create tool.
func NewPlanCreateTool(st *store.Store) tools.Tool { return &CreateTool{store: st} }

func (*CreateTool) Name() string { return "plan.create" }
func (*CreateTool) Description() string {
	return "Creates and persists a DesignPlan from a target for the user to review and approve."
}

func (*CreateTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "object",
				"description": "The design target structure.",
				"properties": map[string]any{
					"pdb_id":    map[string]any{"type": "string"},
					"file_path": map[string]any{"type": "string"},
					"chain":     map[string]any{"type": "string"},
				},
			},
			"application": map[string]any{
				"type":        "string",
				"enum":        []any{"binder", "antibody", "enzyme", "redesign"},
				"description": "The protein-design application area.",
			},
			"method": map[string]any{
				"type":        "string",
				"description": "The primary design method/tool to run.",
			},
			"fallback_method": map[string]any{
				"type":        "string",
				"description": "An optional fallback design method.",
			},
			"filters": map[string]any{
				"type":        "object",
				"description": "FilterConfig thresholds for shortlisting (min_ipsae, min_plddt, ...).",
			},
			"shortlist_size": map[string]any{
				"type":        "integer",
				"description": "Number of designs to keep on the shortlist.",
			},
			"compute_backend": map[string]any{
				"type":        "string",
				"description": "The compute backend the plan should run on.",
			},
			"estimated_cost_usd": map[string]any{
				"type":        "number",
				"description": "Estimated total cost in USD.",
			},
			"estimated_time": map[string]any{
				"type":        "string",
				"description": "Human-readable estimated wall-clock time.",
			},
			"rationale": map[string]any{
				"type":        "string",
				"description": "Why this plan was chosen.",
			},
			"evidence_papers": map[string]any{
				"type":        "array",
				"description": "Supporting literature references.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"doi":   map[string]any{"type": "string"},
						"title": map[string]any{"type": "string"},
						"year":  map[string]any{"type": "integer"},
						"url":   map[string]any{"type": "string"},
					},
				},
			},
		},
		"required": []any{"target", "application", "method"},
	}
}

func (*CreateTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*CreateTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*CreateTool) EstimatedDuration(json.RawMessage) time.Duration { return 100 * time.Millisecond }

func (t *CreateTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var p domain.DesignPlan
	if err := json.Unmarshal(input, &p); err != nil {
		return tools.Result{}, fmt.Errorf("plan.create: invalid input: %w", err)
	}

	switch p.Application {
	case domain.AppBinder, domain.AppAntibody, domain.AppEnzyme, domain.AppRedesign:
		// valid
	default:
		return tools.Result{}, fmt.Errorf(
			"plan.create: application %q must be one of binder, antibody, enzyme, redesign",
			p.Application)
	}
	if p.Method == "" {
		return tools.Result{}, fmt.Errorf("plan.create: method is required")
	}

	// Server-controlled fields.
	p.ID = domain.PlanID("p_" + uuid.NewString())
	p.ProjectID = store.DefaultProjectID
	p.Created = time.Now().UTC()
	p.Approved = false
	p.ApprovedAt = nil

	if err := t.store.InsertPlan(p); err != nil {
		return tools.Result{}, fmt.Errorf("plan.create: persist plan: %w", err)
	}

	out, err := json.Marshal(p)
	if err != nil {
		return tools.Result{}, fmt.Errorf("plan.create: marshal plan: %w", err)
	}
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("created plan %s — review it with /plan", p.ID),
		Provenance: domain.NewToolCallRef("plan.create", input),
	}, nil
}
