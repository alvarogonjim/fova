package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/charmbracelet/lipgloss"
)

// turnaroundDays is the typical Adaptyv wet-lab turnaround used for the
// "day N of ~21" progress copy (SPECS §12.2 / §10.2).
const turnaroundDays = 21

// labModel renders the wet-lab panel (SPECS §10.2) — one line per submitted
// Adaptyv experiment, mirroring jobsModel / designsModel.
type labModel struct {
	theme       Theme
	experiments []domain.Experiment
	width       int
	focused     bool // this panel currently holds keyboard focus
	selected    int  // highlighted row index, clamped to [0, len-1]
}

func newLabModel(th Theme) labModel { return labModel{theme: th, width: 36} }

// setExperiments replaces the panel's experiments, re-clamping the cursor.
func (m *labModel) setExperiments(exps []domain.Experiment) {
	m.experiments = exps
	m.clampSelection()
}

// setWidth sets the panel's render width.
func (m *labModel) setWidth(w int) { m.width = w }

// setFocused records whether this panel currently holds keyboard focus.
func (m *labModel) setFocused(f bool) { m.focused = f }

// clampSelection keeps selected within [0, len-1] (0 when the panel is empty).
func (m *labModel) clampSelection() {
	if m.selected >= len(m.experiments) {
		m.selected = len(m.experiments) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// selectUp / selectDown move the selection cursor and clamp it.
func (m *labModel) selectUp()   { m.selected--; m.clampSelection() }
func (m *labModel) selectDown() { m.selected++; m.clampSelection() }

// selectedExperiment returns the highlighted experiment, or false when empty.
func (m *labModel) selectedExperiment() (domain.Experiment, bool) {
	if len(m.experiments) == 0 {
		return domain.Experiment{}, false
	}
	m.clampSelection()
	return m.experiments[m.selected], true
}

// experimentDay returns the whole-day count since the experiment was
// submitted, clamped to at least 0 (a future SubmittedAt reads as day 0).
func experimentDay(submitted time.Time) int {
	if submitted.IsZero() {
		return 0
	}
	d := int(time.Since(submitted).Hours() / 24)
	if d < 0 {
		d = 0
	}
	return d
}

// View renders the wet-lab panel. When focused, the header is accent-coloured
// and the selected row is marked with a saffron "▸".
func (m labModel) View() string {
	var b strings.Builder
	b.WriteString(panelHeader("wet-lab", m.width, m.theme, m.focused))
	b.WriteString("\n")
	if len(m.experiments) == 0 {
		b.WriteString(m.theme.Subtle.Render(wrapText(
			"no experiments yet · ask the agent to submit designs to Adaptyv", m.width)))
		return b.String()
	}
	accent := lipgloss.NewStyle().Foreground(m.theme.Palette.Accent)
	for i, e := range m.experiments {
		line := fmt.Sprintf("%s · day %d of ~%d",
			shortID(string(e.ID)), experimentDay(e.SubmittedAt), turnaroundDays)
		if m.focused && i == m.selected {
			b.WriteString(accent.Render("▸ " + clipLine(line, m.width-2)))
		} else {
			b.WriteString("  " + m.theme.AgentText.Render(clipLine(line, m.width-2)))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
