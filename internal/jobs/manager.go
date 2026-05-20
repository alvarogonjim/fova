// Package jobs runs long-running tool invocations as tracked async jobs.
package jobs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/store"
)

// Spec describes a job to run.
type Spec struct {
	Kind    domain.JobKind
	Tool    string
	Backend string
	Input   []byte
	// Run performs the work. It must honour ctx (abort promptly when cancelled),
	// may call progress(fraction) to report 0..1 completion, and may write its
	// subprocess output to log (the job's per-job log file, or io.Discard).
	Run func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error)
}

// Manager submits, tracks, and cancels async jobs, persisting every state
// change to the store.
type Manager struct {
	store    *store.Store
	onUpdate func(domain.Job) // optional; called on every job state change
	mu       sync.Mutex
	cancels  map[domain.JobID]context.CancelFunc
	logDir   string // when non-empty, each job gets a <logDir>/<jobID>.log file
}

// NewManager builds a job manager. onUpdate may be nil.
func NewManager(st *store.Store, onUpdate func(domain.Job)) *Manager {
	return &Manager{
		store:    st,
		onUpdate: onUpdate,
		cancels:  map[domain.JobID]context.CancelFunc{},
	}
}

// SetLogDir tells the Manager to give every subsequent job its own log file at
// <dir>/<jobID>.log. An empty dir disables per-job log files.
func (m *Manager) SetLogDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logDir = dir
}

// Submit persists a queued job and starts running it in the background. It
// returns immediately with the new job's ID. The job runs under its own
// context (independent of any agent turn) until completion or Cancel.
func (m *Manager) Submit(spec Spec) (domain.JobID, error) {
	job := domain.Job{
		ID:      domain.JobID("j_" + uuid.NewString()),
		Kind:    spec.Kind,
		Tool:    spec.Tool,
		Status:  domain.JobQueued,
		Created: time.Now().UTC(),
		Backend: spec.Backend,
		Input:   spec.Input,
	}
	if err := m.store.InsertJob(job); err != nil {
		return "", err
	}
	m.emit(job)

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[job.ID] = cancel
	m.mu.Unlock()

	go m.run(ctx, job, spec)
	return job.ID, nil
}

// run executes a job to completion in its own goroutine.
func (m *Manager) run(ctx context.Context, job domain.Job, spec Spec) {
	defer func() {
		m.mu.Lock()
		delete(m.cancels, job.ID)
		m.mu.Unlock()
	}()

	// mutate is the ONLY path that touches `job`. It applies fn under m.mu and
	// persists the result before releasing the lock, so concurrent callers
	// (notably a tool reporting progress from another goroutine) are
	// serialized and the DB always reflects the latest mutation.
	mutate := func(fn func(*domain.Job)) {
		m.mu.Lock()
		defer m.mu.Unlock()
		fn(&job)
		_ = m.store.UpdateJob(job)
		m.emit(job)
	}

	// Set up the per-job log file before the first mutate, so the job record
	// carries its LogFile from the moment it starts running. A failure to
	// create the file is non-fatal: the job still runs, logging to io.Discard.
	var log io.Writer = io.Discard
	m.mu.Lock()
	logDir := m.logDir
	m.mu.Unlock()
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0o755); err == nil {
			path := filepath.Join(logDir, string(job.ID)+".log")
			if f, err := os.Create(path); err == nil {
				job.LogFile = path
				log = f
				defer f.Close()
			}
		}
	}

	mutate(func(j *domain.Job) {
		t := time.Now().UTC()
		j.Status = domain.JobRunning
		j.Started = &t
	})

	output, runErr := spec.Run(ctx, func(f float64) {
		mutate(func(j *domain.Job) {
			if isTerminal(j.Status) {
				return // ignore progress once the job has finished
			}
			j.Progress = clamp01(f)
		})
	}, log)

	mutate(func(j *domain.Job) {
		t := time.Now().UTC()
		j.Finished = &t
		switch {
		case ctx.Err() != nil:
			j.Status = domain.JobCancelled
			j.Error = "cancelled by user"
		case runErr != nil:
			j.Status = domain.JobFailed
			j.Error = runErr.Error()
		default:
			j.Status = domain.JobSucceeded
			j.Output = output
			j.Progress = 1
		}
	})
}

// isTerminal reports whether a job has reached a final status.
func isTerminal(s domain.JobStatus) bool {
	return s == domain.JobSucceeded || s == domain.JobFailed || s == domain.JobCancelled
}

// Status returns the current persisted state of a job.
func (m *Manager) Status(id domain.JobID) (domain.Job, error) {
	return m.store.GetJob(id)
}

// Result returns the job; once terminal its Output/Error are populated. It is
// non-blocking — callers poll rather than wait.
func (m *Manager) Result(id domain.JobID) (domain.Job, error) {
	return m.store.GetJob(id)
}

// Cancel requests cancellation of a running job (best-effort). It errors if no
// job with that ID is currently running.
func (m *Manager) Cancel(id domain.JobID) error {
	m.mu.Lock()
	cancel, ok := m.cancels[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("job %s is not running", id)
	}
	cancel()
	return nil
}

// List returns all jobs for the default project, newest first.
func (m *Manager) List() ([]domain.Job, error) {
	return m.store.ListJobs(store.DefaultProjectID)
}

func (m *Manager) emit(job domain.Job) {
	if m.onUpdate != nil {
		m.onUpdate(job)
	}
}

func clamp01(f float64) float64 {
	if f != f { // NaN
		return 0
	}
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
