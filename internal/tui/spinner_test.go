package tui

import (
	"strings"
	"testing"
	"time"
)

func TestThinkingViewEmptyBeforeStart(t *testing.T) {
	th := NewTheme()
	var m thinkingModel
	if m.active() {
		t.Fatal("indicator should not be active before start")
	}
	if got := m.view(th, time.Now()); got != "" {
		t.Fatalf("view before start = %q, want empty", got)
	}
}

func TestThinkingViewAfterStart(t *testing.T) {
	th := NewTheme()
	t0 := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	now := t0.Add(12 * time.Second)

	var m thinkingModel
	m.start("Designing", t0)
	if !m.active() {
		t.Fatal("indicator should be active after start")
	}

	got := m.view(th, now)
	for _, want := range []string{"Designing…", "12s", "esc to interrupt"} {
		if !strings.Contains(got, want) {
			t.Errorf("view = %q, want it to contain %q", got, want)
		}
	}
}

func TestThinkingTickAdvancesFrame(t *testing.T) {
	th := NewTheme()
	t0 := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	var m thinkingModel
	m.start("Folding", t0)
	first := m.view(th, t0)

	m.tick()
	if got := m.view(th, t0); got == first {
		t.Fatalf("view after one tick = %q, want a different frame", got)
	}

	// One full cycle of ticks returns to the first frame.
	for i := 1; i < len(spinnerFrames); i++ {
		m.tick()
	}
	if got := m.view(th, t0); got != first {
		t.Fatalf("view after %d ticks = %q, want %q (wrap to first frame)",
			len(spinnerFrames), got, first)
	}
}

func TestThinkingStopClears(t *testing.T) {
	th := NewTheme()
	t0 := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)

	var m thinkingModel
	m.start("Scoring", t0)
	m.stop()

	if m.active() {
		t.Error("indicator should be inactive after stop")
	}
	if got := m.view(th, t0); got != "" {
		t.Errorf("view after stop = %q, want empty", got)
	}
}

func TestThinkingVerbForTool(t *testing.T) {
	cases := []struct {
		tool string
		want string
	}{
		{"rfdiffusion.generate", "Designing"},
		{"proteinmpnn.run", "Designing"},
		{"design.binder", "Designing"},
		{"fold.esmfold", "Folding"},
		{"boltz.predict", "Folding"},
		{"chai.fold", "Folding"},
		{"score.ipsae", "Scoring"},
		{"compute.metric", "Scoring"},
		{"knowledge.search", "Searching"},
		{"web.lookup", "Searching"},
		{"", "Thinking"},
		{"unknown.tool", "Thinking"},
	}
	for _, c := range cases {
		if got := verbForTool(c.tool); got != c.want {
			t.Errorf("verbForTool(%q) = %q, want %q", c.tool, got, c.want)
		}
	}
}
