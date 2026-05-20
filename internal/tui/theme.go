// Package tui implements the fova terminal UI.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/alvarogonjim/fova/internal/domain"
)

// Palette is the fova design-token set (SPECS §10.5 + 2026-05-20 rebrand
// spec §2). Every UI style is derived from these semantic roles — no view
// code hard-codes a hex value.
//
// The mockup is dark-only, so the brand roles flatten Light and Dark to the
// same value rather than inventing a light variant. Failed keeps an adaptive
// shape because it is not depicted in the mockup and a sensible default is
// useful on light terminals.
type Palette struct {
	Bg       lipgloss.AdaptiveColor // dark forest background (forced dark)
	Fg       lipgloss.AdaptiveColor // primary text (sand)
	FgMuted  lipgloss.AdaptiveColor // tool output, hints, footer (dim)
	FgSubtle lipgloss.AdaptiveColor // section rules, idle borders (moss-dim)
	Primary  lipgloss.AdaptiveColor // brand / agent voice / success (moss)
	Accent   lipgloss.AdaptiveColor // attention, focus, cost, modal (saffron)
	Border   lipgloss.AdaptiveColor // input border when unfocused (moss-dim)

	Queued    lipgloss.AdaptiveColor
	Running   lipgloss.AdaptiveColor
	Succeeded lipgloss.AdaptiveColor
	Failed    lipgloss.AdaptiveColor
	Warning   lipgloss.AdaptiveColor
}

// DefaultPalette is the fova adaptive palette. The brand roles (Bg, Fg,
// Primary, Accent, …) are dark-only — Light and Dark share the same value
// — because the mockup is dark-only. Failed retains an adaptive value.
var DefaultPalette = Palette{
	Bg:        lipgloss.AdaptiveColor{Light: "#0d1f15", Dark: "#0d1f15"},
	Fg:        lipgloss.AdaptiveColor{Light: "#d4cfc0", Dark: "#d4cfc0"},
	FgMuted:   lipgloss.AdaptiveColor{Light: "#6b7a6b", Dark: "#6b7a6b"},
	FgSubtle:  lipgloss.AdaptiveColor{Light: "#4a7e2a", Dark: "#4a7e2a"},
	Primary:   lipgloss.AdaptiveColor{Light: "#7fc14a", Dark: "#7fc14a"},
	Accent:    lipgloss.AdaptiveColor{Light: "#EF9F27", Dark: "#EF9F27"},
	Border:    lipgloss.AdaptiveColor{Light: "#4a7e2a", Dark: "#4a7e2a"},
	Queued:    lipgloss.AdaptiveColor{Light: "#6b7a6b", Dark: "#6b7a6b"},
	Running:   lipgloss.AdaptiveColor{Light: "#7fc14a", Dark: "#7fc14a"},
	Succeeded: lipgloss.AdaptiveColor{Light: "#7fc14a", Dark: "#7fc14a"},
	Failed:    lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#F87171"},
	Warning:   lipgloss.AdaptiveColor{Light: "#EF9F27", Dark: "#EF9F27"},
}

// Theme groups the Lip Gloss styles the TUI uses, all derived from a Palette.
type Theme struct {
	Palette Palette

	// v0.1 fields — retained so existing views keep compiling.
	StatusBar  lipgloss.Style
	CommandBar lipgloss.Style
	UserText   lipgloss.Style
	AgentText  lipgloss.Style
	ToolTrace  lipgloss.Style
	Error      lipgloss.Style
	ModalBox   lipgloss.Style
	PickerSel  lipgloss.Style

	// v0.4 fields (SPECS §10.7).
	Header            lipgloss.Style // slim top bar
	Footer            lipgloss.Style // bottom status line
	Hint              lipgloss.Style // footer slash-command hints
	Muted             lipgloss.Style // secondary text, tool output
	Subtle            lipgloss.Style // rules, placeholders, truncation notes
	SectionRule       lipgloss.Style // panel label + horizontal rule
	InputBorder       lipgloss.Style // message input, idle / unfocused
	InputBorderActive lipgloss.Style // message input, focused
	InputBorderBusy   lipgloss.Style // message input, turn running

	// fova-rebrand fields (spec §3.4) — status glyphs for the jobs panel.
	// The glyph itself is pre-rendered into the style so callers just write
	// `theme.MarkerSuccess.Render("")` or use `.String()`-style helpers.
	MarkerSuccess   lipgloss.Style // ✓ in Primary (moss)
	MarkerRunning   lipgloss.Style // ⠿ in Primary (moss) — single-frame style
	MarkerQueued    lipgloss.Style // ○ in FgMuted (dim)
	MarkerAttention lipgloss.Style // ▸ in Accent (saffron)
}

// themeApplier is the seam ApplyTheme uses to set the global background mode.
// Production code points it at lipgloss.SetHasDarkBackground; tests swap it
// for a recording stub. It is a package-level variable rather than a parameter
// so callers (cmd/fova, /theme handler) can keep calling ApplyTheme without
// threading a dependency through.
var themeApplier = lipgloss.SetHasDarkBackground

