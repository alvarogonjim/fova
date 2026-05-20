package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
)

// entryKind classifies a chat entry.
type entryKind int

const (
	entryUser entryKind = iota
	entryAgent
	entryTool
	entryError
)

// chatEntry is one rendered block in the chat history.
type chatEntry struct {
	kind entryKind
	text string
	done bool // for tool entries: false = running
}

// chatModel renders the scrolling conversation.
type chatModel struct {
	theme    Theme
	viewport viewport.Model
	entries  []chatEntry
	renderer *glamour.TermRenderer
	width    int
}

func newChatModel(th Theme, width, height int) *chatModel {
	vp := viewport.New(width, height)
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	return &chatModel{theme: th, viewport: vp, renderer: r, width: width}
}

// resize updates the pane dimensions and the markdown wrap width.
func (c *chatModel) resize(width, height int) {
	c.width = width
	c.viewport.Width = width
	c.viewport.Height = height
	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	c.renderer = r
	c.refresh()
}

func (c *chatModel) appendUser(text string) {
	c.entries = append(c.entries, chatEntry{kind: entryUser, text: text})
	c.refresh()
}

// appendAgentDelta appends to the last agent entry, or starts a new one.
func (c *chatModel) appendAgentDelta(delta string) {
	if n := len(c.entries); n > 0 && c.entries[n-1].kind == entryAgent {
		c.entries[n-1].text += delta
	} else {
		c.entries = append(c.entries, chatEntry{kind: entryAgent, text: delta})
	}
	c.refresh()
}

func (c *chatModel) appendError(text string) {
	c.entries = append(c.entries, chatEntry{kind: entryError, text: text})
	c.refresh()
}

func (c *chatModel) appendToolStart(name string) {
	c.entries = append(c.entries, chatEntry{kind: entryTool, text: name + " …", done: false})
	c.refresh()
}

// appendToolDone marks the most recent unfinished tool entry as done.
func (c *chatModel) appendToolDone(name, display string) {
	for i := len(c.entries) - 1; i >= 0; i-- {
		if c.entries[i].kind == entryTool && !c.entries[i].done {
			c.entries[i].text = name + " → " + firstLine(display)
			c.entries[i].done = true
			c.refresh()
			return
		}
	}
	c.entries = append(c.entries, chatEntry{kind: entryTool, text: name + " → " + firstLine(display), done: true})
	c.refresh()
}

// appendAgentDeltaBlock appends text as a standalone agent entry.
func (c *chatModel) appendAgentDeltaBlock(text string) {
	c.entries = append(c.entries, chatEntry{kind: entryAgent, text: text})
	c.refresh()
}

// renderEntries returns the full conversation as a string (used by tests).
func (c *chatModel) renderEntries() string {
	var b strings.Builder
	for _, e := range c.entries {
		switch e.kind {
		case entryUser:
			b.WriteString(c.theme.UserText.Render("› " + e.text))
		case entryAgent:
			md, err := c.renderer.Render(e.text)
			if err != nil {
				md = e.text
			}
			b.WriteString(c.theme.AgentText.Render(strings.TrimRight(md, "\n")))
		case entryTool:
			b.WriteString(c.theme.ToolTrace.Render("⚙ " + e.text))
		case entryError:
			b.WriteString(c.theme.Error.Render("✗ " + e.text))
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func (c *chatModel) refresh() {
	c.viewport.SetContent(c.renderEntries())
	c.viewport.GotoBottom()
}

func (c *chatModel) View() string { return c.viewport.View() }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
