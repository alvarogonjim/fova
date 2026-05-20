package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
	jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// jobIDInput is the decoded argument shared by the single-job jobs.* tools.
type jobIDInput struct {
	JobID string `json:"job_id"`
}

// StatusTool implements jobs.status.
type StatusTool struct{ mgr *jobmgr.Manager }

// NewStatusTool builds the jobs.status tool.
func NewStatusTool(mgr *jobmgr.Manager) *StatusTool { return &StatusTool{mgr: mgr} }

func (*StatusTool) Name() string { return "jobs.status" }
func (*StatusTool) Description() string {
	return "Report the status, progress, and timing of one job by ID."
}
func (*StatusTool) InputSchema() map[string]any                     { return jobIDSchema() }
func (*StatusTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*StatusTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*StatusTool) EstimatedDuration(json.RawMessage) time.Duration { return 50 * time.Millisecond }

func (t *StatusTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in jobIDInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	j, err := t.mgr.Status(domain.JobID(in.JobID))
	if err != nil {
		return tools.Result{}, fmt.Errorf("job %q not found", in.JobID)
	}
	display := fmt.Sprintf("%s  tool=%s  status=%s  progress=%.0f%%",
		j.ID, j.Tool, j.Status, j.Progress*100)
	if j.Error != "" {
		display += "  error=" + j.Error
	}
	out, _ := json.Marshal(j)
	return tools.Result{
		Output:     out,
		Display:    display,
		Provenance: domain.NewToolCallRef("jobs.status", input),
	}, nil
}
