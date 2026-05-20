package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/proteus/internal/agent"
	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/tools"
)

func newTestApp() *Model {
	return New(tools.NewRegistry(), llm.NewModelRegistry(), agent.SystemPrompt)
}

func TestAppHandlesWindowSize(t *testing.T) {
	m := newTestApp()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	got := updated.(*Model)
	if got.width != 120 || got.height != 40 {
		t.Fatalf("size not stored: %dx%d", got.width, got.height)
	}
}

func TestAppQuitKey(t *testing.T) {
	m := newTestApp()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatal("Ctrl+D should return a quit command")
	}
}

func TestAppToolBusMessagesRenderInChat(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(agent.ToolStartMsg{Name: "fold.esmfold"})
	updated, _ := m.Update(agent.ToolDoneMsg{Name: "fold.esmfold", Display: "folded (pLDDT 80)"})
	out := updated.(*Model).chat.renderEntries()
	if want := "fold.esmfold"; !contains(out, want) {
		t.Fatalf("chat missing tool trace %q in:\n%s", want, out)
	}
}

func TestAppCtrlCDuringConfirmOverlay(t *testing.T) {
	m := newTestApp()
	cancelled := false
	m.turnCancel = func() { cancelled = true }
	m.running = true
	m.overlay = overlayConfirm
	m.modal = modalModel{prompt: "Run X?"}

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if m.overlay != overlayNone {
		t.Error("ctrl+c should close the confirm overlay")
	}
	if !cancelled {
		t.Error("ctrl+c during confirm should cancel the turn")
	}
	select {
	case v := <-m.confirmCh:
		if v {
			t.Error("ctrl+c during confirm should send false (decline) to confirmCh")
		}
	default:
		t.Error("ctrl+c during confirm must unblock the agent goroutine via confirmCh")
	}
}

func TestAppCtrlCKeepsRunningUntilTurnEnds(t *testing.T) {
	m := newTestApp()
	m.running = true
	m.turnCancel = func() {}

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.running {
		t.Error("running must stay true after ctrl+c until the goroutine signals completion")
	}

	m.Update(agent.TurnErrorMsg{Err: context.Canceled})
	if m.running {
		t.Error("running must become false after TurnErrorMsg")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
