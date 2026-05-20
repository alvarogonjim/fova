// Package tui implements the Proteus terminal UI.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/alvarogonjim/proteus/internal/domain"
)

// Palette is the v0.4 design-token set (SPECS §10.5). Every UI style is derived
// from these semantic roles — no view code hard-codes a hex value. Colours are
// AdaptiveColor so the TUI renders correctly on light and dark terminals.
type Palette struct {
	Fg       lipgloss.AdaptiveColor // primary text
	FgMuted  lipgloss.AdaptiveColor // tool output, hints, footer
	FgSubtle lipgloss.AdaptiveColor // section rules, placeholders, unfocused border
	Accent   lipgloss.AdaptiveColor // the single brand colour
	Border   lipgloss.AdaptiveColor // input border when unfocused

	Queued    lipgloss.AdaptiveColor
	Running   lipgloss.AdaptiveColor
	Succeeded lipgloss.AdaptiveColor
	Failed    lipgloss.AdaptiveColor
	Warning   lipgloss.AdaptiveColor
}

// DefaultPalette is the single adaptive palette shipped in v0.4. The status
// colours keep their v0.2 hex values; the foreground roles are new.
var DefaultPalette = Palette{
	Fg:        lipgloss.AdaptiveColor{Light: "#1F2937", Dark: "#E5E7EB"},
	FgMuted:   lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"},
	FgSubtle:  lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#4B5563"},
	Accent:    lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"},
	Border:    lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#374151"},
	Queued:    lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"},
	Running:   lipgloss.AdaptiveColor{Light: "#2563EB", Dark: "#60A5FA"},
	Succeeded: lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34D399"},
	Failed:    lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#F87171"},
	Warning:   lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#FBBF24"},
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
}

// NewTheme returns the default light/dark-adaptive theme.
func NewTheme() Theme { return NewThemeFromPalette(DefaultPalette) }

// NewThemeFromPalette builds a Theme from an explicit palette.
func NewThemeFromPalette(p Palette) Theme {
	rounded := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	return Theme{
		Palette:    p,
		StatusBar:  lipgloss.NewStyle().Bold(true).Foreground(p.Accent),
		CommandBar: lipgloss.NewStyle().Foreground(p.FgMuted),
		UserText:   lipgloss.NewStyle().Bold(true).Foreground(p.Accent),
		AgentText:  lipgloss.NewStyle().Foreground(p.Fg),
		ToolTrace:  lipgloss.NewStyle().Foreground(p.FgMuted),
		Error:      lipgloss.NewStyle().Bold(true).Foreground(p.Failed),
		ModalBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Warning).
			Padding(1, 2),
		PickerSel: lipgloss.NewStyle().Bold(true).Foreground(p.Accent),

		Header:            lipgloss.NewStyle().Bold(true).Foreground(p.Accent),
		Footer:            lipgloss.NewStyle().Foreground(p.FgMuted),
		Hint:              lipgloss.NewStyle().Foreground(p.FgMuted),
		Muted:             lipgloss.NewStyle().Foreground(p.FgMuted),
		Subtle:            lipgloss.NewStyle().Foreground(p.FgSubtle),
		SectionRule:       lipgloss.NewStyle().Foreground(p.FgSubtle),
		InputBorder:       rounded.BorderForeground(p.Border),
		InputBorderActive: rounded.BorderForeground(p.Accent),
		InputBorderBusy:   rounded.BorderForeground(p.FgSubtle),
	}
}

// glyph returns the single-rune status indicator for a job status
// (SPECS §10.7.8). It is the single source of truth for status glyphs.
func glyph(s domain.JobStatus) string {
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
