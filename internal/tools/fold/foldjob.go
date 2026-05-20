package fold

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/alvarogonjim/proteus/internal/backends"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// foldJobTool is the shared implementation of every Modal-backed structure
// predictor (fold.boltz2, fold.chai1). Unlike fold.esmfold — a synchronous
// HTTP monomer folder — these run on GPUs and may take minutes, so each call
// submits a background job and returns its ID immediately. They are structure
// PREDICTORS, not design generators: the job's output is the backend's raw
// response and nothing is persisted to the design store.
type foldJobTool struct {
	name        string
	description string
	mgr         *jobs.Manager
	backend     backends.Backend
}

func (t *foldJobTool) Name() string        { return t.name }
func (t *foldJobTool) Description() string { return t.description }

// InputSchema accepts a map of chain id → amino-acid sequence and an optional
// workspace-relative output path.
func (*foldJobTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sequences": map[string]any{
				"type":        "object",
				"description": "Chain id → amino-acid sequence for each chain in the complex",
				"additionalProperties": map[string]any{
					"type":    "string",
					"pattern": "^[ACDEFGHIKLMNPQRSTVWY]+$",
				},
			},
			"save_as": map[string]any{
				"type":        "string",
				"description": "Optional structure output path within the workspace",
			},
		},
		"required": []string{"sequences"},
	}
}

// Structure prediction is cheap and unattended — no confirmation needed.
func (*foldJobTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*foldJobTool) EstimatedCostUSD(json.RawMessage) float64        { return 0.25 }
func (*foldJobTool) EstimatedDuration(json.RawMessage) time.Duration { return 3 * time.Minute }

// Execute validates the request, submits a background job, and returns its ID
// immediately. The job runs the backend and returns its raw output; no design
// records are persisted (these tools predict structures, they do not design).
func (t *foldJobTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var probe struct {
		Sequences map[string]string `json:"sequences"`
	}
	if err := json.Unmarshal(input, &probe); err != nil {
		return tools.Result{}, fmt.Errorf("invalid %s request: %w", t.name, err)
	}
	if len(probe.Sequences) == 0 {
		return tools.Result{}, fmt.Errorf("%s request needs at least one chain in \"sequences\"", t.name)
	}
	jobID, err := t.mgr.Submit(jobs.Spec{
		Kind:    domain.JobCompute,
		Tool:    t.name,
		Backend: t.backend.Name(),
		Input:   input,
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			out, err := t.backend.Run(ctx, t.name, input, log)
			if err != nil {
				return nil, err
			}
			progress(1)
			return out, nil
		},
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		JobID: jobID,
		Display: fmt.Sprintf("started %s job %s — poll jobs.result for the predicted structure",
			t.name, jobID),
		Provenance: domain.NewToolCallRef(t.name, input),
	}, nil
}
