package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// keysOverlay is the /keys modal. It renders the keybindings table inside a
// rounded warning-bordered box (the same style as the confirmation modal) so
// it reads as an overlay, not as inline chat content.
type keysOverlay struct{}

func newKeysOverlay() keysOverlay { return keysOverlay{} }

// view returns the rendered overlay, sized for a terminal of the given total
// width. Width 0 falls back to a sensible default so the test factory works
// without a WindowSizeMsg.
func (k keysOverlay) view(th Theme, width int) string {
	if width <= 0 {
		width = 80
	}

	rows := keybindings()
	// First column is the key, padded to the widest key in the table for
	// alignment. The second column is the description.
	keyWidth := 0
	for _, b := range rows {
		if w := lipgloss.Width(b.Key); w > keyWidth {
			keyWidth = w
		}
	}
	keyStyle := th.AgentText.Bold(true)
	descStyle := th.Muted

	var lines []string
	lines = append(lines, th.AgentText.Bold(true).Render("Keybindings"))
	lines = append(lines, "")
	for _, b := range rows {
		key := keyStyle.Render(padRight(b.Key, keyWidth))
		lines = append(lines, key+"  "+descStyle.Render(b.Description))
	}
	lines = append(lines, "")
	lines = append(lines, th.Subtle.Render("Esc to close"))

	body := strings.Join(lines, "\n")
	// Box width: just wide enough for the longest line, capped at width-4.
	maxLine := 0
	for _, ln := range strings.Split(body, "\n") {
		if w := lipgloss.Width(ln); w > maxLine {
			maxLine = w
		}
	}
	if maxLine > width-4 {
		maxLine = width - 4
	}
	return th.ModalBox.Width(maxLine).Render(body)
}

// padRight pads s with spaces on the right so its display width is at least w.
// Used to align the key column in the keys overlay.
func padRight(s string, w int) string {
	if pad := w - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}
