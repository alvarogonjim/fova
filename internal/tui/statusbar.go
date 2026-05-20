package tui

import (
	"fmt"
	"time"
)

// statusBarModel renders the top bar (SPECS §10.2).
type statusBarModel struct {
	theme    Theme
	width    int
	provider string
	model    string
	cost     float64
	elapsed  time.Duration
}

func newStatusBarModel(th Theme) statusBarModel {
	return statusBarModel{theme: th}
}

// View renders the status bar as a single line.
func (s statusBarModel) View() string {
	el := time.Duration(0)
	if s.elapsed > 0 {
		el = s.elapsed.Round(time.Second)
	}
	line := fmt.Sprintf(" proteus  ·  Model: %s  ·  Provider: %s  ·  $%.2f / %s ",
		orDash(s.model), orDash(s.provider), s.cost, el)
	if s.width > 0 && len(line) > s.width {
		line = line[:s.width]
	}
	return s.theme.StatusBar.Render(line)
}

func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}
