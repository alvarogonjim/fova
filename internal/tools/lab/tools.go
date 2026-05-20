package lab

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

// --- lab.targets_search ---

// targetsSearchTool exposes the Adaptyv target catalog to the agent.
type targetsSearchTool struct{ c *Client }

// NewTargetsSearchTool returns the lab.targets_search tool.
func NewTargetsSearchTool(c *Client) *targetsSearchTool { return &targetsSearchTool{c: c} }

func (*targetsSearchTool) Name() string { return "lab.targets_search" }
func (*targetsSearchTool) Description() string {
	return "List the Adaptyv Foundry target catalog available for wet-lab assays."
}
func (*targetsSearchTool) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}
func (*targetsSearchTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*targetsSearchTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*targetsSearchTool) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

// Execute fetches the target catalog from Adaptyv.
func (t *targetsSearchTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	targets, err := t.c.ListTargets(ctx)
	if err != nil {
		return tools.Result{}, err
	}
	out, _ := json.Marshal(map[string]any{"targets": targets})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("found %d Adaptyv target(s)", len(targets)),
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// --- lab.cost_estimate ---

// costEstimateTool prices an assay before submission.
type costEstimateTool struct{ c *Client }

// NewCostEstimateTool returns the lab.cost_estimate tool.
func NewCostEstimateTool(c *Client) *costEstimateTool { return &costEstimateTool{c: c} }

func (*costEstimateTool) Name() string { return "lab.cost_estimate" }
func (*costEstimateTool) Description() string {
	return "Estimate the cost and turnaround of an Adaptyv assay before submitting it."
}
func (*costEstimateTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_id":  map[string]any{"type": "string", "description": "Adaptyv target ID"},
			"assay_type": map[string]any{"type": "string", "description": "Assay type to run"},
			"sequences": map[string]any{
				"type":        "array",
				"description": "Design sequences to assay",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":     map[string]any{"type": "string"},
						"sequence": map[string]any{"type": "string"},
					},
					"required": []string{"name", "sequence"},
				},
			},
		},
		"required": []string{"target_id", "assay_type", "sequences"},
	}
}
func (*costEstimateTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*costEstimateTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*costEstimateTool) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

// Execute asks Adaptyv to price the requested assay.
func (t *costEstimateTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var req CostRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, fmt.Errorf("invalid lab.cost_estimate request: %w", err)
	}
	est, err := t.c.EstimateCost(ctx, req)
	if err != nil {
		return tools.Result{}, err
	}
	out, _ := json.Marshal(est)
	return tools.Result{
		Output: out,
		Display: fmt.Sprintf("estimated cost $%.2f, turnaround ~%d days",
			est.TotalUSD, est.TurnaroundDays),
		Cost:       est.TotalUSD,
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// --- lab.experiment_status ---

// experimentStatusTool fetches one Adaptyv experiment's current state.
type experimentStatusTool struct{ c *Client }

// NewExperimentStatusTool returns the lab.experiment_status tool.
func NewExperimentStatusTool(c *Client) *experimentStatusTool {
	return &experimentStatusTool{c: c}
}

func (*experimentStatusTool) Name() string { return "lab.experiment_status" }
func (*experimentStatusTool) Description() string {
	return "Fetch the current status of an Adaptyv experiment by its experiment ID."
}
func (*experimentStatusTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"experiment_id": map[string]any{
				"type":        "string",
				"description": "Adaptyv experiment ID",
			},
		},
		"required": []string{"experiment_id"},
	}
}
func (*experimentStatusTool) RequiresConfirmation(json.RawMessage) bool { return false }
func (*experimentStatusTool) EstimatedCostUSD(json.RawMessage) float64  { return 0 }
func (*experimentStatusTool) EstimatedDuration(json.RawMessage) time.Duration {
	return 5 * time.Second
}

