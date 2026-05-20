package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
)

func TestJobsPanelRendersJobs(t *testing.T) {
	m := newJobsModel(NewTheme())
	m.setJobs([]domain.Job{
		{ID: "j_aaaaaaaa", Tool: "design.bindcraft", Status: domain.JobRunning, Progress: 0.4},
		{ID: "j_bbbbbbbb", Tool: "fold.esmfold", Status: domain.JobSucceeded, Progress: 1},
	})
	out := m.View()
	if !strings.Contains(out, "jobs") {
		t.Errorf("panel missing header: %q", out)
	}
	if !strings.Contains(out, "design.bindcraft") || !strings.Contains(out, "fold.esmfold") {
		t.Errorf("panel missing job rows: %q", out)
	}
}

func TestJobsPanelEmpty(t *testing.T) {
	m := newJobsModel(NewTheme())
	out := m.View()
	if !strings.Contains(out, "jobs") {
		t.Errorf("empty panel still shows the header: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "no ") {
		t.Errorf("empty panel says there are no jobs: %q", out)
	}
}

// TestJobsSectionRule checks the panel header is a dim label plus a "─" rule.
func TestJobsSectionRule(t *testing.T) {
	out := sectionRule("jobs", 30, NewTheme())
	if !strings.Contains(out, "jobs") {
		t.Errorf("section rule missing the label: %q", out)
	}
	if !strings.Contains(out, "─") {
		t.Errorf("section rule missing the rule rune: %q", out)
	}
}

// TestJobsSectionRuleLowercase checks the label is lowercased.
func TestJobsSectionRuleLowercase(t *testing.T) {
	out := sectionRule("JOBS", 20, NewTheme())
	if strings.Contains(out, "JOBS") {
		t.Errorf("section rule should lowercase the label: %q", out)
	}
	if !strings.Contains(out, "jobs") {
		t.Errorf("section rule missing the lowercased label: %q", out)
	}
}

// TestJobsEmptyStateActionable checks the empty state nudges the user toward
// an action (SPECS §10.7.8).
func TestJobsEmptyStateActionable(t *testing.T) {
	m := newJobsModel(NewTheme())
	m.setWidth(80)
	out := m.View()
	if !strings.Contains(out, "no jobs yet") {
		t.Errorf("empty state missing the headline: %q", out)
	}
	if !strings.Contains(out, "ask the agent to design") {
		t.Errorf("empty state should be actionable: %q", out)
	}
}

// TestJobsProgressBarHalf checks a half-elapsed bar mixes filled and empty cells.
func TestJobsProgressBarHalf(t *testing.T) {
	out := progressBar(30*time.Second, 60*time.Second, 10)
	if !strings.Contains(out, "▓") || !strings.Contains(out, "░") {
		t.Errorf("half-elapsed bar should have both ▓ and ░: %q", out)
	}
}

// TestJobsProgressBarFull checks a fully-elapsed bar is all filled.
func TestJobsProgressBarFull(t *testing.T) {
	out := progressBar(90*time.Second, 60*time.Second, 8)
	if strings.Contains(out, "░") {
		t.Errorf("fully-elapsed bar should be all ▓: %q", out)
	}
	if out != strings.Repeat("▓", 8) {
		t.Errorf("fully-elapsed bar should fill the width: %q", out)
	}
}

// TestJobsProgressBarZeroETA renders no fill when the ETA is unknown.
func TestJobsProgressBarZeroETA(t *testing.T) {
	out := progressBar(30*time.Second, 0, 8)
	if strings.Contains(out, "▓") {
		t.Errorf("unknown ETA should render an empty bar: %q", out)
	}
}

// TestJobsRunningJobHasProgressBar checks a running job with progress renders
// a "▓" cell in the panel view (the bar is derived from elapsed/ETA).
func TestJobsRunningJobHasProgressBar(t *testing.T) {
	m := newJobsModel(NewTheme())
	m.setWidth(60)
	started := time.Now().Add(-30 * time.Second)
	m.setJobs([]domain.Job{
		{ID: "j_running1", Tool: "design.bindcraft", Status: domain.JobRunning,
			Started: &started, Progress: 0.5},
	})
	out := m.View()
	if !strings.Contains(out, "▓") {
		t.Errorf("running job with an ETA should render a progress bar: %q", out)
	}
}
