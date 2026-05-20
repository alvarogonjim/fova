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

// commandBarModel is the multi-line message input (SPECS §10.7.2). The textarea
// is wrapped in a rounded border whose colour reflects the focus / turn state.
type commandBarModel struct {
	ta    textarea.Model
	width int

	theme   Theme
	focused bool
	running bool
}

func newCommandBarModel(th Theme, width int) commandBarModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message, or / for commands"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.SetWidth(textareaWidth(width))
	ta.SetHeight(3)
	ta.Focus()
	return commandBarModel{
		ta:      ta,
		width:   width,
		theme:   th,
		focused: true,
	}
}

func (m *commandBarModel) value() string { return m.ta.Value() }
func (m *commandBarModel) reset()        { m.ta.Reset() }

// setFocused records whether the message input currently holds keyboard focus.
func (m *commandBarModel) setFocused(f bool) { m.focused = f }

// setRunning records whether a turn is in flight (the agent has the floor).
func (m *commandBarModel) setRunning(r bool) { m.running = r }

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

// inputBorderStyle picks the border style for the current focus / turn state.
func (m commandBarModel) inputBorderStyle() lipgloss.Style {
	switch {
	case m.running:
		return m.theme.InputBorderBusy
	case m.focused:
		return m.theme.InputBorderActive
	default:
		return m.theme.InputBorder
	}
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

// View renders the textarea inside its rounded border with a dim "message"
// label. The border colour signals focus (Accent), a running turn (FgSubtle),
// or idle/unfocused (Border).
func (m commandBarModel) View() string {
	box := m.inputBorderStyle().Render(m.ta.View())
	label := m.theme.Muted.Render(m.inputLabel())
	return label + "\n" + box
}
