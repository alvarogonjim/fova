package fold

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/tools"
)

func TestFoldJobToolsImplementToolInterface(t *testing.T) {
	mgr, backend := newFoldTestDeps(t, `{}`)
	var _ tools.Tool = NewChai1(mgr, backend)
}

func TestFoldJobToolNames(t *testing.T) {
	mgr, backend := newFoldTestDeps(t, `{}`)
	if got := NewChai1(mgr, backend).Name(); got != "fold.chai1" {
		t.Errorf("Chai1 Name = %q, want fold.chai1", got)
	}
}

func TestFoldJobToolSubmitsJob(t *testing.T) {
	cases := []struct {
		name string
		tool func(*jobs.Manager, stubBackend) *foldJobTool
	}{
		{"fold.chai1", func(m *jobs.Manager, b stubBackend) *foldJobTool { return NewChai1(m, b) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, backend := newFoldTestDeps(t, `{"structure_file":"complex.pdb"}`)
			tool := tc.tool(mgr, backend)

			if tool.RequiresConfirmation(nil) {
				t.Error("structure predictors should not require confirmation")
			}

			res, err := tool.Execute(context.Background(),
				json.RawMessage(`{"sequences":{"A":"MAQVQL"}}`))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.JobID == "" {
				t.Fatal("Execute must return a JobID")
			}
			job := waitJob(t, mgr, res.JobID)
			if job.Status != domain.JobSucceeded {
				t.Fatalf("job status = %q, want succeeded (error: %s)", job.Status, job.Error)
			}
		})
	}
}

func TestFoldJobToolRejectsEmptySequences(t *testing.T) {
	mgr, backend := newFoldTestDeps(t, `{}`)
	tool := NewChai1(mgr, backend)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"sequences":{}}`)); err == nil {
		t.Error("Execute should reject a request with no chains")
	}
}
