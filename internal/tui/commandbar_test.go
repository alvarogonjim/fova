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

// TestCommandBarBorderStableAcrossStates locks in rebrand §3.6: the input
// border is moss-dim in every state. Idle, running, and modal-confirm must
// all resolve to the same Theme.InputBorder style.
func TestCommandBarBorderStableAcrossStates(t *testing.T) {
	th := NewTheme()
	const probe = "x"
	want := th.InputBorder.Render(probe)

	for _, tc := range []struct {
		name             string
		focused, running bool
		awaiting         bool
	}{
		{"idle", true, false, false},
		{"unfocused", false, false, false},
		{"running", true, true, false},
		{"awaiting-confirm", true, false, true},
	} {
		m := newCommandBarModel(NewTheme(), 60)
		m.setFocused(tc.focused)
		m.setRunning(tc.running)
		m.setAwaitingConfirm(tc.awaiting)
		if got := m.inputBorderStyle().Render(probe); got != want {
			t.Errorf("%s: border style differs from idle moss-dim — render mismatch", tc.name)
		}
	}
}

// TestCommandBarPromptColourFlipsOnAwaiting locks in rebrand §3.6: the `›`
// prompt is moss (Primary) while idle / running and saffron (Accent) only
// when a confirmation modal is open. We compare GetForeground on the
// textarea's FocusedStyle.Prompt because lipgloss strips colour in non-TTY
// test runs — Render-equality would be vacuous.
func TestCommandBarPromptColourFlipsOnAwaiting(t *testing.T) {
	th := NewTheme()
	moss := th.Palette.Primary // lipgloss.AdaptiveColor
	saffron := th.Palette.Accent

	idle := newCommandBarModel(NewTheme(), 60)
	if got := idle.ta.FocusedStyle.Prompt.GetForeground(); got != moss {
		t.Errorf("idle prompt foreground = %#v, want Primary moss %#v", got, moss)
	}

	running := newCommandBarModel(NewTheme(), 60)
	running.setRunning(true)
	if got := running.ta.FocusedStyle.Prompt.GetForeground(); got != moss {
		t.Errorf("running prompt foreground = %#v, want Primary moss %#v", got, moss)
	}

	awaiting := newCommandBarModel(NewTheme(), 60)
	awaiting.setAwaitingConfirm(true)
	if got := awaiting.ta.FocusedStyle.Prompt.GetForeground(); got != saffron {
		t.Errorf("awaiting-confirm prompt foreground = %#v, want Accent saffron %#v", got, saffron)
	}

	// The cursor block also flips to saffron in modal-confirm.
	if got := awaiting.ta.Cursor.Style.GetForeground(); got != saffron {
		t.Errorf("awaiting-confirm cursor foreground = %#v, want Accent saffron %#v", got, saffron)
	}
	// In idle state the cursor style carries no explicit foreground.
	if got := idle.ta.Cursor.Style.GetForeground(); got == saffron {
		t.Errorf("idle cursor must not be saffron-tinted, got %#v", got)
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

func TestCommandBarStartsAtOneLine(t *testing.T) {
	m := newCommandBarModel(NewTheme(), 60)
	if got := m.inputHeight(); got != inputMinHeight {
		t.Errorf("fresh commandBar inputHeight = %d, want %d", got, inputMinHeight)
	}
}

func TestCommandBarGrowsWithContent(t *testing.T) {
	m := newCommandBarModel(NewTheme(), 60)
	m.ta.SetValue("line1\nline2\nline3\nline4")
	if !m.refreshHeight() {
		t.Fatal("refreshHeight reported no change for a 4-line value")
	}
	if got := m.inputHeight(); got != 4 {
		t.Errorf("inputHeight after 4 lines = %d, want 4", got)
	}
}

func TestCommandBarClampsAtMax(t *testing.T) {
	m := newCommandBarModel(NewTheme(), 60)
	m.ta.SetValue(strings.Repeat("x\n", 20))
	m.refreshHeight()
	if got := m.inputHeight(); got != inputMaxHeight {
		t.Errorf("inputHeight on 20 lines = %d, want %d (clamped)", got, inputMaxHeight)
	}
}

func TestCommandBarResetCollapsesHeight(t *testing.T) {
	m := newCommandBarModel(NewTheme(), 60)
	m.ta.SetValue("line1\nline2\nline3")
	m.refreshHeight()
	if got := m.inputHeight(); got <= inputMinHeight {
		t.Fatalf("setup: inputHeight = %d, want > %d", got, inputMinHeight)
	}
	m.reset()
	if got := m.inputHeight(); got != inputMinHeight {
		t.Errorf("after reset, inputHeight = %d, want %d", got, inputMinHeight)
	}
}

func TestRefreshSlashMenuShowsPlanSubcommands(t *testing.T) {
	m := newTestApp()
	m.cmdbar.ta.SetValue("/plan ")
	m.refreshSlashMenu()
	if !m.showSlashMenu {
		t.Fatalf("/plan ' ' must show the slash menu")
	}
	if !containsRow(m.slashMenu.rows(), "/plan approve") {
		t.Errorf("/plan ' ' missing /plan approve: %+v", m.slashMenu.rows())
	}
	if !containsRow(m.slashMenu.rows(), "/plan cancel") {
		t.Errorf("/plan ' ' missing /plan cancel: %+v", m.slashMenu.rows())
	}
}

func TestRefreshSlashMenuTopLevelStillFilters(t *testing.T) {
	m := newTestApp()
	m.cmdbar.ta.SetValue("/mo")
	m.refreshSlashMenu()
	if !m.showSlashMenu {
		t.Fatalf("'/mo' must show the slash menu")
	}
	rows := m.slashMenu.rows()
	if len(rows) != 2 { // model + modal
		t.Errorf("'/mo' got %d rows, want 2 (model, modal): %+v", len(rows), rows)
	}
}

func TestCompleteSlashCommandWritesInsert(t *testing.T) {
	m := newTestApp()
	m.cmdbar.ta.SetValue("/plan ap")
	m.refreshSlashMenu()
	m.completeSlashCommand()
	if got := m.cmdbar.value(); got != "/plan approve" {
		t.Errorf("after Tab on /plan ap, input = %q, want %q", got, "/plan approve")
	}
	if m.showSlashMenu {
		t.Errorf("after unique completion, popup should close")
	}
}

func TestCompleteSlashCommandWritesLongestCommonPrefix(t *testing.T) {
	// Two top-level commands share the "mod" prefix (model, modal): common
	// prefix of Inserts "/model " and "/modal " is "/mod". Tab writes the
	// common prefix and keeps the popup open.
	m := newTestApp()
	m.cmdbar.ta.SetValue("/m")
	m.refreshSlashMenu()
	m.completeSlashCommand()
	if got := m.cmdbar.value(); got != "/mod" {
		t.Errorf("Tab on /m wrote %q, want /mod", got)
	}
	if !m.showSlashMenu {
		t.Errorf("popup must remain open while several rows match")
	}
}
