package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/proteus/internal/agent"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
)

func newTestApp() *Model {
	return New(tools.NewRegistry(), llm.NewModelRegistry(), agent.SystemPrompt, nil)
}

func TestAppPersistsSessionAndMessages(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := New(tools.NewRegistry(), llm.NewModelRegistry(), agent.SystemPrompt, st)
	if m.sessionID == "" {
		t.Fatal("New with a store must create a session row")
	}
	if _, err := st.GetSession(m.sessionID); err != nil {
		t.Fatalf("session row not persisted: %v", err)
	}
	// A message added to the session must reach the store.
	m.session.AddUserMessage("fold MAQ")
	msgs, err := st.ListMessages(m.sessionID)
	if err != nil || len(msgs) != 1 || msgs[0].Content != "fold MAQ" {
		t.Fatalf("message not persisted: %+v err=%v", msgs, err)
	}
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

func TestAppRefreshLoadsPanelsFromStore(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.InsertDesign(domain.Design{
		ID: "d_panel", ProjectID: store.DefaultProjectID,
		Scores: map[string]float64{"ipsae": 0.66},
	}); err != nil {
		t.Fatal(err)
	}

	m := New(tools.NewRegistry(), llm.NewModelRegistry(), agent.SystemPrompt, st)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	// A refresh tick reloads the panels from the store.
	m.Update(refreshMsg{})
	if len(m.designs.designs) != 1 {
		t.Fatalf("designs panel not refreshed from store: %d", len(m.designs.designs))
	}
}

func TestAppTabCyclesFocus(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30}) // narrow → Tab-cycled
	start := m.focus
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus == start {
		t.Error("Tab should advance the panel focus")
	}
}

func TestAppWideLayoutShowsPanels(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := m.View()
	if !strings.Contains(view, "JOBS") || !strings.Contains(view, "DESIGNS") {
		t.Errorf("wide layout must show the JOBS and DESIGNS panels:\n%s", view)
	}
}

func TestAppPlanCommandNoStore(t *testing.T) {
	m := newTestApp() // store is nil
	m.runSlashCommand("plan", "")
	out := m.chat.renderEntries()
	if !contains(out, "No design plan yet") {
		t.Fatalf("/plan without a plan should post the no-plan block:\n%s", out)
	}
}

func TestAppPlanCommandShowsPersistedPlan(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.InsertPlan(domain.DesignPlan{
		ID: "p_view", ProjectID: store.DefaultProjectID,
		Application: domain.AppBinder, Method: "design.bindcraft",
		Target: domain.PDBReference{PDBID: "6VXX", Chain: "A"},
	}); err != nil {
		t.Fatal(err)
	}

	m := New(tools.NewRegistry(), llm.NewModelRegistry(), agent.SystemPrompt, st)
	m.runSlashCommand("plan", "")
	out := m.chat.renderEntries()
	if !contains(out, "p_view") || !contains(out, "design.bindcraft") {
		t.Fatalf("/plan should post the persisted plan block:\n%s", out)
	}
}

func TestAppPlanApprove(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.InsertPlan(domain.DesignPlan{
		ID: "p_appr", ProjectID: store.DefaultProjectID,
		Application: domain.AppBinder, Method: "design.bindcraft",
	}); err != nil {
		t.Fatal(err)
	}

	m := New(tools.NewRegistry(), llm.NewModelRegistry(), agent.SystemPrompt, st)
	m.runSlashCommand("plan", "approve")
	out := m.chat.renderEntries()
	if !contains(out, "p_appr approved") {
		t.Fatalf("/plan approve should confirm approval:\n%s", out)
	}
	got, err := st.GetPlan("p_appr")
	if err != nil || !got.Approved {
		t.Fatalf("plan not marked approved in store: approved=%v err=%v", got.Approved, err)
	}
}

func TestAppPlanCancel(t *testing.T) {
	m := newTestApp()
	m.runSlashCommand("plan", "cancel")
	out := m.chat.renderEntries()
	if !contains(out, "plan cancelled") {
		t.Fatalf("/plan cancel should post a cancellation message:\n%s", out)
	}
}

func TestAppPlanUnknownArg(t *testing.T) {
	m := newTestApp()
	m.runSlashCommand("plan", "bogus")
	out := m.chat.renderEntries()
	if !contains(out, "unknown /plan argument") {
		t.Fatalf("unknown /plan argument should post an error:\n%s", out)
	}
}

func TestAppOtherSlashStubsRemain(t *testing.T) {
	m := newTestApp()
	m.runSlashCommand("jobs", "")
	out := m.chat.renderEntries()
	if !contains(out, "later Proteus milestone") {
		t.Fatalf("/jobs should still be a stub:\n%s", out)
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
