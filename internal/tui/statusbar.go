package tui

import (
	"fmt"
	"strings"
	"time"
)

// statusBarModel renders the slim header and the status footer (SPECS §10.7.6).
// The v0.1 combined status bar is split into headerView() (top) and
// footerView() (bottom).
type statusBarModel struct {
	theme      Theme
	width      int
	provider   string
	model      string
	cost       float64
	elapsed    time.Duration
	project    string
	ctxPercent int
}

func newStatusBarModel(th Theme) statusBarModel {
	return statusBarModel{theme: th}
}

// setProject records the active project name shown in the header.
func (s *statusBarModel) setProject(name string) { s.project = name }

// setContextPercent records the running token estimate as a percentage of the
// active model's context window.
func (s *statusBarModel) setContextPercent(pct int) { s.ctxPercent = pct }

// View renders the status bar. It delegates to headerView so app.go keeps
// compiling against the original API.
func (s statusBarModel) View() string { return s.headerView() }

// headerView renders the slim top bar: " proteus · <project> " in Header
// style. When no project is set it renders just " proteus ".
func (s statusBarModel) headerView() string {
	line := " proteus "
	if s.project != "" {
		line = " proteus · " + s.project + " "
	}
	if s.width > 0 {
		line = clipRunes(line, s.width)
	}
	return s.theme.Header.Render(line)
}

// footerView renders the bottom status line (SPECS §10.7.6):
//
//	<hint>   <model> · $<cost> · <NN>% context
//
// The context segment turns Warning above 80%. The line is clipped to width
// before any styling so the rendered output never exceeds the terminal.
func (s statusBarModel) footerView() string {
	hint := footerHint()
	left := fmt.Sprintf("%s   %s · $%.2f · ", hint, orDash(s.model), s.cost)
	ctx := fmt.Sprintf("%d%% context", s.ctxPercent)

	// Clip the plain text first so styling escapes are never counted or cut.
	if s.width > 0 {
		full := []rune(left + ctx)
		if len(full) > s.width {
			full = full[:s.width]
			leftRunes := []rune(left)
			if len(full) <= len(leftRunes) {
				left = string(full)
				ctx = ""
			} else {
				left = string(leftRunes)
				ctx = string(full[len(leftRunes):])
			}
		}
	}

	ctxStyle := s.theme.Footer
	if s.ctxPercent > 80 {
		ctxStyle = s.theme.Footer.Foreground(s.theme.Palette.Warning)
	}
	return s.theme.Footer.Render(left) + ctxStyle.Render(ctx)
}

// footerHint builds the slash-command hint from the first four catalogue
// entries, e.g. "/model  /provider  /clear  /help".
func footerHint() string {
	n := 4
	if n > len(slashCommands) {
		n = len(slashCommands)
	}
	parts := make([]string, 0, n)
	for _, c := range slashCommands[:n] {
		parts = append(parts, "/"+c.Name)
	}
	return strings.Join(parts, "  ")
}

// clipRunes truncates s to at most w runes (w<=0 means no clipping).
func clipRunes(s string, w int) string {
	if w <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w])
}

func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}
