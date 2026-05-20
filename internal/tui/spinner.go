package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// spinnerFrames are the Braille spinner glyphs cycled by the thinking
// indicator (SPECS §10.7.4).
var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// spinnerTickInterval is the cadence of the spinner animation.
const spinnerTickInterval = 80 * time.Millisecond

// spinnerTickMsg advances the thinking indicator to its next frame. app.go
// (Phase C) re-issues spinnerTick on each one so the animation keeps running.
type spinnerTickMsg struct{}

// spinnerTick returns a command that fires a spinnerTickMsg after ~80 ms.
func spinnerTick() tea.Cmd {
	return tea.Tick(spinnerTickInterval, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// thinkingModel is the animated "thinking" status line shown directly above
// the message input while a turn is running (SPECS §10.7.4).
type thinkingModel struct {
	on    bool      // whether the indicator is showing
	frame int       // index into spinnerFrames
	verb  string    // current activity verb (Designing, Folding, …)
	since time.Time // turn start time, for the elapsed counter
}

// start begins the indicator with the given verb and start time.
func (t *thinkingModel) start(verb string, at time.Time) {
	t.on = true
	t.frame = 0
	t.verb = verb
	t.since = at
}

// stop clears the indicator.
func (t *thinkingModel) stop() {
	t.on = false
	t.frame = 0
	t.verb = ""
	t.since = time.Time{}
}

// tick advances the spinner to the next frame, wrapping at the end.
func (t *thinkingModel) tick() {
	if len(spinnerFrames) == 0 {
		return
	}
	t.frame = (t.frame + 1) % len(spinnerFrames)
}

// active reports whether the indicator is currently showing.
func (t thinkingModel) active() bool { return t.on }

// view renders the indicator line styled with Theme.Muted, or "" when the
// indicator is not active. now is injected so callers (and tests) control the
// elapsed-seconds counter; elapsed is the whole seconds between the start time
// and now.
func (t thinkingModel) view(th Theme, now time.Time) string {
	if !t.on {
		return ""
	}
	frame := spinnerFrames[t.frame%len(spinnerFrames)]
	elapsed := int(now.Sub(t.since).Seconds())
	if elapsed < 0 {
		elapsed = 0
	}
	line := fmt.Sprintf("%c %s… (%ds · esc to interrupt)", frame, t.verb, elapsed)
	return th.Muted.Render(line)
}

// verbForTool maps a tool name to a human activity verb. The match is on
// case-insensitive substrings of the tool name; unknown tools default to
// "Thinking" (SPECS §10.7.4).
func verbForTool(tool string) string {
	name := strings.ToLower(tool)
	switch {
	case containsAny(name, "diffusion", "design", "mpnn"):
		return "Designing"
	case containsAny(name, "fold", "esmfold", "boltz", "chai"):
		return "Folding"
	case containsAny(name, "score", "ipsae", "metric"):
		return "Scoring"
	case containsAny(name, "search", "knowledge", "web"):
		return "Searching"
	default:
		return "Thinking"
	}
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
