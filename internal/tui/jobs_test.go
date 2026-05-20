package tui

import (
	"strings"
	"testing"

	"github.com/alvarogonjim/proteus/internal/domain"
)

func TestJobsPanelRendersJobs(t *testing.T) {
	m := newJobsModel(NewTheme())
	m.setJobs([]domain.Job{
		{ID: "j_aaaaaaaa", Tool: "design.bindcraft", Status: domain.JobRunning, Progress: 0.4},
		{ID: "j_bbbbbbbb", Tool: "fold.esmfold", Status: domain.JobSucceeded, Progress: 1},
	})
	out := m.View()
	if !strings.Contains(out, "JOBS") {
		t.Errorf("panel missing header: %q", out)
	}
	if !strings.Contains(out, "design.bindcraft") || !strings.Contains(out, "fold.esmfold") {
		t.Errorf("panel missing job rows: %q", out)
	}
}

func TestJobsPanelEmpty(t *testing.T) {
	m := newJobsModel(NewTheme())
	out := m.View()
	if !strings.Contains(out, "JOBS") {
		t.Errorf("empty panel still shows the header: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "no ") {
		t.Errorf("empty panel says there are no jobs: %q", out)
	}
}
