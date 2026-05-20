package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/alvarogonjim/proteus/internal/domain"
)

func TestThemeStylesExist(t *testing.T) {
	th := NewTheme()
	// Render must not panic and must produce non-empty output.
	if th.StatusBar.Render("x") == "" {
		t.Error("StatusBar style produced empty output")
	}
	if th.UserText.Render("x") == "" {
		t.Error("UserText style produced empty output")
	}
	if th.ToolTrace.Render("x") == "" {
		t.Error("ToolTrace style produced empty output")
	}
}

func TestPaletteTokens(t *testing.T) {
	p := DefaultPalette
	empty := func(c lipgloss.AdaptiveColor) bool { return c.Light == "" && c.Dark == "" }
	tokens := map[string]lipgloss.AdaptiveColor{
		"Fg": p.Fg, "FgMuted": p.FgMuted, "FgSubtle": p.FgSubtle,
		"Accent": p.Accent, "Border": p.Border,
		"Queued": p.Queued, "Running": p.Running, "Succeeded": p.Succeeded,
		"Failed": p.Failed, "Warning": p.Warning,
	}
	for name, c := range tokens {
		if empty(c) {
			t.Errorf("palette token %s is empty", name)
		}
	}
}

func TestThemeV04Styles(t *testing.T) {
	th := NewTheme()
	styles := map[string]lipgloss.Style{
		"Header": th.Header, "Footer": th.Footer, "Hint": th.Hint,
		"Muted": th.Muted, "Subtle": th.Subtle, "SectionRule": th.SectionRule,
		"InputBorder": th.InputBorder, "InputBorderActive": th.InputBorderActive,
		"InputBorderBusy": th.InputBorderBusy,
	}
	for name, s := range styles {
		if s.Render("x") == "" {
			t.Errorf("v0.4 style %s produced empty output", name)
		}
	}
}

func TestGlyph(t *testing.T) {
	cases := map[domain.JobStatus]string{
		domain.JobQueued:    "·",
		domain.JobRunning:   "⟳",
		domain.JobSucceeded: "✓",
		domain.JobFailed:    "✗",
		domain.JobCancelled: "⊘",
	}
	for st, want := range cases {
		if got := glyph(st); got != want {
			t.Errorf("glyph(%s) = %q, want %q", st, got, want)
		}
	}
}
