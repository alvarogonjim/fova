package tui

// statusBarModel is, post-rebrand, the footer-hint line only (rebrand spec
// §3.1: the title role moved to the new RenderHeader). Many of its fields
// (model, cost, costLimit, ctxPercent) are kept as no-op state so the rest
// of the TUI (app.go, addTurnCost, /model, /reload) can still write to them
// without compiling errors — they simply do not surface in the view today.
// A future surface (e.g. a "cost" peek pane) can re-read them.
type statusBarModel struct {
	theme      Theme
	width      int
	provider   string
	model      string
	cost       float64
	costLimit  float64
	project    string
	ctxPercent int
	replay     string // "replay X/Y" — set only in replay mode
}

func newStatusBarModel(th Theme) statusBarModel {
	return statusBarModel{theme: th}
}

// setProject is retained for API compatibility with app.go and the v0.4
// header-takes-the-project-path behaviour. The new header derives the path
// from m.fovaHome directly, so this is now a no-op-ish setter that just
// records the project name in case a future surface wants it.
func (s *statusBarModel) setProject(name string) { s.project = name }

// setContextPercent records the running token estimate. Held for the same
// "future surface" reason as setProject: today the footer is a static hint
// line, but the percentage is still wired up in case it returns.
func (s *statusBarModel) setContextPercent(pct int) { s.ctxPercent = pct }

// footerHintText is the static slash-command hint displayed under the
// message input (rebrand spec §3.1).
const footerHintText = "type a message, or / for commands  ·  /keys  ·  ctrl+x for $EDITOR"

// View returns the footer hint line. Kept named View() for compat with the
// pre-rebrand call sites; today there is no separate "header" view on the
// status bar — that role moved to RenderHeader.
func (s statusBarModel) View() string { return s.footerView() }

// footerView renders the bottom hint line. In replay mode it appends a
// " · replay X/Y" segment so the user always knows which event they're on.
func (s statusBarModel) footerView() string {
	hint := footerHintText
	if s.replay != "" {
		hint += "  ·  " + s.replay
	}
	if s.width > 0 {
		hint = clipRunes(hint, s.width)
	}
	return s.theme.Footer.Render(hint)
}

// clipRunes truncates s to at most w runes (w<=0 means no clipping). Used
// by footerView to keep the hint line within the terminal width on narrow
// terminals; the dim style remains intact because clipping happens before
// rendering.
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

// orDash returns "—" for an empty input so missing labels (model id,
// provider name, target id) read uniformly across the TUI. Retained from
// the v0.4 statusbar because submit-modal helpers (app.go) still call it.
func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}
