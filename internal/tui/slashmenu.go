package tui

import "strings"

// slashMenuModel is the slash-command autocomplete popup (SPECS §10.7.3). It is
// a filterable list of slash commands rendered above the message input: typing
// `/` followed by text narrows the catalogue to matching commands, and the
// cursor row is completed into the input on Tab/Enter. With a trailing space
// after the command word, the popup switches to per-command mode — keyword
// sub-commands (/plan approve) or dynamic argument rows (/install <tool>).
type slashMenuModel struct {
	entries []SlashRow
	cur     int
}

// newSlashMenu returns a slash menu pre-populated with the full top-level
// catalogue.
func newSlashMenu() *slashMenuModel {
	return &slashMenuModel{entries: matchTopLevel("", slashCommands), cur: 0}
}

// setRows replaces the entries (the caller resolves dynamic argument lists)
// and clamps the cursor into the new range.
func (m *slashMenuModel) setRows(rows []SlashRow) {
	m.entries = rows
	m.clamp()
}

// setFilter is the legacy top-level entry point: it refilters the catalogue by
// command-name prefix. Callers that need sub-command / argument mode build
// rows via MatchSlash and call setRows.
func (m *slashMenuModel) setFilter(prefix string) {
	m.entries = matchTopLevel(prefix, slashCommands)
	m.clamp()
}

// next moves the cursor down, clamping at the last entry (no wrap).
func (m *slashMenuModel) next() {
	if m.cur < len(m.entries)-1 {
		m.cur++
	}
}

// prev moves the cursor up, clamping at the first entry (no wrap).
func (m *slashMenuModel) prev() {
	if m.cur > 0 {
		m.cur--
	}
}

// selected returns the entry under the cursor; ok is false when the list is
// empty.
func (m *slashMenuModel) selected() (SlashRow, bool) {
	if len(m.entries) == 0 {
		return SlashRow{}, false
	}
	return m.entries[m.cur], true
}

// visible reports whether there is at least one entry to show.
func (m *slashMenuModel) visible() bool {
	return len(m.entries) > 0
}

// rows returns the current rows (used by the Tab handler that wants the full
// list to compute the longest common prefix).
func (m *slashMenuModel) rows() []SlashRow {
	return m.entries
}

// view renders one row per command, formatted "<label>  — <description>". The
// cursor row is styled with th.PickerSel and descriptions in th.Muted; each row
// is clipped to width display columns. It mirrors the pickerModel row pattern.
func (m *slashMenuModel) view(th Theme, width int) string {
	var b strings.Builder
	for i, r := range m.entries {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.row(th, r, i == m.cur, width))
	}
	return b.String()
}

// row renders a single menu row. The cursor row is styled wholesale with
// th.PickerSel; other rows keep the description dimmed with th.Muted. The plain
// text is clipped to width before any styling is applied.
func (m *slashMenuModel) row(th Theme, r SlashRow, cursor bool, width int) string {
	sep := "  — "
	if r.Description == "" {
		sep = ""
	}
	if cursor {
		text := slashClip("› "+r.Label+sep+r.Description, width)
		return th.PickerSel.Render(text)
	}
	head := "  " + r.Label + sep
	plain := head + r.Description
	clipped := slashClip(plain, width)
	if len(clipped) <= len(head) {
		return clipped
	}
	return clipped[:len(head)] + th.Muted.Render(clipped[len(head):])
}

// slashClip truncates s to at most w runes (w<=0 means no clipping). Rows carry
// no styling when clipped, so a rune count matches display columns.
func slashClip(s string, w int) string {
	if w <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w])
}

// clamp keeps the cursor within the bounds of the current entries.
func (m *slashMenuModel) clamp() {
	if m.cur > len(m.entries)-1 {
		m.cur = len(m.entries) - 1
	}
	if m.cur < 0 {
		m.cur = 0
	}
}