// ApplyTheme forces the lipgloss adaptive-colour mode based on cfg.UI.Theme.
// "light" → SetHasDarkBackground(false); "dark" → SetHasDarkBackground(true);
// "auto" (or any unknown value) → no-op, leaving lipgloss's auto-detection
// alone. Calling ApplyTheme is safe at any time — it merely flips a global
// flag the next style.Render() will read.
func ApplyTheme(mode string) {
	switch mode {
	case "light":
		themeApplier(false)
	case "dark":
		themeApplier(true)
	}
}

// NewTheme returns the default light/dark-adaptive theme.
func NewTheme() Theme { return NewThemeFromPalette(DefaultPalette) }

// NewThemeFromPalette builds a Theme from an explicit palette.
//
// Note on Accent vs Primary: in the fova palette, Primary (moss) is the
// brand / agent / success role and Accent (saffron) is the attention /
// focus / cost role. Styles that previously used Accent for brand identity
// (StatusBar, UserText, Header, PickerSel) now consume Primary.
func NewThemeFromPalette(p Palette) Theme {
	rounded := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	return Theme{
		Palette:    p,
		StatusBar:  lipgloss.NewStyle().Bold(true).Foreground(p.Primary),
		CommandBar: lipgloss.NewStyle().Foreground(p.FgMuted),
		UserText:   lipgloss.NewStyle().Bold(true).Foreground(p.Primary),
		AgentText:  lipgloss.NewStyle().Foreground(p.Fg),
		ToolTrace:  lipgloss.NewStyle().Foreground(p.FgMuted),
		Error:      lipgloss.NewStyle().Bold(true).Foreground(p.Failed),
		ModalBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Accent).
			Padding(1, 2),
		PickerSel: lipgloss.NewStyle().Bold(true).Foreground(p.Primary),

		Header:            lipgloss.NewStyle().Bold(true).Foreground(p.Primary),
		Footer:            lipgloss.NewStyle().Foreground(p.FgMuted),
		Hint:              lipgloss.NewStyle().Foreground(p.FgMuted),
		Muted:             lipgloss.NewStyle().Foreground(p.FgMuted),
		Subtle:            lipgloss.NewStyle().Foreground(p.FgSubtle),
		SectionRule:       lipgloss.NewStyle().Foreground(p.FgSubtle),
		InputBorder:       rounded.BorderForeground(p.Border),
		InputBorderActive: rounded.BorderForeground(p.Accent),
		InputBorderBusy:   rounded.BorderForeground(p.FgSubtle),

		MarkerSuccess:   lipgloss.NewStyle().Foreground(p.Primary),
		MarkerRunning:   lipgloss.NewStyle().Foreground(p.Primary),
		MarkerQueued:    lipgloss.NewStyle().Foreground(p.FgMuted),
		MarkerAttention: lipgloss.NewStyle().Foreground(p.Accent),
	}
}

// Marker glyph constants (spec §3.4). These are the runes the rebrand markers
// render; pairing them with the Theme.Marker* styles is the only correct way
// to display a job's status in the jobs panel.
const (
	MarkerSuccessGlyph   = "✓"
	MarkerRunningGlyph   = "⠿"
	MarkerQueuedGlyph    = "○"
	MarkerAttentionGlyph = "▸"
)

// glyph returns the single-rune status indicator for a job status. It is the
// single source of truth for status glyphs in the jobs panel (spec §3.4).
// The "failed" and "cancelled" glyphs are not depicted in the mockup; their
// pre-rebrand glyphs are retained.
func glyph(s domain.JobStatus) string {
	switch s {
	case domain.JobRunning:
		return MarkerRunningGlyph
	case domain.JobSucceeded:
		return MarkerSuccessGlyph
	case domain.JobFailed:
		return "✗"
	case domain.JobCancelled:
		return "⊘"
	default: // queued
		return MarkerQueuedGlyph
	}
}

// statusColor returns the palette colour for a job status.
func (t Theme) statusColor(s domain.JobStatus) lipgloss.AdaptiveColor {
	switch s {
	case domain.JobRunning:
		return t.Palette.Running
	case domain.JobSucceeded:
		return t.Palette.Succeeded
	case domain.JobFailed:
		return t.Palette.Failed
	case domain.JobCancelled:
		return t.Palette.FgMuted
	default: // queued
		return t.Palette.Queued
	}
}

// statusMarker returns the pre-styled glyph string for a job status, using
// the Theme.Marker* styles where the rebrand spec defines them and falling
// back to a per-status foreground style for failed/cancelled (no marker
// style is defined for those in the mockup).
func (t Theme) statusMarker(s domain.JobStatus) string {
	switch s {
	case domain.JobQueued:
		return t.MarkerQueued.Render(MarkerQueuedGlyph)
	case domain.JobRunning:
		return t.MarkerRunning.Render(MarkerRunningGlyph)
	case domain.JobSucceeded:
		return t.MarkerSuccess.Render(MarkerSuccessGlyph)
	case domain.JobFailed:
		return lipgloss.NewStyle().Foreground(t.Palette.Failed).Render("✗")
	case domain.JobCancelled:
		return lipgloss.NewStyle().Foreground(t.Palette.FgMuted).Render("⊘")
	default:
		return t.MarkerQueued.Render(MarkerQueuedGlyph)
	}
}
