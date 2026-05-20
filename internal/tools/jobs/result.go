package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	jobmgr "github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/tools"
)

// ResultTool implements jobs.result.
type ResultTool struct {
	mgr  *jobmgr.Manager
	estd EstimatedDurationFn
}

// NewResultTool builds the jobs.result tool. estd may be nil; when non-nil it
// is used to enrich the "still running" display with the job's `estimated`
// field, mirroring jobs.status.
func NewResultTool(mgr *jobmgr.Manager, estd EstimatedDurationFn) *ResultTool {
	return &ResultTool{mgr: mgr, estd: estd}
}

func (*ResultTool) Name() string { return "jobs.result" }
func (*ResultTool) Description() string {
	return "Fetch a job's final result. If the job is still running, reports its progress instead."
}
func (*ResultTool) InputSchema() map[string]any                     { return jobIDSchema() }
func (*ResultTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*ResultTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*ResultTool) EstimatedDuration(json.RawMessage) time.Duration { return 50 * time.Millisecond }

func (t *ResultTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in jobIDInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	j, err := t.mgr.Result(domain.JobID(in.JobID))
	if err != nil {
		return tools.Result{}, fmt.Errorf("job %q not found", in.JobID)
	}
	var display string
	switch j.Status {
	case domain.JobSucceeded:
		display = "job " + string(j.ID) + " succeeded. output: " + string(j.Output)
		if e := elapsedOf(j); e != "" {
			display += "  elapsed=" + e
		}
	case domain.JobFailed:
		display = "job " + string(j.ID) + " failed: " + j.Error
		if e := elapsedOf(j); e != "" {
			display += "  elapsed=" + e
		}
	case domain.JobCancelled:
		display = "job " + string(j.ID) + " was cancelled"
		if e := elapsedOf(j); e != "" {
			display += "  elapsed=" + e
		}
	default:
		display = fmt.Sprintf("job %s is still %s (progress %.0f%%) — poll again later",
			j.ID, j.Status, j.Progress*100)
		if e := elapsedOf(j); e != "" {
			display += "  elapsed=" + e
		}
		if est := estimatedOf(j, t.estd); est != "" {
			display += "  estimated=" + est
		}
	}
	out, _ := json.Marshal(j)
	return tools.Result{
		Output:     out,
		Display:    display,
		Provenance: domain.NewToolCallRef("jobs.result", input),
	}, nil
}
