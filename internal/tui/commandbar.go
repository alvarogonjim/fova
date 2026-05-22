package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

// parseSlashCommand splits an input line. If it starts with "/", it returns
// the command word, the remaining argument, and true.
func parseSlashCommand(line string) (cmd, arg string, isSlash bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return "", "", false
	}
	body := strings.TrimPrefix(line, "/")
	if i := strings.IndexByte(body, ' '); i >= 0 {
		return body[:i], strings.TrimSpace(body[i+1:]), true
	}
	return body, "", true
}

// inputMinHeight / inputMaxHeight bound the auto-growing textarea. The bar
// starts at one line and grows up to inputMaxHeight as the user types — past
// that the textarea scrolls internally and the user can hand off to $EDITOR
// with Ctrl+X.
const (
	inputMinHeight = 1
	inputMaxHeight = 8
)

// commandBarModel is the message input (SPECS §10.7.2 + fova rebrand §3.6).
// The textarea is wrapped in a rounded border that stays moss-dim in every
// state — the per-state distinction lives in the `›` prompt and cursor block:
// moss when idle / running, saffron when a confirmation modal is awaiting.
// Height tracks the content (refreshHeight) between inputMinHeight and
// inputMaxHeight.
type commandBarModel struct {
	ta    textarea.Model
	width int

	theme    Theme
	focused  bool
	active   bool // the input has the keyboard (false while a panel is focused)
	running  bool
	awaiting bool // a confirmation modal is open and waiting on the user
}

func newCommandBarModel(th Theme, width int) commandBarModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message, or / for commands"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.SetWidth(textareaWidth(width))
	ta.SetHeight(inputMinHeight)
	ta.Focus()
	m := commandBarModel{
		ta:      ta,
		width:   width,
		theme:   th,
		focused: true,
		active:  true,
	}
	m.applyPromptStyle()
	return m
}

func (m *commandBarModel) value() string { return m.ta.Value() }

// reset clears the textarea and collapses it back to inputMinHeight so the
// next message starts on a single line.
func (m *commandBarModel) reset() {
	m.ta.Reset()
	m.ta.SetHeight(inputMinHeight)
}

// inputHeight returns the textarea's current line height (1..inputMaxHeight).
// The total command-bar height adds the label (1), the top/bottom border
// (2 rows), so callers reserving rows for the bar use `inputHeight() + 3`.
func (m *commandBarModel) inputHeight() int { return m.ta.Height() }

// refreshHeight grows or shrinks the textarea to fit its current content,
// clamped to [inputMinHeight, inputMaxHeight]. Returns true when the height
// changed, so the parent can re-layout the chat pane.
func (m *commandBarModel) refreshHeight() bool {
	want := m.ta.LineCount()
	if want < inputMinHeight {
		want = inputMinHeight
	}
	if want > inputMaxHeight {
		want = inputMaxHeight
	}
	if want == m.ta.Height() {
		return false
	}
	m.ta.SetHeight(want)
	return true
}

// setFocused records whether the message input currently holds keyboard focus.
func (m *commandBarModel) setFocused(f bool) { m.focused = f }

// setActive records whether the message input currently has the keyboard.
// While a side panel holds focus the input is inactive and renders dimmed.
func (m *commandBarModel) setActive(a bool) { m.active = a }

// setRunning records whether a turn is in flight (the agent has the floor).
func (m *commandBarModel) setRunning(r bool) {
	m.running = r
	m.applyPromptStyle()
}

// setAwaitingConfirm records whether a confirmation modal is open (rebrand
// spec §3.6). The prompt `›` and cursor block flip to saffron while this is
// true; otherwise they render in moss.
func (m *commandBarModel) setAwaitingConfirm(a bool) {
	m.awaiting = a
	m.applyPromptStyle()
}

// applyPromptStyle repaints the textarea's prompt + cursor based on the
// awaiting-confirm flag. Moss is the brand colour; saffron is the only
// "awaiting user" signal we expose in the input row.
func (m *commandBarModel) applyPromptStyle() {
	colour := m.theme.Palette.Primary
	if m.awaiting {
		colour = m.theme.Palette.Accent
	}
	promptStyle := lipgloss.NewStyle().Foreground(colour)
	m.ta.FocusedStyle.Prompt = promptStyle
	m.ta.BlurredStyle.Prompt = promptStyle
	if m.awaiting {
		m.ta.Cursor.Style = lipgloss.NewStyle().Foreground(colour)
	} else {
		m.ta.Cursor.Style = lipgloss.NewStyle()
	}
}

func (m *commandBarModel) setWidth(w int) {
	m.width = w
	m.ta.SetWidth(textareaWidth(w))
}

// textareaWidth reserves the columns consumed by the rounded border and the
// Padding(0,1) of the box (2 border + 2 padding = 4), clamped to a small floor.
func textareaWidth(boxWidth int) int {
	w := boxWidth - 4
	if w < 8 {
		w = 8
	}
	return w
}

// inputBorderStyle returns the single moss-dim rounded border the rebrand
// spec (§3.6) prescribes. The border colour no longer changes with focus or
// turn state — that distinction now lives in the `›` prompt + cursor style.
// The three Theme.InputBorder* slots remain on the theme for back-compat but
// all three resolve to the same moss-dim colour after Agent A's palette work.
func (m commandBarModel) inputBorderStyle() lipgloss.Style {
	return m.theme.InputBorder
}

// inputLabel is the dim caption shown above the message box. It always
// contains the word "message"; while a turn runs it gains a "· busy" hint so
// the state is legible even on terminals without colour.
func (m commandBarModel) inputLabel() string {
	if m.running {
		return "message · busy"
	}
	return "message"
}

// View renders the textarea inside its moss-dim rounded border with a dim
// "message" label. Per rebrand §3.6 the border is colour-stable across all
// states; the only colour cue lives in the `›` prompt + cursor block.
func (m commandBarModel) View() string {
	box := m.inputBorderStyle().Render(m.ta.View())
	label := m.theme.Muted.Render(m.inputLabel())
	if !m.active {
		label = m.theme.Subtle.Render(m.inputLabel() + " · panel focus — esc to type")
	}
	return label + "\n" + box
}
