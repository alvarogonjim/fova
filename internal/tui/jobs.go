package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
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

// sectionRule renders a panel header (SPECS §10.7.8 / §10.7.1): a lowercase
// label, a space, then a run of "─" filling out to width, all styled with
// Theme.SectionRule.
func sectionRule(label string, width int, th Theme) string {
	label = strings.ToLower(label)
	line := label + " "
	if pad := width - len([]rune(line)); pad > 0 {
		line += strings.Repeat("─", pad)
	}
	return th.SectionRule.Render(clipLine(line, width))
}

// progressBar renders a width-wide unicode bar (SPECS §10.7.8): "▓" cells for
// the elapsed-over-eta proportion (clamped to [0,1]) and "░" for the rest.
func progressBar(elapsed, eta time.Duration, width int) string {
	if width <= 0 {
		return ""
	}
	ratio := 0.0
	if eta > 0 {
		ratio = float64(elapsed) / float64(eta)
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}
	return strings.Repeat("▓", filled) + strings.Repeat("░", width-filled)
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

// jobETA estimates a running job's total duration from its elapsed time and
// reported Progress: elapsed / progress. It returns (0, false) when no
// meaningful ETA can be derived (job not running, not started, or no progress).
func jobETA(j domain.Job) (elapsed, eta time.Duration, ok bool) {
	if j.Status != domain.JobRunning || j.Started == nil || j.Progress <= 0 {
		return 0, 0, false
	}
	elapsed = time.Since(*j.Started)
	if elapsed < 0 {
		elapsed = 0
	}
	p := j.Progress
	if p > 1 {
		p = 1
	}
	return elapsed, time.Duration(float64(elapsed) / p), true
}

// View renders the jobs panel.
func (m jobsModel) View() string {
	var b strings.Builder
	b.WriteString(sectionRule("jobs", m.width, m.theme))
	b.WriteString("\n")
	if len(m.jobs) == 0 {
		b.WriteString(m.theme.Subtle.Render(wrapText(
			"no jobs yet · /install a tool or ask the agent to design", m.width)))
		return b.String()
	}
	for _, j := range m.jobs {
		g := m.theme.statusMarker(j.Status)
		line := fmt.Sprintf("%-16s %s %s",
			j.Tool, shortID(string(j.ID)), jobTimeInfo(j))
		b.WriteString(g + " " + m.theme.ToolTrace.Render(clipLine(line, m.width-2)))
		b.WriteString("\n")
		if elapsed, eta, ok := jobETA(j); ok {
			bar := progressBar(elapsed, eta, m.width-2)
			if bar != "" {
				b.WriteString("  " + m.theme.SectionRule.Render(bar))
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
