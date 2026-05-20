package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
)

// TestLabPanelRendersExperiments checks the panel shows the wet-lab header and
// a "day N of ~21" line per experiment (SPECS §10.2).
func TestLabPanelRendersExperiments(t *testing.T) {
	m := newLabModel(NewTheme())
	m.setWidth(60)
	m.setExperiments([]domain.Experiment{
		{ID: "expt_4", TargetName: "HER2", AssayType: "binding",
			SubmittedAt: time.Now().Add(-3 * 24 * time.Hour)},
	})
	out := m.View()
	if !strings.Contains(out, "wet-lab") {
		t.Errorf("panel missing the wet-lab header: %q", out)
	}
	if !strings.Contains(out, "expt_4") {
		t.Errorf("panel missing the experiment id: %q", out)
	}
	if !strings.Contains(out, "day 3") {
		t.Errorf("panel missing the day count: %q", out)
	}
	if !strings.Contains(out, "of ~21") {
		t.Errorf("panel missing the ~21 turnaround: %q", out)
	}
}

// TestLabPanelEmpty checks the empty state shows the header and an actionable
// nudge to submit designs.
func TestLabPanelEmpty(t *testing.T) {
	m := newLabModel(NewTheme())
	m.setWidth(80)
	out := m.View()
	if !strings.Contains(out, "wet-lab") {
		t.Errorf("empty panel still shows the header: %q", out)
	}
	if !strings.Contains(out, "no experiments yet") {
		t.Errorf("empty state missing the headline: %q", out)
	}
	if !strings.Contains(out, "submit designs to Adaptyv") {
		t.Errorf("empty state should be actionable: %q", out)
	}
}

// TestLabPanelClampsFutureDay checks an experiment with a future SubmittedAt
// reads as day 0 rather than a negative count.
func TestLabPanelClampsFutureDay(t *testing.T) {
	m := newLabModel(NewTheme())
	m.setWidth(60)
	m.setExperiments([]domain.Experiment{
		{ID: "expt_9", SubmittedAt: time.Now().Add(48 * time.Hour)},
	})
	out := m.View()
	if !strings.Contains(out, "day 0 of ~21") {
		t.Errorf("future submission should clamp to day 0: %q", out)
	}
}

// TestLabPanelClipsToWidth checks long lines are clipped to the panel width.
func TestLabPanelClipsToWidth(t *testing.T) {
	m := newLabModel(NewTheme())
	m.setWidth(12)
	m.setExperiments([]domain.Experiment{
		{ID: "expt_long_identifier", SubmittedAt: time.Now()},
	})
	for _, line := range strings.Split(m.View(), "\n") {
		if got := len([]rune(stripANSI(line))); got > 12 {
			t.Errorf("line exceeds width 12 (%d): %q", got, line)
		}
	}
}

// stripANSI removes ANSI escape sequences so width assertions count only
// visible runes.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case inEsc:
			// skip
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
