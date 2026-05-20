package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// HeaderInput is the data RenderHeader needs to render the 6-line fova
// header (rebrand spec §3.1). All four fields are stable strings — none of
// them is required: RenderHeader degrades gracefully when any one is empty.
type HeaderInput struct {
	// Version is the line-1 right-column label, e.g. "fova 0.5.0-dev".
	Version string
	// Model is the line-2 right-column label, e.g. "qwen3.5-35b-a3b-fp8".
	// An empty Model renders a blank text cell so the brand mark still spans
	// all six lines without a gap.
	Model string
	// FullPath is the line-0 absolute project path, dim. e.g.
	// "/home/alvaro/fova/projects/binder-v3".
	FullPath string
	// ShortPath is the line-3 tilde-shortened cwd, dim. e.g.
	// "~/Projects/fova/binder-v3". Callers compute it; RenderHeader does not
	// reach into the environment.
	ShortPath string
}

// headerMarkColWidth is the fixed visible width of the brand-mark column in
// the header. The text column starts immediately after, at column 8. Picked
// to fit the widest mark line ("│ ╰──●", 7 runes) with one space of breathing
// room before the text.
const headerMarkColWidth = 8

// RenderHeader returns the six-line fova header (rebrand spec §3.1) as a
// single newline-joined string:
//
//	fova · <full path>                    ← line 0: dim
//	 ┌─╮     fova 0.5.0-dev               ← line 1: mark + version (sand)
//	 │ ╰──●  qwen3.5-35b-a3b-fp8          ← line 2: mark + model (sand, ● saffron)
//	 ├─╮    ~/Projects/fova/<name>        ← line 3: mark + short cwd (dim)
//	 │ ╰─                                  ← line 4: mark continued
//	 │                                     ← line 5: mark closer
//
// The brand mark occupies columns 0..7 (headerMarkColWidth) on lines 1-5;
// line 0 has no mark and starts at column 0. Empty mark cells are padded
// with spaces so the text column always aligns.
func RenderHeader(theme Theme, in HeaderInput) string {
	mark := RenderLogo(theme.Palette)

	muted := theme.Muted // dim, FgMuted
	// Sand foreground for the version + model labels. Built fresh rather than
	// reused from theme.Header (which is bold Primary) so the header text
	// reads as plain sand on the dark forest background.
	bodyStyle := lipgloss.NewStyle().Foreground(theme.Palette.Fg)

	// Line 0: "fova · <full path>" in dim, no mark.
	var line0 string
	if in.FullPath != "" {
		line0 = muted.Render("fova · " + in.FullPath)
	} else {
		line0 = muted.Render("fova")
	}

	// Lines 1-5: the brand mark in column 0, then padded gap, then the text
	// column. padMark right-pads each mark line so its *visible* width hits
	// headerMarkColWidth — styled escapes are not counted.
	line1 := padMark(mark[0]) + bodyStyle.Render(in.Version)
	line2 := padMark(mark[1]) + bodyStyle.Render(in.Model)
	line3 := padMark(mark[2]) + muted.Render(in.ShortPath)
	line4 := padMark(mark[3])
	line5 := padMark(mark[4])

	return strings.Join([]string{line0, line1, line2, line3, line4, line5}, "\n")
}

// padMark right-pads a (possibly styled) mark line to the fixed mark column
// width by appending plain spaces. The padding is computed against the
// line's *visible* rune count so ANSI escape sequences don't throw off the
// column alignment.
func padMark(line string) string {
	w := visibleRuneCount(line)
	if w >= headerMarkColWidth {
		return line
	}
	return line + strings.Repeat(" ", headerMarkColWidth-w)
}

// visibleRuneCount returns the number of printable runes in s, skipping CSI
// escape sequences. It's a single-purpose helper kept local to the header
// because the statusbar's visibleWidth has the same shape but in test code.
func visibleRuneCount(s string) int {
	n := 0
	inEsc := false
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case inEsc:
			// Final byte of a CSI is a letter in 0x40..0x7e (other than '[').
			if r >= 0x40 && r <= 0x7e && r != '[' {
				inEsc = false
			}
		case r == 0x1b:
			inEsc = true
		default:
			n++
		}
	}
	return n
}
