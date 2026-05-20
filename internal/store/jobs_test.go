package store

import (
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
)

func sampleJob(id domain.JobID) domain.Job {
	return domain.Job{
		ID:      id,
		Kind:    domain.JobCompute,
		Tool:    "design.bindcraft",
		Status:  domain.JobQueued,
		Created: time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
		Backend: "local",
		Input:   []byte(`{"target":"1ZWG"}`),
	}
}

func TestJobInsertGet(t *testing.T) {
	st := openTestStore(t)
	if err := st.InsertJob(sampleJob("j_0001")); err != nil {
		t.Fatalf("InsertJob: %v", err)
	}
	got, err := st.GetJob("j_0001")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != domain.JobQueued || got.Tool != "design.bindcraft" {
		t.Fatalf("job mismatch: %+v", got)
	}
	if string(got.Input) != `{"target":"1ZWG"}` {
		t.Errorf("input = %q", string(got.Input))
	}
	if got.Started != nil || got.Finished != nil {
		t.Error("queued job should have nil started/finished")
	}
}

func TestJobUpdate(t *testing.T) {
	st := openTestStore(t)
	j := sampleJob("j_0001")
	if err := st.InsertJob(j); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 5, 16, 12, 1, 0, 0, time.UTC)
	j.Status = domain.JobRunning
	j.Started = &started
	j.Progress = 0.5
	if err := st.UpdateJob(j); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	got, _ := st.GetJob("j_0001")
	if got.Status != domain.JobRunning || got.Progress != 0.5 {
		t.Fatalf("update not applied: %+v", got)
	}
	if got.Started == nil || !got.Started.Equal(started) {
		t.Errorf("started = %v, want %v", got.Started, started)
	}
}

func TestJobMarkRunningInterrupted(t *testing.T) {
	st := openTestStore(t)
	running := sampleJob("j_run")
	running.Status = domain.JobRunning
	if err := st.InsertJob(running); err != nil {
		t.Fatal(err)
	}
	queued := sampleJob("j_q")
	if err := st.InsertJob(queued); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkRunningJobsInterrupted(); err != nil {
		t.Fatalf("MarkRunningJobsInterrupted: %v", err)
	}
	r, _ := st.GetJob("j_run")
	if r.Status != domain.JobFailed || r.Error == "" {
		t.Errorf("running job not marked failed: %+v", r)
	}
	if r.Finished == nil {
		t.Error("interrupted job should have a finished timestamp")
	}
	q, _ := st.GetJob("j_q")
	if q.Status != domain.JobQueued {
		t.Errorf("queued job should be untouched, got %v", q.Status)
	}
}

func TestJobList(t *testing.T) {
	st := openTestStore(t)
	for _, id := range []domain.JobID{"j1", "j2"} {
		if err := st.InsertJob(sampleJob(id)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.ListJobs(DefaultProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ListJobs returned %d, want 2", len(got))
	}
}
