package tui

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// --- confirmation modal ---

// modalModel is a confirmation overlay. The default surface is the simple
// y/n prompt used historically; setting editable swaps in the four-key
// `[y] [e] [n] [esc]` row used by the editable tool-call review gate
// (spec §3.3 / §3.5). Today every modal opened from ConfirmRequestMsg is
// editable — the flag exists for forward flexibility.
type modalModel struct {
	prompt   string
	editable bool
}

// view renders the modal box. The box uses Theme.ModalBox, whose border is
// saffron-coloured per rebrand spec §3.7; the action row is rendered through
// RenderKeyRow so the bracketed keys pick up the saffron Accent and the
// labels sit in sand Fg.
//
// editable modals carry their full content (header, body, four-key row) in
// m.prompt via renderJSONModal — view only wraps that in the bordered box.
// Non-editable modals get the legacy `[y] confirm  [n] cancel` row appended.
func (m modalModel) view(th Theme, width int) string {
	if m.editable {
		// renderJSONModal already embedded the four-key row inside the
		// prompt; wrapping it in the box is all that's left.
		return th.ModalBox.Width(min(width-4, 70)).Render(m.prompt)
	}
	body := m.prompt + "\n\n" + RenderKeyRow(th,
		KeyRowEntry{Key: "y", Label: "confirm"},
		KeyRowEntry{Key: "n", Label: "cancel"},
	)
	return th.ModalBox.Width(min(width-4, 70)).Render(body)
}

// renderJSONModal renders a tool-call confirmation as pretty-printed JSON
// with the four-key editable action row. The body is capped at maxLines
// rows; when truncated the tail reads "… [e] to edit · scroll with PgUp/PgDn".
// edited is true when the user has saved an edited version — the header
// shows "(edited)" so they know what they're about to submit.
//
// The returned string is the raw prompt content (header + body + key row),
// not yet wrapped in a ModalBox; callers install it into
// modalModel{prompt: ..., editable: true} which adds the saffron border.
// Spec §3.5: every tool with RequiresConfirmation that does not opt into a
// bespoke surface (today only lab.submit_experiment) lands here.
func renderJSONModal(name string, input json.RawMessage, edited bool, th Theme, width, maxLines int) string {
	_ = width // reserved for future re-flow; the wrapping ModalBox owns width today.
	header := "Run " + name + "?"
	if edited {
		header += " (edited)"
	}

	var pretty bytes.Buffer
	if len(input) > 0 {
		if err := json.Indent(&pretty, input, "", "  "); err != nil {
			pretty.Reset()
			pretty.Write(input)
		}
	}
	lines := strings.Split(strings.TrimRight(pretty.String(), "\n"), "\n")

	const truncationTail = "… [e] to edit · scroll with PgUp/PgDn"
	body := lines
	if maxLines > 0 && len(lines) > maxLines {
		body = append([]string{}, lines[:maxLines]...)
		body = append(body, truncationTail)
	}

	keyRow := RenderKeyRow(th,
		KeyRowEntry{Key: "y", Label: "accept"},
		KeyRowEntry{Key: "e", Label: "edit"},
		KeyRowEntry{Key: "n", Label: "decline"},
		KeyRowEntry{Key: "esc", Label: "cancel turn"},
	)

	return strings.Join([]string{
		th.StatusBar.Render(header),
		"",
		strings.Join(body, "\n"),
		"",
		keyRow,
	}, "\n")
}

// KeyRowEntry is one `[letter] label` pair rendered in a modal key row.
type KeyRowEntry struct {
	Key   string // single-letter key
	Label string // human-readable action
}

// RenderKeyRow renders entries as `[y] yes  [n] no  ...` (rebrand spec §3.7).
// Bracketed keys take the Accent (saffron) colour; labels render in Fg (sand);
// the double-space separator uses FgMuted (dim) so the eye snaps to the keys.
// Used by the confirmation modal and the wet-lab submit overlay so the
// pattern stays consistent without copy-paste.
func RenderKeyRow(theme Theme, entries ...KeyRowEntry) string {
	keyStyle := lipgloss.NewStyle().Foreground(theme.Palette.Accent)
	labelStyle := lipgloss.NewStyle().Foreground(theme.Palette.Fg)
	sepStyle := lipgloss.NewStyle().Foreground(theme.Palette.FgMuted)

	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts,
			keyStyle.Render("["+e.Key+"]")+" "+labelStyle.Render(e.Label))
	}
	return strings.Join(parts, sepStyle.Render("  "))
}

// --- model / provider picker ---

// pickerItem is one selectable row.
type pickerItem struct {
	id    string
	label string
}

// pickerModel is a vertical single-select list overlay.
type pickerModel struct {
	title string
	items []pickerItem
	cur   int
}

func newPicker(title string, items []pickerItem) *pickerModel {
	return &pickerModel{title: title, items: items}
}

func (p *pickerModel) next() {
	if p.cur < len(p.items)-1 {
		p.cur++
	}
}

func (p *pickerModel) prev() {
	if p.cur > 0 {
		p.cur--
	}
}

func (p *pickerModel) selected() pickerItem {
	if len(p.items) == 0 {
		return pickerItem{}
	}
	return p.items[p.cur]
}

// view renders the picker box.
func (p *pickerModel) view(th Theme, width int) string {
	var b strings.Builder
	b.WriteString(th.StatusBar.Render(p.title))
	b.WriteString("\n\n")
	for i, it := range p.items {
		line := "  " + it.label
		if i == p.cur {
			line = th.PickerSel.Render("› " + it.label)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n↑/↓ select · enter confirm · esc cancel")
	return th.ModalBox.Width(min(width-4, 70)).Render(b.String())
}
