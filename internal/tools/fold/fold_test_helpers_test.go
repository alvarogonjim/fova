package fold

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
)

// stubBackend returns a fixed structure-prediction output, ignoring the request.
type stubBackend struct{ output string }

func (s stubBackend) Name() string { return "stub" }
func (s stubBackend) Run(ctx context.Context, tool string, input []byte, log io.Writer, progress func(float64)) ([]byte, error) {
	_, _ = log.Write(input)
	if progress != nil {
		progress(0.5)
	}
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
