package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/charmbracelet/lipgloss"
)

// jobsModel renders the JOBS panel (SPECS §10.2).
type jobsModel struct {
	theme    Theme
	jobs     []domain.Job
	width    int
	focused  bool // this panel currently holds keyboard focus
	selected int  // highlighted row index, clamped to [0, len-1]
}

func newJobsModel(th Theme) jobsModel { return jobsModel{theme: th, width: 36} }

// setJobs replaces the panel's jobs, re-clamping the selection cursor.
func (m *jobsModel) setJobs(jobs []domain.Job) {
	m.jobs = jobs
	m.clampSelection()
}

// setFocused records whether this panel currently holds keyboard focus.
func (m *jobsModel) setFocused(f bool) { m.focused = f }

// clampSelection keeps selected within [0, len-1] (0 when the panel is empty).
func (m *jobsModel) clampSelection() {
	if m.selected >= len(m.jobs) {
		m.selected = len(m.jobs) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// selectUp / selectDown move the selection cursor and clamp it.
func (m *jobsModel) selectUp()   { m.selected--; m.clampSelection() }
func (m *jobsModel) selectDown() { m.selected++; m.clampSelection() }

// selectedJob returns the highlighted job, or false when the panel is empty.
func (m *jobsModel) selectedJob() (domain.Job, bool) {
	if len(m.jobs) == 0 {
		return domain.Job{}, false
	}
	m.clampSelection()
	return m.jobs[m.selected], true
}

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

// panelHeader renders a panel's section-rule header. A focused panel's header
// is recoloured to the theme accent so the focus is visible at a glance.
func panelHeader(label string, width int, th Theme, focused bool) string {
	label = strings.ToLower(label)
	line := label + " "
	if pad := width - len([]rune(line)); pad > 0 {
		line += strings.Repeat("─", pad)
	}
	style := th.SectionRule
	if focused {
		style = lipgloss.NewStyle().Foreground(th.Palette.Accent)
	}
	return style.Render(clipLine(line, width))
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

// View renders the jobs panel. When focused, the header is accent-coloured
// and the selected row is marked with a saffron "▸".
func (m jobsModel) View() string {
	var b strings.Builder
	b.WriteString(panelHeader("jobs", m.width, m.theme, m.focused))
	b.WriteString("\n")
	if len(m.jobs) == 0 {
		b.WriteString(m.theme.Subtle.Render(wrapText(
			"no jobs yet · /install a tool or ask the agent to design", m.width)))
		return b.String()
	}
	accent := lipgloss.NewStyle().Foreground(m.theme.Palette.Accent)
	for i, j := range m.jobs {
		line := fmt.Sprintf("%-16s %s %s", j.Tool, shortID(string(j.ID)), jobTimeInfo(j))
		prefix := m.theme.statusMarker(j.Status) + " "
		rowStyle := m.theme.ToolTrace
		if m.focused && i == m.selected {
			prefix = accent.Render("▸") + " "
			rowStyle = accent
		}
		b.WriteString(prefix + rowStyle.Render(clipLine(line, m.width-2)))
		b.WriteString("\n")
		if elapsed, eta, ok := jobETA(j); ok {
			if bar := progressBar(elapsed, eta, m.width-2); bar != "" {
				b.WriteString("  " + m.theme.SectionRule.Render(bar))
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
