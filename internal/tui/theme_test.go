package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/alvarogonjim/fova/internal/domain"
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
		"Bg": p.Bg, "Fg": p.Fg, "FgMuted": p.FgMuted, "FgSubtle": p.FgSubtle,
		"Primary": p.Primary, "Accent": p.Accent, "Border": p.Border,
		"Queued": p.Queued, "Running": p.Running, "Succeeded": p.Succeeded,
		"Failed": p.Failed, "Warning": p.Warning,
	}
	for name, c := range tokens {
		if empty(c) {
			t.Errorf("palette token %s is empty", name)
		}
	}
}

// TestPaletteFovaHexes locks in the rebrand token values (spec §2). If you
// reskin again, update these expectations — but they exist so an accidental
// edit to theme.go fails the test rather than silently shipping.
func TestPaletteFovaHexes(t *testing.T) {
	p := DefaultPalette
	cases := []struct {
		name                string
		c                   lipgloss.AdaptiveColor
		wantLight, wantDark string
	}{
		{"Bg", p.Bg, "#0d1f15", "#0d1f15"},
		{"Fg", p.Fg, "#d4cfc0", "#d4cfc0"},
		{"FgMuted", p.FgMuted, "#6b7a6b", "#6b7a6b"},
		{"FgSubtle", p.FgSubtle, "#4a7e2a", "#4a7e2a"},
		{"Primary", p.Primary, "#7fc14a", "#7fc14a"},
		{"Accent", p.Accent, "#EF9F27", "#EF9F27"},
		{"Border", p.Border, "#4a7e2a", "#4a7e2a"},
		{"Queued", p.Queued, "#6b7a6b", "#6b7a6b"},
		{"Running", p.Running, "#7fc14a", "#7fc14a"},
		{"Succeeded", p.Succeeded, "#7fc14a", "#7fc14a"},
		{"Failed", p.Failed, "#DC2626", "#F87171"},
		{"Warning", p.Warning, "#EF9F27", "#EF9F27"},
	}
	for _, c := range cases {
		if c.c.Light != c.wantLight {
			t.Errorf("%s.Light = %q, want %q", c.name, c.c.Light, c.wantLight)
		}
		if c.c.Dark != c.wantDark {
			t.Errorf("%s.Dark = %q, want %q", c.name, c.c.Dark, c.wantDark)
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

// TestThemeMarkerStyles covers the rebrand §3.4 marker styles. Each marker
// must render its glyph non-empty and the rendered string must include both
// the expected glyph and the corresponding palette colour hex code.
func TestThemeMarkerStyles(t *testing.T) {
	th := NewTheme()
	cases := []struct {
		name  string
		style lipgloss.Style
		glyph string
	}{
		{"MarkerSuccess", th.MarkerSuccess, MarkerSuccessGlyph},
		{"MarkerRunning", th.MarkerRunning, MarkerRunningGlyph},
		{"MarkerQueued", th.MarkerQueued, MarkerQueuedGlyph},
		{"MarkerAttention", th.MarkerAttention, MarkerAttentionGlyph},
	}
	for _, c := range cases {
		out := c.style.Render(c.glyph)
		if out == "" {
			t.Errorf("%s rendered empty", c.name)
			continue
		}
		if !strings.Contains(out, c.glyph) {
			t.Errorf("%s output %q missing glyph %q", c.name, out, c.glyph)
		}
		// lipgloss may render true-colour or 256-colour. Check that the
		// style's foreground resolves to the expected palette adaptive
		// colour (not the rendered ANSI bytes, which depend on terminal
		// detection in tests).
		if fg := c.style.GetForeground(); fg == nil {
			t.Errorf("%s has no foreground set", c.name)
		}
	}

	// Re-assert the foreground colours explicitly via the palette so we
	// don't depend on ANSI byte inspection.
	if got := th.MarkerSuccess.GetForeground(); got != th.Palette.Primary {
		t.Errorf("MarkerSuccess fg = %v, want Primary %v", got, th.Palette.Primary)
	}
	if got := th.MarkerRunning.GetForeground(); got != th.Palette.Primary {
		t.Errorf("MarkerRunning fg = %v, want Primary %v", got, th.Palette.Primary)
	}
	if got := th.MarkerQueued.GetForeground(); got != th.Palette.FgMuted {
		t.Errorf("MarkerQueued fg = %v, want FgMuted %v", got, th.Palette.FgMuted)
	}
	if got := th.MarkerAttention.GetForeground(); got != th.Palette.Accent {
		t.Errorf("MarkerAttention fg = %v, want Accent %v", got, th.Palette.Accent)
	}
}

// TestThemeBrandUsesPrimary locks in the Accent → Primary reassignment for
// brand-identity styles (spec §2 "Backward-compat" paragraph). Saffron is
// for attention/focus only; brand voice is moss.
func TestThemeBrandUsesPrimary(t *testing.T) {
	th := NewTheme()
	cases := map[string]lipgloss.Style{
		"StatusBar": th.StatusBar,
		"UserText":  th.UserText,
		"Header":    th.Header,
		"PickerSel": th.PickerSel,
	}
	for name, s := range cases {
		if got := s.GetForeground(); got != th.Palette.Primary {
			t.Errorf("%s fg = %v, want Primary %v", name, got, th.Palette.Primary)
		}
	}
	// Modal box uses the attention colour (saffron) for its border.
	if got := th.ModalBox.GetBorderTopForeground(); got != th.Palette.Accent {
		t.Errorf("ModalBox border fg = %v, want Accent %v", got, th.Palette.Accent)
	}
}

func TestApplyThemeMapsModes(t *testing.T) {
	cases := []struct {
		mode    string
		want    bool // value passed to themeApplier (only meaningful when called)
		applied bool // whether themeApplier was called at all
	}{
		{"light", false, true},
		{"dark", true, true},
		{"auto", false, false}, // auto leaves lipgloss alone
		{"", false, false},     // unknown values are a no-op
		{"neon", false, false}, // unknown values are a no-op
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			var got bool
			var called bool
			prev := themeApplier
			themeApplier = func(dark bool) { got = dark; called = true }
			defer func() { themeApplier = prev }()

			ApplyTheme(tc.mode)
			if called != tc.applied {
				t.Errorf("ApplyTheme(%q): called=%v, want %v", tc.mode, called, tc.applied)
			}
			if called && got != tc.want {
				t.Errorf("ApplyTheme(%q) passed %v, want %v", tc.mode, got, tc.want)
			}
		})
	}
}

func TestGlyph(t *testing.T) {
	cases := map[domain.JobStatus]string{
		domain.JobQueued:    MarkerQueuedGlyph,
		domain.JobRunning:   MarkerRunningGlyph,
		domain.JobSucceeded: MarkerSuccessGlyph,
		domain.JobFailed:    "✗",
		domain.JobCancelled: "⊘",
	}
	for st, want := range cases {
		if got := glyph(st); got != want {
			t.Errorf("glyph(%s) = %q, want %q", st, got, want)
		}
	}
}

// TestStatusMarkerRoutesThroughThemeStyles confirms that statusMarker
// returns non-empty output for every JobStatus and that the rendered
// glyph for the rebrand-covered states matches the corresponding constant.
func TestStatusMarkerRoutesThroughThemeStyles(t *testing.T) {
	th := NewTheme()
	cases := map[domain.JobStatus]string{
		domain.JobQueued:    MarkerQueuedGlyph,
		domain.JobRunning:   MarkerRunningGlyph,
		domain.JobSucceeded: MarkerSuccessGlyph,
	}
	for st, glyph := range cases {
		out := th.statusMarker(st)
		if !strings.Contains(out, glyph) {
			t.Errorf("statusMarker(%s) = %q, want it to contain %q", st, out, glyph)
		}
	}
	// Failed and cancelled keep their pre-rebrand glyphs.
	if !strings.Contains(th.statusMarker(domain.JobFailed), "✗") {
		t.Errorf("statusMarker(failed) missing ✗ glyph")
	}
	if !strings.Contains(th.statusMarker(domain.JobCancelled), "⊘") {
		t.Errorf("statusMarker(cancelled) missing ⊘ glyph")
	}
}
