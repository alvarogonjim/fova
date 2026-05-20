package fold

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// stubBackend returns a fixed structure-prediction output, ignoring the request.
type stubBackend struct{ output string }

func (s stubBackend) Name() string { return "stub" }
func (s stubBackend) Run(ctx context.Context, tool string, input []byte, log io.Writer) ([]byte, error) {
	_, _ = log.Write(input)
	return []byte(s.output), nil
}

func newFoldTestDeps(t *testing.T, backendOutput string) (*jobs.Manager, stubBackend) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return jobs.NewManager(st, nil), stubBackend{output: backendOutput}
}

func waitJob(t *testing.T, m *jobs.Manager, id domain.JobID) domain.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := m.Status(id)
		switch j.Status {
		case domain.JobSucceeded, domain.JobFailed, domain.JobCancelled:
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return domain.Job{}
}

func TestFoldJobToolsImplementToolInterface(t *testing.T) {
	mgr, backend := newFoldTestDeps(t, `{}`)
	var _ tools.Tool = NewBoltz2(mgr, backend)
	var _ tools.Tool = NewChai1(mgr, backend)
}

func TestFoldJobToolNames(t *testing.T) {
	mgr, backend := newFoldTestDeps(t, `{}`)
	if got := NewBoltz2(mgr, backend).Name(); got != "fold.boltz2" {
		t.Errorf("Boltz2 Name = %q, want fold.boltz2", got)
	}
	if got := NewChai1(mgr, backend).Name(); got != "fold.chai1" {
		t.Errorf("Chai1 Name = %q, want fold.chai1", got)
	}
}

func TestFoldJobToolSubmitsJob(t *testing.T) {
	cases := []struct {
		name string
		tool func(*jobs.Manager, stubBackend) *foldJobTool
	}{
		{"fold.boltz2", func(m *jobs.Manager, b stubBackend) *foldJobTool { return NewBoltz2(m, b) }},
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
	tool := NewBoltz2(mgr, backend)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"sequences":{}}`)); err == nil {
		t.Error("Execute should reject a request with no chains")
	}
}
