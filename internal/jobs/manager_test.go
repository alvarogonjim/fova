package jobs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/store"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return NewManager(st, nil)
}

// waitJob polls until the job reaches a terminal status or the deadline passes.
func waitJob(t *testing.T, m *Manager, id domain.JobID) domain.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, err := m.Status(id)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		switch j.Status {
		case domain.JobSucceeded, domain.JobFailed, domain.JobCancelled:
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish within deadline", id)
	return domain.Job{}
}

func TestManagerSubmitSucceeds(t *testing.T) {
	m := newTestManager(t)
	id, err := m.Submit(Spec{
		Kind: domain.JobCompute, Tool: "test.tool", Backend: "local",
		Input: []byte(`{}`),
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			progress(0.5)
			return []byte(`{"ok":true}`), nil
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	j := waitJob(t, m, id)
	if j.Status != domain.JobSucceeded {
		t.Fatalf("status = %s, want succeeded", j.Status)
	}
	if string(j.Output) != `{"ok":true}` {
		t.Errorf("output = %q", string(j.Output))
	}
	if j.Progress != 1 {
		t.Errorf("progress = %v, want 1", j.Progress)
	}
	if j.Started == nil || j.Finished == nil {
		t.Error("succeeded job must have started and finished timestamps")
	}
}

func TestManagerSubmitFails(t *testing.T) {
	m := newTestManager(t)
	id, _ := m.Submit(Spec{
		Kind: domain.JobCompute, Tool: "test.tool", Backend: "local", Input: []byte(`{}`),
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			return nil, errors.New("boom")
		},
	})
	j := waitJob(t, m, id)
	if j.Status != domain.JobFailed {
		t.Fatalf("status = %s, want failed", j.Status)
	}
	if j.Error != "boom" {
		t.Errorf("error = %q, want boom", j.Error)
	}
}

func TestManagerCancel(t *testing.T) {
	m := newTestManager(t)
	id, _ := m.Submit(Spec{
		Kind: domain.JobCompute, Tool: "test.tool", Backend: "local", Input: []byte(`{}`),
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			<-ctx.Done() // block until cancelled
			return nil, ctx.Err()
		},
	})
	// Wait until the job is running, then cancel it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := m.Status(id)
		if j.Status == domain.JobRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	j := waitJob(t, m, id)
	if j.Status != domain.JobCancelled {
		t.Fatalf("status = %s, want cancelled", j.Status)
	}
}

func TestManagerProgressFromGoroutineRaceClean(t *testing.T) {
	m := newTestManager(t)
	id, err := m.Submit(Spec{
		Kind: domain.JobCompute, Tool: "test.tool", Backend: "local", Input: []byte(`{}`),
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			// Report progress from a background goroutine that keeps firing
			// briefly after Run returns — this exercises the progress/terminal
			// race that issue #1 fixed.
			go func() {
				for i := 0; i < 200; i++ {
					progress(float64(i) / 200)
				}
			}()
			return []byte(`{"ok":true}`), nil
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	j := waitJob(t, m, id)
	if j.Status != domain.JobSucceeded {
		t.Fatalf("status = %s, want succeeded (a late progress call must not revert it)", j.Status)
	}
}

func TestManagerListAndCancelUnknown(t *testing.T) {
	m := newTestManager(t)
	id, _ := m.Submit(Spec{
		Kind: domain.JobCompute, Tool: "test.tool", Backend: "local", Input: []byte(`{}`),
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			return []byte(`{}`), nil
		},
	})
	waitJob(t, m, id)
	jobs, err := m.List()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("List: n=%d err=%v", len(jobs), err)
	}
	if err := m.Cancel("no-such-job"); err == nil {
		t.Error("cancelling an unknown job should error")
	}
}

func TestManagerWritesPerJobLogFile(t *testing.T) {
	m := newTestManager(t)
	m.SetLogDir(t.TempDir())

	const want = "hello from the job\nsecond line\n"
	id, err := m.Submit(Spec{
		Kind: domain.JobCompute, Tool: "test.tool", Backend: "local", Input: []byte(`{}`),
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			if _, err := io.WriteString(log, want); err != nil {
				return nil, err
			}
			return []byte(`{"ok":true}`), nil
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	j := waitJob(t, m, id)
	if j.Status != domain.JobSucceeded {
		t.Fatalf("status = %s, want succeeded", j.Status)
	}
	if j.LogFile == "" {
		t.Fatal("a finished job with a log dir must have a non-empty LogFile")
	}
	body, err := os.ReadFile(j.LogFile)
	if err != nil {
		t.Fatalf("the job log file must exist: %v", err)
	}
	if string(body) != want {
		t.Errorf("log file = %q, want %q", string(body), want)
	}
}

func TestManagerNoLogDirHasEmptyLogFile(t *testing.T) {
	m := newTestManager(t) // no SetLogDir call
	id, err := m.Submit(Spec{
		Kind: domain.JobCompute, Tool: "test.tool", Backend: "local", Input: []byte(`{}`),
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			_, _ = io.WriteString(log, "discarded") // log is io.Discard
			return []byte(`{}`), nil
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	j := waitJob(t, m, id)
	if j.LogFile != "" {
		t.Errorf("LogFile = %q, want empty when no log dir is set", j.LogFile)
	}
}
