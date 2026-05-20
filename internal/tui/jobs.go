package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
)

// jobsModel renders the JOBS panel (SPECS §10.2).
type jobsModel struct {
	theme Theme
	jobs  []domain.Job
	width int
}

func newJobsModel(th Theme) jobsModel { return jobsModel{theme: th, width: 36} }

// setJobs replaces the panel's jobs.
func (m *jobsModel) setJobs(jobs []domain.Job) { m.jobs = jobs }

// setWidth sets the panel's render width.
func (m *jobsModel) setWidth(w int) { m.width = w }

// jobGlyph returns a single-rune status indicator for a job.
func jobGlyph(s domain.JobStatus) string {
	switch s {
	case domain.JobRunning:
		return "⟳"
	case domain.JobSucceeded:
		return "✓"
	case domain.JobFailed:
		return "✗"
	case domain.JobCancelled:
		return "⊘"
	default: // queued
		return "·"
	}
}

// shortID truncates an ID for compact panel display.
func shortID(id string) string {
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

// jobTimeInfo returns a short elapsed-time string for a job: "queued" before
// it starts, the running elapsed time while in progress, and the total
// run time once finished.
func jobTimeInfo(j domain.Job) string {
	if j.Started == nil {
		return "queued"
	}
	end := time.Now()
	if j.Finished != nil {
		end = *j.Finished
	}
	d := end.Sub(*j.Started).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return d.String()
}

// clipLine truncates s to at most w display columns (w<=0 means no clipping).
// It clips the plain text before any styling is applied.
func clipLine(s string, w int) string {
	if w <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w])
}

// View renders the jobs panel.
func (m jobsModel) View() string {
	var b strings.Builder
	b.WriteString(m.theme.StatusBar.Render(clipLine("JOBS", m.width)))
	b.WriteString("\n")
	if len(m.jobs) == 0 {
		b.WriteString(m.theme.ToolTrace.Render(clipLine("  no jobs yet", m.width)))
		return b.String()
	}
	for _, j := range m.jobs {
		line := fmt.Sprintf("%s %-16s %s %s",
			jobGlyph(j.Status), j.Tool, shortID(string(j.ID)), jobTimeInfo(j))
		b.WriteString(m.theme.ToolTrace.Render(clipLine(line, m.width)))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
