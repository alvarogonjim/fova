package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// LogoLines is the fova folded-F brand mark, 5 lines (rebrand spec §3.2).
//
// Each line is exported separately so callers can lay it out beside other
// content — for example the header text column, or a future /about splash.
// Centralising the glyphs here prevents drift between consumers.
//
// The endpoint dot `●` on line 1 is the saffron accent; everything else is
// moss in the rendered form (see RenderLogo).
var LogoLines = [5]string{
	" ┌─╮",
	" │ ╰──●",
	" ├─╮",
	" │ ╰─",
	" │",
}

// logoEndpoint is the rune that marks the saffron accent on line index 1 of
// the brand mark. Kept as a named constant so RenderLogo and tests agree.
const logoEndpoint = "●"

// RenderLogo returns the 5-line folded-F brand mark styled with the given
// palette: moss for the strokes (Palette.Primary) and saffron for the single
// endpoint dot on line 2 (Palette.Accent).
//
// Lines are returned individually so the caller controls layout — typically
// they are stacked vertically and padded into a fixed column to align with
// text on their right (see RenderHeader).
func RenderLogo(p Palette) [5]string {
	moss := lipgloss.NewStyle().Foreground(p.Primary)
	saffron := lipgloss.NewStyle().Foreground(p.Accent)

	var out [5]string
	for i, line := range LogoLines {
		if i == 1 && strings.Contains(line, logoEndpoint) {
			// Split at the saffron endpoint so only that rune picks up
			// the accent style; the surrounding stroke stays moss.
			head, tail, _ := strings.Cut(line, logoEndpoint)
			out[i] = moss.Render(head) + saffron.Render(logoEndpoint) + moss.Render(tail)
			continue
		}
		out[i] = moss.Render(line)
	}
	return out
}
