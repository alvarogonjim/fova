// Package jobs provides the agent-facing jobs.* tools over a job Manager.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	jobmgr "github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/tools"
)

// emptyObjectSchema is the JSON Schema for a tool taking no arguments.
func emptyObjectSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// jobIDSchema is the JSON Schema for a tool taking a single job_id argument.
func jobIDSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string", "description": "Job ID"},
		},
		"required": []string{"job_id"},
	}
}

// ListTool implements jobs.list.
type ListTool struct{ mgr *jobmgr.Manager }

// NewListTool builds the jobs.list tool.
func NewListTool(mgr *jobmgr.Manager) *ListTool { return &ListTool{mgr: mgr} }

func (*ListTool) Name() string                                    { return "jobs.list" }
func (*ListTool) Concurrent() bool                               { return true }
func (*ListTool) Description() string                             { return "List active and recent compute and lab jobs." }
func (*ListTool) InputSchema() map[string]any                     { return emptyObjectSchema() }
func (*ListTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*ListTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*ListTool) EstimatedDuration(json.RawMessage) time.Duration { return 50 * time.Millisecond }

func (t *ListTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	jobs, err := t.mgr.List()
	if err != nil {
		return tools.Result{}, err
	}
	var b strings.Builder
	if len(jobs) == 0 {
		b.WriteString("no jobs")
	}
	for _, j := range jobs {
		fmt.Fprintf(&b, "%s  %-20s  %-9s  %3.0f%%\n",
			j.ID, j.Tool, j.Status, j.Progress*100)
	}
	out, _ := json.Marshal(jobs)
	return tools.Result{
		Output:     out,
		Display:    strings.TrimRight(b.String(), "\n"),
		Provenance: domain.NewToolCallRef("jobs.list", input),
	}, nil
}
