package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// --- confirmation modal ---

// modalModel is a yes/no confirmation overlay.
type modalModel struct {
	prompt string
}

// view renders the modal box. The box uses Theme.ModalBox, whose border is
// saffron-coloured per rebrand spec §3.7; the action row is rendered through
// RenderKeyRow so the `[y]` / `[n]` letters pick up the saffron Accent and
// the labels sit in sand Fg.
func (m modalModel) view(th Theme, width int) string {
	body := m.prompt + "\n\n" + RenderKeyRow(th,
		KeyRowEntry{Key: "y", Label: "confirm"},
		KeyRowEntry{Key: "n", Label: "cancel"},
	)
	return th.ModalBox.Width(min(width-4, 70)).Render(body)
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
