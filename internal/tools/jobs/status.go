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

// jobIDInput is the decoded argument shared by the single-job jobs.* tools.
type jobIDInput struct {
	JobID string `json:"job_id"`
}

// EstimatedDurationFn returns the advertised EstimatedDuration for the tool
// with the given name. Implementations should return 0 when the tool is not
// known (in which case the `estimated` field is omitted from the status row).
// Passing nil is also legal — callers that don't have a registry handle simply
// don't surface estimates.
type EstimatedDurationFn func(toolName string) time.Duration

// StatusTool implements jobs.status.
type StatusTool struct {
	mgr  *jobmgr.Manager
	estd EstimatedDurationFn
}

// NewStatusTool builds the jobs.status tool. estd may be nil; when non-nil it
// is used to enrich the status row with the running job's `estimated` field.
func NewStatusTool(mgr *jobmgr.Manager, estd EstimatedDurationFn) *StatusTool {
	return &StatusTool{mgr: mgr, estd: estd}
}

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
	if e := elapsedOf(j); e != "" {
		display += "  elapsed=" + e
	}
	if est := estimatedOf(j, t.estd); est != "" {
		display += "  estimated=" + est
	}
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

// elapsedOf returns the elapsed wall-clock duration since the job started, or
// an empty string if the job hasn't started yet. For terminal jobs we measure
// against Finished so the elapsed field reflects the actual run time rather
// than time since job start.
func elapsedOf(j domain.Job) string {
	if j.Started == nil {
		return ""
	}
	end := time.Now()
	if j.Finished != nil {
		end = *j.Finished
	}
	d := end.Sub(*j.Started)
	if d < 0 {
		d = 0
	}
	return d.Round(time.Second).String()
}

// estimatedOf returns the advertised EstimatedDuration for the job's tool,
// formatted via time.Duration.String(), or an empty string if no estimate is
// available (no fn, unknown tool, or zero duration).
func estimatedOf(j domain.Job, fn EstimatedDurationFn) string {
	if fn == nil {
		return ""
	}
	d := fn(j.Tool)
	if d <= 0 {
		return ""
	}
	return d.String()
}
