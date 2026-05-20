package jobs

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	jobmgr "github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
)

// newTestManagerWithJob opens a store-backed manager, submits one job, waits
// for it to finish, and returns the manager plus the finished job's ID.
func newTestManagerWithJob(t *testing.T) (*jobmgr.Manager, domain.JobID) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m := jobmgr.NewManager(st, nil)
	id, err := m.Submit(jobmgr.Spec{
		Kind: domain.JobCompute, Tool: "design.bindcraft", Backend: "local",
		Input: []byte(`{}`),
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			return []byte(`{"designs":3}`), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := m.Status(id)
		if j.Status == domain.JobSucceeded {
			return m, id
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return nil, ""
}

// newTestManagerWithRunningJob submits a long-running job (one that blocks on
// ctx until cancelled), waits for it to enter the running state, and returns
// the manager + ID. The caller is responsible for cancelling/finishing the
// job; t.Cleanup is registered to do so automatically.
func newTestManagerWithRunningJob(t *testing.T, tool string) (*jobmgr.Manager, domain.JobID) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m := jobmgr.NewManager(st, nil)
	id, err := m.Submit(jobmgr.Spec{
		Kind: domain.JobCompute, Tool: tool, Backend: "local",
		Input: []byte(`{}`),
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Cancel(id) })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := m.Status(id)
		if j.Status == domain.JobRunning {
			return m, id
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not reach running state")
	return nil, ""
}

func TestJobsListTool(t *testing.T) {
	m, _ := newTestManagerWithJob(t)
	res, err := NewListTool(m).Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Display, "design.bindcraft") {
		t.Fatalf("list missing job: %q", res.Display)
	}
}

func TestJobsStatusTool(t *testing.T) {
	m, id := newTestManagerWithJob(t)
	res, err := NewStatusTool(m, nil).Execute(context.Background(),
		json.RawMessage(`{"job_id":"`+string(id)+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(strings.ToLower(res.Display), "succeeded") {
		t.Fatalf("status missing state: %q", res.Display)
	}
	// Bug 3: a terminal job should still surface its elapsed wall-clock.
	if !strings.Contains(res.Display, "elapsed=") {
		t.Errorf("status of finished job should include elapsed=: %q", res.Display)
	}
	if _, err := NewStatusTool(m, nil).Execute(context.Background(),
		json.RawMessage(`{"job_id":"no-such-job"}`)); err == nil {
		t.Error("status of an unknown job should error")
	}
}

// TestJobsStatusToolRunningWithEstimate verifies AC1: a running job's status
// row surfaces both elapsed and estimated duration. With Started set ~now and
// EstimatedDurationFn returning 30 min, the display must include
// "elapsed=…" and "estimated=30m0s".
func TestJobsStatusToolRunningWithEstimate(t *testing.T) {
	m, id := newTestManagerWithRunningJob(t, "design.proteinmpnn")
	estd := func(name string) time.Duration {
		if name == "design.proteinmpnn" {
			return 30 * time.Minute
		}
		return 0
	}
	res, err := NewStatusTool(m, estd).Execute(context.Background(),
		json.RawMessage(`{"job_id":"`+string(id)+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Display, "status=running") {
		t.Fatalf("expected running status: %q", res.Display)
	}
	if !strings.Contains(res.Display, "elapsed=") {
		t.Errorf("expected elapsed= in row: %q", res.Display)
	}
	if !strings.Contains(res.Display, "estimated=30m0s") {
		t.Errorf("expected estimated=30m0s in row: %q", res.Display)
	}
}

// TestJobsStatusToolNilEstimatedDurationFn verifies the graceful fallback:
// passing a nil EstimatedDurationFn must not crash and must simply omit the
// `estimated` field.
func TestJobsStatusToolNilEstimatedDurationFn(t *testing.T) {
	m, id := newTestManagerWithRunningJob(t, "design.proteinmpnn")
	res, err := NewStatusTool(m, nil).Execute(context.Background(),
		json.RawMessage(`{"job_id":"`+string(id)+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(res.Display, "estimated=") {
		t.Errorf("nil EstimatedDurationFn should omit estimated=: %q", res.Display)
	}
	// elapsed must still appear — the job has started.
	if !strings.Contains(res.Display, "elapsed=") {
		t.Errorf("running job should still include elapsed=: %q", res.Display)
	}
}

// TestJobsStatusToolUnknownToolOmitsEstimate verifies that an
// EstimatedDurationFn that returns 0 (unknown tool) cleanly omits the field
// rather than rendering `estimated=0s`.
func TestJobsStatusToolUnknownToolOmitsEstimate(t *testing.T) {
	m, id := newTestManagerWithRunningJob(t, "design.proteinmpnn")
	estd := func(name string) time.Duration { return 0 }
	res, err := NewStatusTool(m, estd).Execute(context.Background(),
		json.RawMessage(`{"job_id":"`+string(id)+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(res.Display, "estimated=") {
		t.Errorf("zero EstimatedDuration should omit estimated=: %q", res.Display)
	}
}

func TestJobsResultTool(t *testing.T) {
	m, id := newTestManagerWithJob(t)
	res, err := NewResultTool(m, nil).Execute(context.Background(),
		json.RawMessage(`{"job_id":"`+string(id)+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Display, `{"designs":3}`) {
		t.Fatalf("result missing job output: %q", res.Display)
	}
	if !strings.Contains(res.Display, "elapsed=") {
		t.Errorf("result of finished job should include elapsed=: %q", res.Display)
	}
}

// TestJobsResultToolRunningSurfacesEstimate verifies that polling
// jobs.result on a still-running job surfaces both elapsed and estimated, so
// the agent has the timing anchor described in Bug 3.
func TestJobsResultToolRunningSurfacesEstimate(t *testing.T) {
	m, id := newTestManagerWithRunningJob(t, "design.proteinmpnn")
	estd := func(name string) time.Duration { return 30 * time.Minute }
	res, err := NewResultTool(m, estd).Execute(context.Background(),
		json.RawMessage(`{"job_id":"`+string(id)+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Display, "still running") {
		t.Fatalf("expected still running display: %q", res.Display)
	}
	if !strings.Contains(res.Display, "elapsed=") {
		t.Errorf("running result should include elapsed=: %q", res.Display)
	}
	if !strings.Contains(res.Display, "estimated=30m0s") {
		t.Errorf("running result should include estimated=30m0s: %q", res.Display)
	}
}

func TestJobsCancelTool(t *testing.T) {
	m, id := newTestManagerWithJob(t)
	// The job already finished, so it is no longer cancellable.
	res, err := NewCancelTool(m).Execute(context.Background(),
		json.RawMessage(`{"job_id":"`+string(id)+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(strings.ToLower(res.Display), "not running") {
		t.Fatalf("cancel of a finished job: %q", res.Display)
	}
}
