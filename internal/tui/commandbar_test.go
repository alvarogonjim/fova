package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestParseSlashCommand(t *testing.T) {
	cmd, arg, isSlash := parseSlashCommand("/model claude-opus-4-7")
	if !isSlash || cmd != "model" || arg != "claude-opus-4-7" {
		t.Fatalf("got cmd=%q arg=%q isSlash=%v", cmd, arg, isSlash)
	}
	if _, _, isSlash := parseSlashCommand("fold MAQ"); isSlash {
		t.Fatal("plain text misclassified as slash command")
	}
	cmd, arg, isSlash = parseSlashCommand("/help")
	if !isSlash || cmd != "help" || arg != "" {
		t.Fatalf("got cmd=%q arg=%q isSlash=%v", cmd, arg, isSlash)
	}
}

func TestCommandBarViewBorderedLabel(t *testing.T) {
	m := newCommandBarModel(NewTheme(), 60)
	out := m.View()
	for _, r := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(out, r) {
			t.Errorf("View() missing rounded-border rune %q", r)
		}
	}
	if !strings.Contains(out, "message") {
		t.Errorf("View() missing the %q label, got:\n%s", "message", out)
	}
}

func TestCommandBarViewStateDiffers(t *testing.T) {
	idle := newCommandBarModel(NewTheme(), 60)
	idle.setFocused(true)
	idle.setRunning(false)

	running := newCommandBarModel(NewTheme(), 60)
	running.setFocused(true)
	running.setRunning(true)

	if idle.View() == running.View() {
		t.Error("focused-idle and running View() output should differ (border colour)")
	}

	// The selected border style must reflect the state: rendering a probe
	// string through it must equal the corresponding Theme style's output.
	th := NewTheme()
	const probe = "x"
	if idle.inputBorderStyle().Render(probe) != th.InputBorderActive.Render(probe) {
		t.Error("focused-idle should use InputBorderActive")
	}
	if running.inputBorderStyle().Render(probe) != th.InputBorderBusy.Render(probe) {
		t.Error("running should use InputBorderBusy")
	}
	unfocused := newCommandBarModel(NewTheme(), 60)
	unfocused.setFocused(false)
	unfocused.setRunning(false)
	if unfocused.inputBorderStyle().Render(probe) != th.InputBorder.Render(probe) {
		t.Error("unfocused-idle should use InputBorder")
	}
}

func TestCommandBarSetWidthBounds(t *testing.T) {
	for _, w := range []int{4, 10, 40, 120} {
		m := newCommandBarModel(NewTheme(), 80)
		m.setWidth(w)
		out := m.View() // must not panic
		for _, line := range strings.Split(out, "\n") {
			if lipgloss.Width(line) > w && w >= 12 {
				t.Errorf("setWidth(%d): line width %d exceeds requested width:\n%s",
					w, lipgloss.Width(line), line)
			}
		}
	}
}

func TestPickerNavigation(t *testing.T) {
	p := newPicker("Model", []pickerItem{
		{id: "a", label: "A"}, {id: "b", label: "B"}, {id: "c", label: "C"},
	})
	p.next()
	p.next()
	if p.selected().id != "c" {
		t.Errorf("after two next(), selected = %q, want c", p.selected().id)
	}
	p.prev()
	if p.selected().id != "b" {
		t.Errorf("after prev(), selected = %q, want b", p.selected().id)
	}
	// Clamp at edges.
	p.prev()
	p.prev()
	if p.selected().id != "a" {
		t.Errorf("selection should clamp at top, got %q", p.selected().id)
	}
}
