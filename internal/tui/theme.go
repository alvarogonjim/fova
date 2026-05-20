// Package tui implements the Proteus terminal UI.
package tui

import "github.com/charmbracelet/lipgloss"

// Status colours (SPECS §10.5).
var (
	colorRunning   = lipgloss.AdaptiveColor{Light: "#2563EB", Dark: "#60A5FA"}
	colorSucceeded = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34D399"}
	colorFailed    = lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#F87171"}
	colorWarning   = lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#FBBF24"}
	colorMuted     = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	colorAccent    = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"}
)

// Theme groups the Lip Gloss styles the TUI uses.
type Theme struct {
	StatusBar  lipgloss.Style
	CommandBar lipgloss.Style
	UserText   lipgloss.Style
	AgentText  lipgloss.Style
	ToolTrace  lipgloss.Style
	Error      lipgloss.Style
	ModalBox   lipgloss.Style
	PickerSel  lipgloss.Style
}

// NewTheme returns the default light/dark-adaptive theme.
func NewTheme() Theme {
	return Theme{
		StatusBar:  lipgloss.NewStyle().Bold(true).Foreground(colorAccent),
		CommandBar: lipgloss.NewStyle().Foreground(colorMuted),
		UserText:   lipgloss.NewStyle().Bold(true).Foreground(colorRunning),
		AgentText:  lipgloss.NewStyle(),
		ToolTrace:  lipgloss.NewStyle().Foreground(colorMuted).Italic(true),
		Error:      lipgloss.NewStyle().Foreground(colorFailed).Bold(true),
		ModalBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorWarning).
			Padding(1, 2),
		PickerSel: lipgloss.NewStyle().Bold(true).Foreground(colorSucceeded),
	}
}