// Execute retrieves the experiment record from Adaptyv.
func (t *experimentStatusTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		ExperimentID string `json:"experiment_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, fmt.Errorf("invalid lab.experiment_status request: %w", err)
	}
	if in.ExperimentID == "" {
		return tools.Result{}, fmt.Errorf("experiment_id is required")
	}
	exp, err := t.c.GetExperiment(ctx, in.ExperimentID)
	if err != nil {
		return tools.Result{}, err
	}
	out, _ := json.Marshal(exp)
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("experiment %s is %q", exp.ID, exp.Status),
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// --- lab.results ---

// resultsTool fetches the measured kinetics of an Adaptyv experiment.
type resultsTool struct{ c *Client }

// NewResultsTool returns the lab.results tool.
func NewResultsTool(c *Client) *resultsTool { return &resultsTool{c: c} }

func (*resultsTool) Name() string { return "lab.results" }
func (*resultsTool) Description() string {
	return "Fetch the measured wet-lab results (kinetics) for a completed Adaptyv experiment."
}
func (*resultsTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"experiment_id": map[string]any{
				"type":        "string",
				"description": "Adaptyv experiment ID",
			},
		},
		"required": []string{"experiment_id"},
	}
}
func (*resultsTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*resultsTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*resultsTool) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

// Execute retrieves the measured results from Adaptyv.
func (t *resultsTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		ExperimentID string `json:"experiment_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, fmt.Errorf("invalid lab.results request: %w", err)
	}
	if in.ExperimentID == "" {
		return tools.Result{}, fmt.Errorf("experiment_id is required")
	}
	results, err := t.c.GetResults(ctx, in.ExperimentID)
	if err != nil {
		return tools.Result{}, err
	}
	out, _ := json.Marshal(map[string]any{"results": results})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("experiment %s has %d result(s)", in.ExperimentID, len(results)),
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// --- lab.submit_experiment ---

// submitExperimentTool submits sequences to Adaptyv and records the experiment.
type submitExperimentTool struct {
	c  *Client
	st *store.Store
}

// NewSubmitExperimentTool returns the lab.submit_experiment tool. It persists a
// domain.Experiment to st on every successful submission.
func NewSubmitExperimentTool(c *Client, st *store.Store) *submitExperimentTool {
	return &submitExperimentTool{c: c, st: st}
}

func (*submitExperimentTool) Name() string { return "lab.submit_experiment" }
func (*submitExperimentTool) Description() string {
	return "Submit design sequences to Adaptyv Foundry for a wet-lab assay against a target."
}
func (*submitExperimentTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_id":  map[string]any{"type": "string", "description": "Adaptyv target ID"},
			"assay_type": map[string]any{"type": "string", "description": "Assay type to run"},
			"sequences": map[string]any{
				"type":        "array",
				"description": "Design sequences to submit",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":     map[string]any{"type": "string"},
						"sequence": map[string]any{"type": "string"},
					},
					"required": []string{"name", "sequence"},
				},
			},
			"webhook_url": map[string]any{
				"type":        "string",
				"description": "Optional URL Adaptyv calls when results are ready",
			},
		},
		"required": []string{"target_id", "assay_type", "sequences"},
	}
}

// Submitting a wet-lab experiment spends real money — always confirm.
func (*submitExperimentTool) RequiresConfirmation(json.RawMessage) bool { return true }
func (*submitExperimentTool) EstimatedCostUSD(json.RawMessage) float64  { return 0 }
func (*submitExperimentTool) EstimatedDuration(json.RawMessage) time.Duration {
	return 10 * time.Second
}

// Execute submits the assay to Adaptyv and persists a domain.Experiment record
// carrying the returned Adaptyv id in ExternalID.
func (t *submitExperimentTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var req SubmitRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, fmt.Errorf("invalid lab.submit_experiment request: %w", err)
	}
	exp, err := t.c.SubmitExperiment(ctx, req)
	if err != nil {
		return tools.Result{}, err
	}

	record := domain.Experiment{
		ID:          domain.ExperimentID(uuid.NewString()),
		ProjectID:   store.DefaultProjectID,
		Backend:     "adaptyv",
		ExternalID:  exp.ID,
		AssayType:   firstNonEmpty(exp.AssayType, req.AssayType),
		TargetID:    firstNonEmpty(exp.TargetID, req.TargetID),
		TargetName:  exp.TargetName,
		SubmittedAt: time.Now().UTC(),
		Status:      firstNonEmpty(exp.Status, "submitted"),
		CostUSD:     exp.CostUSD,
	}
	if t.st != nil {
		if err := t.st.InsertExperiment(record); err != nil {
			return tools.Result{}, fmt.Errorf("persist experiment: %w", err)
		}
	}

	out, _ := json.Marshal(map[string]any{
		"experiment_id": string(record.ID),
		"external_id":   exp.ID,
		"status":        record.Status,
	})
	return tools.Result{
		Output: out,
		Display: fmt.Sprintf("submitted experiment %s to Adaptyv (external id %s)",
			record.ID, exp.ID),
		Cost:       exp.CostUSD,
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// firstNonEmpty returns the first non-empty string of its arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
