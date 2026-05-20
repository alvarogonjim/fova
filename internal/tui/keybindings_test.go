package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeybindingsTableNonEmpty(t *testing.T) {
	kb := keybindings()
	if len(kb) == 0 {
		t.Fatal("keybindings() returned an empty table")
	}
	// Every entry must populate all three fields — the overlay relies on it.
	for _, b := range kb {
		if b.Key == "" || b.Action == "" || b.Description == "" {
			t.Errorf("incomplete binding: %+v", b)
		}
	}
}

func TestKeybindingsTableCoversNewBindings(t *testing.T) {
	want := []string{"PgUp", "PgDown", "Home", "End", "Ctrl+L", "Ctrl+R", "?"}
	got := keybindings()
	for _, w := range want {
		found := false
		for _, b := range got {
			if b.Key == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("keybindings() missing %q", w)
		}
	}
}

func TestKeybindingsTableCoversSpecs104(t *testing.T) {
	// SPECS §10.4 lists Ctrl+C, Ctrl+D, Tab, Esc, Enter; the /keys overlay
	// is the single source of truth so each must appear.
	want := []string{"Ctrl+C", "Ctrl+D", "Tab", "Esc", "Enter"}
	got := keybindings()
	for _, w := range want {
		found := false
		for _, b := range got {
			if b.Key == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("keybindings() missing SPECS §10.4 entry %q", w)
		}
	}
}

func TestKeysOverlayRenders(t *testing.T) {
	th := NewTheme()
	out := newKeysOverlay().view(th, 80)
	if out == "" {
		t.Fatal("keysOverlay.view returned empty")
	}
	// Each binding's key must appear in the rendered overlay.
	for _, b := range keybindings() {
		if !strings.Contains(out, b.Key) {
			t.Errorf("overlay missing key %q in output:\n%s", b.Key, out)
		}
	}
}

func TestKeysOverlayOpensOnQuestionMarkWhenInputEmpty(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Input bar is empty → ? opens the overlay.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if m.overlay != overlayKeys {
		t.Fatalf("? on an empty input must open the keys overlay; overlay=%v", m.overlay)
	}

	// Esc closes it again.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != overlayNone {
		t.Errorf("Esc should close the keys overlay; overlay=%v", m.overlay)
	}
}

func TestKeysOverlayNotOpenedMidMessage(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.cmdbar.ta.SetValue("hello")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if m.overlay == overlayKeys {
		t.Error("? must not open the overlay while the user is mid-message")
	}
}

func TestPgUpScrollsChat(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	// Stuff the chat with enough entries to make scrolling meaningful.
	for i := 0; i < 200; i++ {
		m.chat.appendAgentDeltaBlock("line")
	}
	m.chat.viewport.GotoBottom()
	before := m.chat.viewport.YOffset
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.chat.viewport.YOffset >= before {
		t.Errorf("PgUp should scroll up: before=%d after=%d", before, m.chat.viewport.YOffset)
	}
}

func TestHomeEndScrollChat(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for i := 0; i < 200; i++ {
		m.chat.appendAgentDeltaBlock("line")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if m.chat.viewport.YOffset != 0 {
		t.Errorf("Home should set YOffset=0; got %d", m.chat.viewport.YOffset)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !m.chat.viewport.AtBottom() {
		t.Error("End should leave the viewport at the bottom")
	}
}

func TestCtrlLClearsChat(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.chat.appendAgentDeltaBlock("first")
	m.chat.appendAgentDeltaBlock("second")
	before := len(m.chat.entries)
	if before < 2 {
		t.Fatalf("setup: expected ≥2 entries, got %d", before)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if len(m.chat.entries) >= before {
		t.Errorf("Ctrl+L should clear chat history: before=%d after=%d",
			before, len(m.chat.entries))
	}
}

func TestCtrlRReloadsConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if _, err := configLoadForKeysTest(); err != nil {
		t.Fatalf("seed LoadConfig: %v", err)
	}
	m := newTestApp()
	m.configDir = dir
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	before := len(m.chat.entries)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if len(m.chat.entries) <= before {
		t.Error("Ctrl+R should emit a reload confirmation in the chat")
	}
}

// configLoadForKeysTest is a thin wrapper around config.LoadConfig that the
// keybindings_test file can call without re-importing config at the top (the
// rest of the test file is self-contained).
func configLoadForKeysTest() (any, error) {
	return loadConfigForTest()
}
