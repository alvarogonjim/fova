package tui

import "strings"

// --- confirmation modal ---

// modalModel is a yes/no confirmation overlay.
type modalModel struct {
	prompt string
}

// view renders the modal box.
func (m modalModel) view(th Theme, width int) string {
	body := m.prompt + "\n\n[ y ] confirm    [ n / esc ] cancel"
	return th.ModalBox.Width(min(width-4, 70)).Render(body)
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
