package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/agent"
	"github.com/alvarogonjim/fova/internal/assets"
	"github.com/alvarogonjim/fova/internal/config"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

func newTestApp() *Model {
	return New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(config.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
	})
}

func TestAppHeaderShowsWorkspacePath(t *testing.T) {
	// Bug 5 / rebrand spec §3.1: the header must display the active
	// project's workspace ($FOVA_HOME/projects/default), not the launch cwd.
	// Post-rebrand the title role moved from statusBarModel.headerView() to
	// Model.renderHeader() (which calls RenderHeader); the contract is the
	// same — the workspace path appears in the rendered header.
	home := t.TempDir()
	m := New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(config.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
		FovaHome:     home,
	})
	want := filepath.Join(home, "projects", "default")
	got := m.renderHeader()
	if !strings.Contains(got, want) {
		t.Fatalf("header = %q, want it to contain workspace %q", got, want)
	}
}

func TestAppPersistsSessionAndMessages(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(config.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
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

func TestAppEscCancelsRunningTurn(t *testing.T) {
	m := newTestApp()
	cancelled := false
	m.running = true
	m.turnCancel = func() { cancelled = true }
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Error("Esc during a running turn must cancel it")
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
		Models:       llm.NewModelRegistry(config.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
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
		Models:       llm.NewModelRegistry(config.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
		Store:        st,
	})
	m.runSlashCommand("plan", "")
	out := m.chat.renderEntries()
	if !contains(out, "p_view") || !contains(out, "design.bindcraft") {
		t.Fatalf("/plan should post the persisted plan block:\n%s", out)
	}
}

// TestAppPlanCommandPreservesNewlines guards spec Bug 6: the /plan view must
// render as a labelled multi-line block, not a single squashed paragraph.
func TestAppPlanCommandPreservesNewlines(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.InsertPlan(domain.DesignPlan{
		ID: "p_multi", ProjectID: store.DefaultProjectID,
		Application: domain.AppBinder, Method: "BindCraft",
		Target:        domain.PDBReference{PDBID: "1LYZ", Chain: "A"},
		ShortlistSize: 50, EstimatedCost: 15.0, EstimatedTime: "45 minutes",
	}); err != nil {
		t.Fatal(err)
	}

	m := New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(config.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
		Store:        st,
	})
	m.runSlashCommand("plan", "")
	out := m.chat.renderEntries()

	// Each label appears on its own line — check the rendered output keeps the
	// labels in distinct lines, the way the table format intends.
	idxTarget := strings.Index(out, "Target:")
	idxMethod := strings.Index(out, "Method:")
	idxShortlist := strings.Index(out, "Shortlist:")
	if idxTarget == -1 || idxMethod == -1 || idxShortlist == -1 {
		t.Fatalf("expected Target:/Method:/Shortlist: in rendered plan:\n%s", out)
	}
	if !strings.Contains(out[idxTarget:idxMethod], "\n") {
		t.Errorf("expected a newline between Target: and Method:\n%s", out)
	}
	if !strings.Contains(out[idxMethod:idxShortlist], "\n") {
		t.Errorf("expected a newline between Method: and Shortlist:\n%s", out)
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
		Models:       llm.NewModelRegistry(config.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
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
	if !contains(out, "later fova milestone") {
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
		if strings.Contains(last, "later fova milestone") ||
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

func TestAddTurnCostAccumulatesAndWarns(t *testing.T) {
	cat := config.Catalog{
		Providers: []config.Provider{{Name: "p", Kind: "anthropic"}},
		Models:    []config.Model{{ID: "m", Provider: "p", InputPricePer1M: 100, OutputPricePer1M: 100}},
	}
	m := &Model{
		chat:        newChatModel(NewTheme(), 80, 20),
		status:      newStatusBarModel(NewTheme()),
		models:      llm.NewModelRegistry(cat),
		budgetLimit: 5.0,
	}

	// 10k in + 10k out at $100 / 1M = $1.00 + $1.00 = $2.00 — under the limit.
	m.addTurnCost(llm.Usage{InputTokens: 10_000, OutputTokens: 10_000})
	if m.sessionCost < 1.99 || m.sessionCost > 2.01 {
		t.Fatalf("sessionCost = %v, want ~2.00", m.sessionCost)
	}
	if m.budgetWarned {
		t.Fatal("budget warned before the limit was crossed")
	}

	// A large turn pushes the session well past the $5 limit.
	m.addTurnCost(llm.Usage{InputTokens: 10_000_000, OutputTokens: 0})
	if !m.budgetWarned {
		t.Fatal("expected a budget warning after crossing the limit")
	}
	if m.status.cost != m.sessionCost {
		t.Errorf("status cost %v not synced with sessionCost %v", m.status.cost, m.sessionCost)
	}
}

func TestRunSlashCommandTheme(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	// Materialise the embedded default so /theme has a config to mutate.
	if _, err := config.LoadConfig(); err != nil {
		t.Fatalf("seed LoadConfig: %v", err)
	}

	m := newTestApp()
	m.configDir = dir

	m.runSlashCommand("theme", "dark")
	got, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after /theme: %v", err)
	}
	if got.UI.Theme != "dark" {
		t.Errorf("UI.Theme not persisted: %q", got.UI.Theme)
	}

	// A bad argument must not touch the file.
	m.runSlashCommand("theme", "neon")
	got2, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after bad /theme: %v", err)
	}
	if got2.UI.Theme != "dark" {
		t.Errorf("invalid /theme overwrote the file: %q", got2.UI.Theme)
	}
	out := m.chat.renderEntries()
	if !contains(out, "usage:") && !contains(out, "must be") {
		t.Errorf("/theme neon expected a usage/error line; got:\n%s", out)
	}
}

func TestRunSlashCommandThemePreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if _, err := config.LoadConfig(); err != nil {
		t.Fatalf("seed LoadConfig: %v", err)
	}
	pre, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	m := newTestApp()
	m.configDir = dir
	m.runSlashCommand("theme", "light")

	got, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after /theme: %v", err)
	}
	if got.UI.Theme != "light" {
		t.Errorf("UI.Theme: %q want light", got.UI.Theme)
	}
	// Every other section must round-trip unchanged.
	if got.Defaults != pre.Defaults || got.Knowledge != pre.Knowledge ||
		got.Webhook != pre.Webhook || got.Budget != pre.Budget ||
		got.UI.InlineGraphics != pre.UI.InlineGraphics {
		t.Errorf("/theme writeback dropped fields:\nbefore=%+v\nafter =%+v", pre, got)
	}
}

func TestRunSlashCommandReload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if _, err := config.LoadConfig(); err != nil {
		t.Fatalf("seed LoadConfig: %v", err)
	}

	m := newTestApp()
	m.configDir = dir
	m.runSlashCommand("reload", "")

	out := m.chat.renderEntries()
	if !contains(out, "reloaded") {
		t.Errorf("/reload should confirm; got:\n%s", out)
	}
}

