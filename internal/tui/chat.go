package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/alvarogonjim/proteus/internal/domain"
)

// entryKind classifies a chat entry.
type entryKind int

const (
	entryUser entryKind = iota
	entryAgent
	entryTool
	entryError
	entryWelcome
	entryJobLog
)

// toolBodyMaxLines caps how many result lines a tool trace renders before it is
// truncated with a `… +N lines` footer (SPECS §10.7.5).
const toolBodyMaxLines = 6

// chatEntry is one rendered block in the chat history.
type chatEntry struct {
	kind entryKind
	text string // user/agent/error/welcome text; tool name for entryTool
	done bool   // for tool entries: false = running

	// Tool-trace fields (SPECS §10.7.5).
	result  string        // result body shown under the ⎿ connector
	toolErr bool          // true when the tool call failed
	started time.Time     // wall-clock start, recorded by appendToolStart
	dur     time.Duration // elapsed time, recorded by appendToolDone
	hasDur  bool          // true once a duration has been recorded

	// Job-log fields (entryJobLog, design §4.4): a compact, auto-updating block
	// per job showing the tail of its log file.
	jobID      string           // the job's ID, used to update the block in place
	jobTool    string           // the tool name shown in the header
	jobStatus  domain.JobStatus // current job status, drives the header glyph
	jobStarted *time.Time       // wall-clock start, used for the elapsed column
	jobTail    []string         // the last ~6 log lines
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

// appendWelcome appends the startup welcome block as an entryWelcome chat entry
// (SPECS §10.7.7). It is cleared by /clear like any other history.
func (c *chatModel) appendWelcome(text string) {
	c.entries = append(c.entries, chatEntry{kind: entryWelcome, text: text})
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
	c.entries = append(c.entries, chatEntry{
		kind:    entryTool,
		text:    name,
		done:    false,
		started: time.Now(),
	})
	c.refresh()
}

// appendToolDone marks the most recent unfinished tool entry as done. A display
// string with an `error:` prefix (how app.go formats failures) marks the entry
// as an error so it renders the ✗ glyph.
func (c *chatModel) appendToolDone(name, display string) {
	toolErr := strings.HasPrefix(display, "error:")
	for i := len(c.entries) - 1; i >= 0; i-- {
		if c.entries[i].kind == entryTool && !c.entries[i].done {
			c.entries[i].text = name
			c.entries[i].result = display
			c.entries[i].toolErr = toolErr
			c.entries[i].done = true
			if !c.entries[i].started.IsZero() {
				c.entries[i].dur = time.Since(c.entries[i].started)
				c.entries[i].hasDur = true
			}
			c.refresh()
			return
		}
	}
	c.entries = append(c.entries, chatEntry{
		kind:    entryTool,
		text:    name,
		result:  display,
		toolErr: toolErr,
		done:    true,
	})
	c.refresh()
}

// appendAgentDeltaBlock appends text as a standalone agent entry.
func (c *chatModel) appendAgentDeltaBlock(text string) {
	c.entries = append(c.entries, chatEntry{kind: entryAgent, text: text})
	c.refresh()
}

// upsertJobLog creates or updates the in-chat job-log block for a job
// (design §4.4). If an entryJobLog with the given id already exists it is
// updated in place (status, started, tail); otherwise a new block is appended.
func (c *chatModel) upsertJobLog(id, tool string, status domain.JobStatus, started *time.Time, tail []string) {
	for i := range c.entries {
		if c.entries[i].kind == entryJobLog && c.entries[i].jobID == id {
			c.entries[i].jobTool = tool
			c.entries[i].jobStatus = status
			c.entries[i].jobStarted = started
			c.entries[i].jobTail = tail
			c.refresh()
			return
		}
	}
	c.entries = append(c.entries, chatEntry{
		kind:       entryJobLog,
		jobID:      id,
		jobTool:    tool,
		jobStatus:  status,
		jobStarted: started,
		jobTail:    tail,
	})
	c.refresh()
}

// renderJobLogEntry renders an in-chat job-log block (design §4.4): a header
// line `<glyph> <tool> · <id> · <elapsed>` with a status-coloured glyph, then
// the dim tail lines indented under a ⎿ connector, like the tool traces.
func (c *chatModel) renderJobLogEntry(e chatEntry) string {
	var b strings.Builder

	glyphStyle := lipglossForeground(c.theme, c.theme.statusColor(e.jobStatus))
	header := glyphStyle.Render(glyph(e.jobStatus)) + " " +
		c.theme.AgentText.Render(e.jobTool) +
		c.theme.Muted.Render(" · "+e.jobID+" · "+jobLogElapsed(e.jobStarted))
	b.WriteString(header)

	for i, line := range e.jobTail {
		b.WriteString("\n")
		connector := "  "
		if i == 0 {
			connector = c.theme.Muted.Render("⎿ ")
		}
		b.WriteString(connector + c.theme.Muted.Render(line))
	}
	return b.String()
}

// jobLogElapsed renders the elapsed-since-start string for a job-log header,
// matching the format jobs.go's jobTimeInfo uses.
func jobLogElapsed(started *time.Time) string {
	if started == nil {
		return "queued"
	}
	d := time.Since(*started).Round(time.Second)
	if d < 0 {
		d = 0
	}
	return d.String()
}

// renderToolEntry renders a single tool-call trace (SPECS §10.7.5): a header
// line with a status glyph, then the result body indented under a ⎿ connector.
func (c *chatModel) renderToolEntry(e chatEntry) string {
	var b strings.Builder

	// Header: glyph + tool name + optional duration.
	var glyphRune string
	var glyphColor = c.theme.Palette.Running
	switch {
	case !e.done:
		glyphRune = "⏺"
		glyphColor = c.theme.statusColor(domain.JobRunning)
	case e.toolErr:
		glyphRune = "✗"
		glyphColor = c.theme.statusColor(domain.JobFailed)
	default:
		glyphRune = "⏺"
		glyphColor = c.theme.statusColor(domain.JobSucceeded)
	}
	glyphStyle := lipglossForeground(c.theme, glyphColor)

	header := glyphStyle.Render(glyphRune) + " " + c.theme.AgentText.Render(e.text)
	if e.hasDur {
		header += c.theme.Muted.Render(" (" + formatToolDur(e.dur) + ")")
	}
	b.WriteString(header)

	// Result body under the ⎿ connector.
	if e.result != "" {
		lines := strings.Split(e.result, "\n")
		truncated := 0
		if len(lines) > toolBodyMaxLines {
			truncated = len(lines) - toolBodyMaxLines
			lines = lines[:toolBodyMaxLines]
		}
		for i, line := range lines {
			b.WriteString("\n")
			connector := "  "
			if i == 0 {
				connector = c.theme.Muted.Render("⎿ ")
			}
			b.WriteString(connector + c.theme.Muted.Render(line))
		}
		if truncated > 0 {
			b.WriteString("\n  " + c.theme.Subtle.Render(fmt.Sprintf("… +%d lines", truncated)))
		}
	}
	return b.String()
}

// renderEntries returns the full conversation as a string (used by tests).
func (c *chatModel) renderEntries() string {
	var b strings.Builder
	for _, e := range c.entries {
		switch e.kind {
		case entryUser:
			b.WriteString(c.theme.UserText.Render("› " + e.text))
		case entryWelcome:
			b.WriteString(c.theme.Muted.Render(e.text))
		case entryAgent:
			md, err := c.renderer.Render(e.text)
			if err != nil {
				md = e.text
			}
			b.WriteString(c.theme.AgentText.Render(strings.TrimRight(md, "\n")))
		case entryTool:
			b.WriteString(c.renderToolEntry(e))
		case entryJobLog:
			b.WriteString(c.renderJobLogEntry(e))
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

// formatToolDur renders an elapsed duration compactly for a tool-trace header.
func formatToolDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// lipglossForeground returns a Muted-derived style recoloured to c, used for
// the per-status tool-trace glyph.
func lipglossForeground(th Theme, c lipgloss.AdaptiveColor) lipgloss.Style {
	return th.Muted.Foreground(c)
}
