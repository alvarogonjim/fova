package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
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
}

func newLabModel(th Theme) labModel { return labModel{theme: th, width: 36} }

// setExperiments replaces the panel's experiments.
func (m *labModel) setExperiments(exps []domain.Experiment) { m.experiments = exps }

// setWidth sets the panel's render width.
func (m *labModel) setWidth(w int) { m.width = w }

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

// View renders the wet-lab panel.
func (m labModel) View() string {
	var b strings.Builder
	b.WriteString(sectionRule("wet-lab", m.width, m.theme))
	b.WriteString("\n")
	if len(m.experiments) == 0 {
		b.WriteString(m.theme.Subtle.Render(clipLine(
			"no experiments yet · ask the agent to submit designs to Adaptyv", m.width)))
		return b.String()
	}
	for _, e := range m.experiments {
		line := fmt.Sprintf("%s · day %d of ~%d",
			shortID(string(e.ID)), experimentDay(e.SubmittedAt), turnaroundDays)
		b.WriteString(m.theme.AgentText.Render(clipLine(line, m.width)))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