func TestRunSlashCommandKeysStubbedForNow(t *testing.T) {
	// In Task 3 /keys just posts a placeholder; Task 4 wires the overlay.
	m := newTestApp()
	before := len(m.chat.entries)
	m.runSlashCommand("keys", "")
	if len(m.chat.entries) <= before && m.overlay == overlayNone {
		t.Errorf("/keys produced no chat output and opened no overlay")
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

func TestRenderStructureKittyAppendsEscape(t *testing.T) {
	dir := t.TempDir()
	pdb := filepath.Join(dir, "x.pdb")
	if err := os.WriteFile(pdb, []byte("HEADER fake pdb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	png := filepath.Join(dir, "x.png")
	if err := os.WriteFile(png, []byte("imaginary PNG bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Model{
		chat:     newChatModel(NewTheme(), 80, 20),
		graphics: Kitty,
		pymolRender: func(p string) (string, error) {
			if p != pdb {
				t.Errorf("pymolRender called with %q, want %q", p, pdb)
			}
			return png, nil
		},
	}
	m.RenderStructure(pdb)

	view := m.chat.renderEntries()
	if !strings.Contains(view, "\x1b_Ga=T,f=100;") {
		t.Error("chat does not contain the Kitty escape after RenderStructure")
	}
}

func TestRenderStructureNoRendererIsNoop(t *testing.T) {
	m := &Model{
		chat:        newChatModel(NewTheme(), 80, 20),
		graphics:    Kitty,
		pymolRender: nil, // SP-C has not wired the renderer yet
	}
	before := m.chat.renderEntries()
	m.RenderStructure("/whatever.pdb")
	if m.chat.renderEntries() != before {
		t.Error("RenderStructure with nil pymolRender must be a noop")
	}
}

func TestRenderStructureOffProtocolFallsBackToText(t *testing.T) {
	dir := t.TempDir()
	pdb := filepath.Join(dir, "x.pdb")
	if err := os.WriteFile(pdb, []byte("HEADER"), 0o644); err != nil {
		t.Fatal(err)
	}
	png := filepath.Join(dir, "x.png")
	if err := os.WriteFile(png, []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Model{
		chat:        newChatModel(NewTheme(), 80, 20),
		graphics:    Off,
		pymolRender: func(string) (string, error) { return png, nil },
	}
	m.RenderStructure(pdb)
	view := m.chat.renderEntries()
	if strings.Contains(view, "\x1b_G") || strings.Contains(view, "\x1b]1337") {
		t.Error("chat must not contain a graphics escape when graphics are off")
	}
	if !strings.Contains(view, png) {
		t.Errorf("chat fallback should mention the PNG path; view = %q", view)
	}
}

func TestRenderStructureRendererErrorAppendsError(t *testing.T) {
	m := &Model{
		chat:     newChatModel(NewTheme(), 80, 20),
		graphics: Kitty,
		pymolRender: func(string) (string, error) {
			return "", errors.New("pymol exploded")
		},
	}
	m.RenderStructure("/tmp/x.pdb")
	if !strings.Contains(m.chat.renderEntries(), "pymol exploded") {
		t.Errorf("chat does not surface the renderer error; view = %q", m.chat.renderEntries())
	}
}
