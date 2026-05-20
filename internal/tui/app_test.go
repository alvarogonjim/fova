package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/proteus/internal/agent"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
)

func newTestApp() *Model {
	return New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(),
		SystemPrompt: agent.SystemPrompt,
	})
}

func TestAppPersistsSessionAndMessages(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(),
		SystemPrompt: agent.SystemPrompt,
		Store:        st,
	})
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

	m := New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(),
		SystemPrompt: agent.SystemPrompt,
		Store:        st,
	})
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
	if !strings.Contains(view, "jobs") || !strings.Contains(view, "designs") {
		t.Errorf("wide layout must show the jobs and designs panels:\n%s", view)
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

	m := New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(),
		SystemPrompt: agent.SystemPrompt,
		Store:        st,
	})
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

	m := New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(),
		SystemPrompt: agent.SystemPrompt,
		Store:        st,
	})
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

func TestRunSlashCommandRoutesSetupCommands(t *testing.T) {
	// Each setup command must reach its handler — not the "later milestone"
	// stub or the unknown-command default.
	for _, cmd := range []string{"doctor", "tools", "install", "uninstall", "modal"} {
		m := newSetupTestModel(t)
		before := len(m.chat.entries)
		m.runSlashCommand(cmd, "")
		if len(m.chat.entries) <= before {
			t.Errorf("/%s produced no chat output — not routed?", cmd)
			continue
		}
		last := m.chat.entries[len(m.chat.entries)-1].text
		if strings.Contains(last, "later Proteus milestone") ||
			strings.Contains(last, "unknown command") {
			t.Errorf("/%s hit the stub/unknown path: %q", cmd, last)
		}
	}
}

func TestAppSubmitConfirmShowsRichModal(t *testing.T) {
	m := newTestApp()
	input := `{"target_id":"comp-her2","assay_type":"binding","sequences":[{"name":"d1","sequence":"MAQVQL"}]}`
	m.Update(agent.ConfirmContextMsg{Tool: "lab.submit_experiment", Input: []byte(input)})
	m.Update(agent.ConfirmRequestMsg{Prompt: "Run lab.submit_experiment?"})
	if m.overlay != overlaySubmit {
		t.Fatalf("lab.submit_experiment should open the rich submit overlay, got %v", m.overlay)
	}
	if m.submit.AssayType != "binding" || len(m.submit.Sequences) != 1 {
		t.Fatalf("submit modal not populated from the tool input: %+v", m.submit)
	}
}

func TestAppGenericConfirmForOtherTools(t *testing.T) {
	m := newTestApp()
	m.Update(agent.ConfirmContextMsg{Tool: "design.bindcraft", Input: []byte(`{}`)})
	m.Update(agent.ConfirmRequestMsg{Prompt: "Run design.bindcraft?"})
	if m.overlay != overlayConfirm {
		t.Fatalf("a non-lab tool should use the generic confirm overlay, got %v", m.overlay)
	}
}

func TestAppRefreshShowsJobLogBlock(t *testing.T) {
	m := newTestApp()
	logf := filepath.Join(t.TempDir(), "j.log")
	if err := os.WriteFile(logf, []byte("step one\nstep two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	m.jobs.setJobs([]domain.Job{{
		ID: "j_demo", Tool: "install bindcraft", Status: domain.JobRunning,
		Created: time.Now().UTC(), Started: &started, LogFile: logf,
	}})
	m.Update(refreshMsg{})
	out := m.chat.renderEntries()
	if !contains(out, "install bindcraft") || !contains(out, "step two") {
		t.Fatalf("expected an in-chat job-log block with the tool name and a log line:\n%s", out)
	}
}

func TestAppTabFocusesRunningJob(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	started := time.Now().UTC()
	m.jobs.setJobs([]domain.Job{{
		ID: "j_run", Tool: "design.bindcraft", Status: domain.JobRunning,
		Created: time.Now().UTC(), Started: &started, LogFile: "",
	}})
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // chat → the running job
	if m.overlay != overlayJobLog || m.jobLogID != "j_run" {
		t.Fatalf("Tab should focus the running job's log overlay; overlay=%v jobLogID=%q", m.overlay, m.jobLogID)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // overlay → back to chat
	if m.overlay != overlayNone {
		t.Fatalf("Esc should close the job-log overlay, got %v", m.overlay)
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
