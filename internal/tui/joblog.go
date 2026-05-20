package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// readLog reads and returns the whole contents of a job log file. A missing
// file or an empty path yields "" with no error — the full-screen view simply
// shows nothing rather than crashing (design doc §6).
func readLog(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// tailLines returns the last n lines of the file at path, newest-last. A
// missing/empty file or a non-positive n yields an empty slice.
func tailLines(path string, n int) []string {
	if path == "" || n <= 0 {
		return []string{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	text := strings.TrimRight(string(b), "\n")
	if text == "" {
		return []string{}
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// jobLogView is the full-screen, scrollable view of a job's complete log
// (design doc §4.5). It wraps a bubbles/viewport for the log body and renders
// a styled header line above it.
type jobLogView struct {
	theme    Theme
	viewport viewport.Model
	header   string
}

// newJobLogView returns a jobLogView with an empty viewport.
func newJobLogView(th Theme) jobLogView {
	return jobLogView{theme: th, viewport: viewport.New(0, 0)}
}

// setSize resizes the inner viewport. The header occupies one line, so the
// viewport gets the remaining height.
func (v *jobLogView) setSize(w, h int) {
	if w < 0 {
		w = 0
	}
	v.viewport.Width = w
	vh := h - 1
	if vh < 0 {
		vh = 0
	}
	v.viewport.Height = vh
}

// setContent stores the header line and sets the viewport content to the full
// log body.
func (v *jobLogView) setContent(header, body string) {
	v.header = header
	v.viewport.SetContent(body)
}

// update routes scroll keys (↑/↓/PgUp/PgDn, and k/j) to the viewport and
// returns the updated jobLogView. The viewport's default key map already binds
// all of these.
func (v jobLogView) update(msg tea.KeyMsg) jobLogView {
	v.viewport, _ = v.viewport.Update(msg)
	return v
}

// View renders the styled header line above the viewport's view.
func (v jobLogView) View() string {
	return v.theme.Header.Render(v.header) + "\n" + v.viewport.View()
}
