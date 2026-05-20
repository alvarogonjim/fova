package jobs

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
	jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// CancelTool implements jobs.cancel.
type CancelTool struct{ mgr *jobmgr.Manager }

// NewCancelTool builds the jobs.cancel tool.
func NewCancelTool(mgr *jobmgr.Manager) *CancelTool { return &CancelTool{mgr: mgr} }

func (*CancelTool) Name() string { return "jobs.cancel" }
func (*CancelTool) Description() string {
	return "Request cancellation of a running job by ID (best-effort)."
}
func (*CancelTool) InputSchema() map[string]any                     { return jobIDSchema() }
func (*CancelTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*CancelTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*CancelTool) EstimatedDuration(json.RawMessage) time.Duration { return 50 * time.Millisecond }

func (t *CancelTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in jobIDInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	display := "cancellation requested for " + in.JobID
	if err := t.mgr.Cancel(domain.JobID(in.JobID)); err != nil {
		// Not a tool failure: the job is simply not running (already finished
		// or unknown). Report it as text the model can act on.
		display = err.Error()
	}
	return tools.Result{
		Display:    display,
		Provenance: domain.NewToolCallRef("jobs.cancel", input),
	}, nil
}
