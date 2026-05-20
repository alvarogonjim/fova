package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
)

// slashCommandHints is shown in the command bar (SPECS §10.2 / §10.3).
const slashCommandHints = " /model  /provider  /plan  /clear  /help  /quit "

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

// commandBarModel is the multi-line input plus the slash-command hint line.
type commandBarModel struct {
	ta    textarea.Model
	hints string
	width int
}

func newCommandBarModel(th Theme, width int) commandBarModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message, or /help …"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.SetWidth(width)
	ta.SetHeight(3)
	ta.Focus()
	return commandBarModel{ta: ta, hints: slashCommandHints, width: width}
}

func (m *commandBarModel) value() string { return m.ta.Value() }
func (m *commandBarModel) reset()        { m.ta.Reset() }
func (m *commandBarModel) setWidth(w int) {
	m.width = w
	m.ta.SetWidth(w)
}
