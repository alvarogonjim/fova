package jobs

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
	jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/store"
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
	res, err := NewStatusTool(m).Execute(context.Background(),
		json.RawMessage(`{"job_id":"`+string(id)+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(strings.ToLower(res.Display), "succeeded") {
		t.Fatalf("status missing state: %q", res.Display)
	}
	if _, err := NewStatusTool(m).Execute(context.Background(),
		json.RawMessage(`{"job_id":"no-such-job"}`)); err == nil {
		t.Error("status of an unknown job should error")
	}
}

func TestJobsResultTool(t *testing.T) {
	m, id := newTestManagerWithJob(t)
	res, err := NewResultTool(m).Execute(context.Background(),
		json.RawMessage(`{"job_id":"`+string(id)+`"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Display, `{"designs":3}`) {
		t.Fatalf("result missing job output: %q", res.Display)
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
