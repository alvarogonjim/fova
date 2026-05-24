package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/agent"
	"github.com/alvarogonjim/fova/internal/assets"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

func newTestApp() *Model {
	return New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(assets.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
	})
}

func TestAppMouseWheelScrollsChat(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	for i := 0; i < 40; i++ {
		m.chat.appendAgentDeltaBlock(fmt.Sprintf("entry %d", i))
	}
	m.chat.viewport.GotoBottom()
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if m.chat.viewport.AtBottom() {
		t.Error("a MouseMsg wheel-up should scroll the chat")
	}
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
		Models:       llm.NewModelRegistry(assets.DefaultCatalog()),
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
		Models:       llm.NewModelRegistry(assets.DefaultCatalog()),
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
		if v.accepted {
			t.Error("ctrl+c during confirm should send accepted=false (decline) to confirmCh")
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
		Models:       llm.NewModelRegistry(assets.DefaultCatalog()),
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
		Models:       llm.NewModelRegistry(assets.DefaultCatalog()),
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
		Models:       llm.NewModelRegistry(assets.DefaultCatalog()),
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

// drainTurn waits for the agent-loop goroutine that startTurn launched (e.g.
// from /plan approve) to reach its terminal state. Without this, a test
// returns while that goroutine is still writing the synthetic user message
// into the persisted session — and t.TempDir cleanup's RemoveAll races those
// store-directory writes, an intermittent "directory not empty" failure. It
// reads m.bus until the turn-ending message; the m.running guard skips the
// wait when no turn was started, and the deadline catches a genuine hang.
func drainTurn(t *testing.T, m *Model) {
	t.Helper()
	if !m.running {
		return // /plan approve started no agent turn (no provider)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-m.bus:
			switch msg.(type) {
			case agent.TurnDoneMsg, agent.TurnErrorMsg:
				return
			}
		case <-deadline:
			t.Fatal("drainTurn: agent turn did not settle within 5s")
		}
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
		Models:       llm.NewModelRegistry(assets.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
		Store:        st,
	})
	m.runSlashCommand("plan", "approve")
	drainTurn(t, m) // /plan approve starts an agent turn — let it settle before cleanup
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

// --- Task 7: BoltzGen /plan rendering + /plan approve re-check ---

// fakeCheckTool is a tools.Tool stub standing in for design.boltzgen_check.
// Its Execute returns the canned JSON contract the /plan view and /plan
// approve re-check decode.
type fakeCheckTool struct {
	name   string
	output string
}

func (f *fakeCheckTool) Name() string                            { return f.name }
func (f *fakeCheckTool) Description() string                     { return "fake " + f.name }
func (f *fakeCheckTool) InputSchema() map[string]any             { return map[string]any{"type": "object"} }
func (*fakeCheckTool) RequiresConfirmation(json.RawMessage) bool { return false }
func (*fakeCheckTool) EstimatedCostUSD(json.RawMessage) float64  { return 0 }
func (*fakeCheckTool) EstimatedDuration(json.RawMessage) time.Duration {
	return 0
}
func (f *fakeCheckTool) Execute(_ context.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{Output: json.RawMessage(f.output)}, nil
}

// newBoltzGenTestApp builds a TUI Model with a store holding a BoltzGen plan
// (spec written into the workspace) and a registry carrying the given check
// tool. checkOutput is the canned design.boltzgen_check JSON.
func newBoltzGenTestApp(t *testing.T, checkOutput string) (*Model, *store.Store) {
	t.Helper()
	home := t.TempDir()
	workspace := filepath.Join(home, "projects", "default")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := "version: 1\nentities:\n  - protein:\n      id: A\n      sequence: 80..140\n"
	if err := os.WriteFile(filepath.Join(workspace, "spec.yaml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(workspace, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.InsertPlan(domain.DesignPlan{
		ID: "p_bg", ProjectID: store.DefaultProjectID,
		Application: domain.AppBinder, Method: "BoltzGen",
		Target: domain.PDBReference{PDBID: "6VXX", Chain: "A"},
		MethodConfig: &domain.MethodConfig{
			SpecPath: "spec.yaml",
			BoltzGen: &domain.BoltzGenParams{
				Protocol: "protein-anything", NumDesigns: 5000, Budget: 20,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	if checkOutput != "" {
		reg.Register(&fakeCheckTool{name: "design.boltzgen_check", output: checkOutput})
	}
	m := New(Deps{
		Registry:     reg,
		Models:       llm.NewModelRegistry(assets.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
		Store:        st,
		FovaHome:     home,
	})
	return m, st
}

// TestAppPlanShowsBoltzGenSection: /plan on a BoltzGen plan renders the
// method-config section, the spec absolute path, and the check result.
func TestAppPlanShowsBoltzGenSection(t *testing.T) {
	m, _ := newBoltzGenTestApp(t, `{"valid": true, "visualization_path": "out/viz.cif"}`)
	m.runSlashCommand("plan", "")
	out := m.chat.renderEntries()

	for _, want := range []string{
		"BoltzGen design specification",
		"protein-anything",
		"spec.yaml", // the spec path
		"entities:", // a preview line
		"valid",     // the check verdict
	} {
		if !contains(out, want) {
			t.Errorf("/plan BoltzGen view missing %q in:\n%s", want, out)
		}
	}
}

// TestAppPlanApproveBoltzGenValidSpec: /plan approve on a BoltzGen plan whose
// spec passes the re-check approves the plan and starts the design turn.
func TestAppPlanApproveBoltzGenValidSpec(t *testing.T) {
	m, st := newBoltzGenTestApp(t, `{"valid": true}`)
	m.runSlashCommand("plan", "approve")
	drainTurn(t, m) // /plan approve starts an agent turn — let it settle before cleanup
	out := m.chat.renderEntries()
	if !contains(out, "p_bg approved") {
		t.Fatalf("a valid-spec BoltzGen plan should be approved:\n%s", out)
	}
	got, err := st.GetPlan("p_bg")
	if err != nil || !got.Approved {
		t.Fatalf("plan not marked approved: approved=%v err=%v", got.Approved, err)
	}
}

// TestAppPlanApproveBoltzGenInvalidSpec: /plan approve on a BoltzGen plan
// whose spec fails the re-check holds the approval and surfaces the errors —
// catching edits the user made to the spec after plan.create.
func TestAppPlanApproveBoltzGenInvalidSpec(t *testing.T) {
	m, st := newBoltzGenTestApp(t, `{"valid": false, "errors": ["chain B missing"]}`)
	m.runSlashCommand("plan", "approve")
	out := m.chat.renderEntries()
	if !contains(out, "chain B missing") {
		t.Errorf("re-check failure should surface the errors:\n%s", out)
	}
	if contains(out, "p_bg approved") {
		t.Errorf("a failed re-check must not approve the plan:\n%s", out)
	}
	got, err := st.GetPlan("p_bg")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Approved {
		t.Error("a BoltzGen plan with an invalid spec must not be marked approved")
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
	// The generic modal must be the editable JSON renderer (spec §3.5):
	// header + four-key row, not the legacy y/n prompt.
	if !m.modal.editable {
		t.Errorf("generic confirm modal should be editable")
	}
	if !strings.Contains(m.modal.prompt, "Run design.bindcraft?") {
		t.Errorf("generic confirm modal missing rendered header:\n%s", m.modal.prompt)
	}
	if !strings.Contains(m.modal.prompt, "[e]") {
		t.Errorf("generic confirm modal missing [e] edit key:\n%s", m.modal.prompt)
	}
}

// --- Editable confirmation gate (spec §3.3 / §3.4 / §3.5) ---

// stubConfirmTool is a minimal tools.Tool that opts into confirmation but
// does not implement tools.Validator. It is used to exercise the editable
// gate's no-Validator fallback: any well-formed JSON edit is accepted as-is.
type stubConfirmTool struct{ name string }

func (s *stubConfirmTool) Name() string                            { return s.name }
func (s *stubConfirmTool) Description() string                     { return "stub " + s.name }
func (s *stubConfirmTool) InputSchema() map[string]any             { return map[string]any{"type": "object"} }
func (*stubConfirmTool) RequiresConfirmation(json.RawMessage) bool { return true }
func (*stubConfirmTool) EstimatedCostUSD(json.RawMessage) float64  { return 0 }
func (*stubConfirmTool) EstimatedDuration(json.RawMessage) time.Duration {
	return 0
}
func (*stubConfirmTool) Execute(_ context.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{}, nil
}

// stubValidatorTool extends stubConfirmTool with a programmable Validate that
// returns the next queued error on each call. Lets a single test drive the
// validate-fail → rewrite-with-ERROR → re-edit → validate-pass loop.
type stubValidatorTool struct {
	stubConfirmTool
	results []error // popped front-to-back per Validate call
}

func (s *stubValidatorTool) Validate(_ json.RawMessage) error {
	if len(s.results) == 0 {
		return nil
	}
	r := s.results[0]
	s.results = s.results[1:]
	return r
}

// newEditableConfirmApp builds a TUI Model with the given tool registered
// and the editable-gate hooks pointed at the test's temp dir + a fake editor.
// The fake editor takes a queue of byte sequences and writes the next one
// to the pending-input file each time the modal hands off via [e].
func newEditableConfirmApp(t *testing.T, tool tools.Tool, editorPayloads [][]byte) *Model {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(tool)
	m := New(Deps{
		Registry:     reg,
		Models:       llm.NewModelRegistry(assets.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
	})
	pendingDir := t.TempDir()
	m.pendingInputDir = func() string { return pendingDir }
	idx := 0
	m.openEditorFile = func(path, _ string) tea.Cmd {
		i := idx
		idx++
		return func() tea.Msg {
			if i < len(editorPayloads) {
				// Mimic what a user does: replace the entire file (header
				// included) with raw body bytes. readPendingInput strips
				// the (now absent) comments and returns the body intact.
				if err := os.WriteFile(path, editorPayloads[i], 0o644); err != nil {
					return editorFileDoneMsg{Path: path, Err: err}
				}
			}
			return editorFileDoneMsg{Path: path}
		}
	}
	return m
}

// runCmd drives a tea.Cmd until quiescence: it invokes the command, feeds
// the resulting message back through m.Update, and repeats so chained
// follow-up commands (the editor's done-msg → re-open editor on validate
// failure) all run inside the test goroutine.
func runCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return
		}
		_, cmd = m.Update(msg)
	}
}

// openConfirmModal pumps the two-message handshake the agent loop sends
// before blocking on confirmCh, so a test can immediately drive the modal's
// key handler.
func openConfirmModal(t *testing.T, m *Model, tool string, input []byte) {
	t.Helper()
	m.Update(agent.ConfirmContextMsg{Tool: tool, Input: input})
	m.Update(agent.ConfirmRequestMsg{Prompt: "Run " + tool + "?"})
	if m.overlay != overlayConfirm {
		t.Fatalf("confirm modal did not open for %s: overlay=%v", tool, m.overlay)
	}
}

// TestConfirmAcceptUnchanged: pressing [y] with no prior edit must accept
// the original proposal — the agent loop sees a nil edited slice and runs
// the bytes the model produced.
func TestConfirmAcceptUnchanged(t *testing.T) {
	tool := &stubConfirmTool{name: "fold.boltz2"}
	m := newEditableConfirmApp(t, tool, nil)
	openConfirmModal(t, m, "fold.boltz2", []byte(`{"sequence":"MAQ"}`))

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	select {
	case r := <-m.confirmCh:
		if !r.accepted {
			t.Error("[y] should accept the proposal")
		}
		if r.input != nil {
			t.Errorf("accept-unchanged must send a nil input; got %q", r.input)
		}
	case <-time.After(time.Second):
		t.Fatal("[y] did not write on confirmCh")
	}
	if m.overlay != overlayNone {
		t.Errorf("overlay should close after [y]; got %v", m.overlay)
	}
}

// TestConfirmEditAccept: tool without a Validator. [e] hands off to the
// fake editor which writes valid JSON; modal must re-render with `(edited)`
// and a second [y] must submit the edited bytes. The pending file is gone
// after accept.
func TestConfirmEditAccept(t *testing.T) {
	tool := &stubConfirmTool{name: "fold.boltz2"}
	edited := []byte(`{"sequence":"EDITED"}`)
	m := newEditableConfirmApp(t, tool, [][]byte{edited})
	openConfirmModal(t, m, "fold.boltz2", []byte(`{"sequence":"MAQ"}`))

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("[e] should produce an editor command")
	}
	runCmd(t, m, cmd)
	if m.pendingEdited == nil || !strings.Contains(string(m.pendingEdited), "EDITED") {
		t.Errorf("modal must store the edited bytes; got %q", m.pendingEdited)
	}
	if !strings.Contains(m.modal.prompt, "(edited)") {
		t.Errorf("modal must re-render with (edited) hint:\n%s", m.modal.prompt)
	}

	editedPath := m.pendingInputPath
	if editedPath == "" {
		t.Fatal("pendingInputPath must be set after the edit cycle")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	select {
	case r := <-m.confirmCh:
		if !r.accepted {
			t.Error("[y] after edit should accept")
		}
		if string(r.input) != string(edited) {
			t.Errorf("[y] after edit should submit edited bytes; got %q want %q", r.input, edited)
		}
	case <-time.After(time.Second):
		t.Fatal("[y] did not write on confirmCh after edit")
	}
	if _, err := os.Stat(editedPath); !os.IsNotExist(err) {
		t.Errorf("pending file should be removed after accept; err=%v", err)
	}
}

// TestConfirmEditValidateFailRetry: tool with a Validator that rejects the
// first edit and accepts the second. The first edit must rewrite the pending
// file with `// ERROR:` and reopen the editor; the second pass must succeed
// and let [y] submit the new bytes.
func TestConfirmEditValidateFailRetry(t *testing.T) {
	tool := &stubValidatorTool{
		stubConfirmTool: stubConfirmTool{name: "fold.chai1"},
		results:         []error{errors.New("missing target"), nil},
	}
	bad := []byte(`{"seq":"BAD"}`)
	good := []byte(`{"seq":"GOOD"}`)
	m := newEditableConfirmApp(t, tool, [][]byte{bad, good})
	openConfirmModal(t, m, "fold.chai1", []byte(`{"seq":"MAQ"}`))

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("[e] should produce an editor command")
	}
	runCmd(t, m, cmd)

	// After the second pass, pendingEdited must hold the good bytes and the
	// pending file must contain GOOD (and no longer the ERROR line).
	if m.pendingEdited == nil || !strings.Contains(string(m.pendingEdited), "GOOD") {
		t.Errorf("after retry, pendingEdited must hold the good bytes; got %q", m.pendingEdited)
	}
	if !strings.Contains(m.modal.prompt, "(edited)") {
		t.Errorf("after retry, modal must re-render with (edited) hint:\n%s", m.modal.prompt)
	}

	// Sanity: between the two passes the pending file received an ERROR
	// line. The file at this point reflects the second (success) seed, so
	// inspect the validator state instead — both queued errors must have
	// been consumed.
	if len(tool.results) != 0 {
		t.Errorf("validator should have been called twice; remaining=%v", tool.results)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	select {
	case r := <-m.confirmCh:
		if !r.accepted {
			t.Error("[y] after successful retry should accept")
		}
		if string(r.input) != string(good) {
			t.Errorf("[y] should submit the good bytes; got %q", r.input)
		}
	case <-time.After(time.Second):
		t.Fatal("[y] did not write on confirmCh after retry")
	}
}

// TestConfirmEditDecline: editing then declining must cleanup the pending
// file and never call the tool's Validate.
func TestConfirmEditDecline(t *testing.T) {
	tool := &stubValidatorTool{
		stubConfirmTool: stubConfirmTool{name: "fold.boltz2"},
	}
	m := newEditableConfirmApp(t, tool, [][]byte{[]byte(`{"x":1}`)})
	openConfirmModal(t, m, "fold.boltz2", []byte(`{"x":0}`))

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	runCmd(t, m, cmd)
	editedPath := m.pendingInputPath
	if editedPath == "" {
		t.Fatal("pendingInputPath must be set after [e]")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	select {
	case r := <-m.confirmCh:
		if r.accepted {
			t.Error("[n] after edit must decline")
		}
	case <-time.After(time.Second):
		t.Fatal("[n] did not write on confirmCh")
	}
	if _, err := os.Stat(editedPath); !os.IsNotExist(err) {
		t.Errorf("pending file should be removed after decline; err=%v", err)
	}
	if m.pendingInputPath != "" || m.pendingEdited != nil || m.pendingValidator != nil {
		t.Errorf("decline must reset every pending field; got path=%q edited=%q validator=%v",
			m.pendingInputPath, m.pendingEdited, m.pendingValidator != nil)
	}
}

// TestConfirmBespokeDispatchUnchanged: lab.submit_experiment continues to
// open the bespoke submit overlay, not the generic JSON renderer (spec §3.5).
func TestConfirmBespokeDispatchUnchanged(t *testing.T) {
	m := newTestApp()
	input := `{"target_id":"t1","assay_type":"binding","sequences":[{"name":"d","sequence":"MAQ"}]}`
	m.Update(agent.ConfirmContextMsg{Tool: "lab.submit_experiment", Input: []byte(input)})
	m.Update(agent.ConfirmRequestMsg{Prompt: "Run lab.submit_experiment?"})
	if m.overlay != overlaySubmit {
		t.Fatalf("lab.submit_experiment must open overlaySubmit; got %v", m.overlay)
	}
	// And the editable JSON renderer must not have populated m.modal.
	if m.modal.editable {
		t.Error("lab.submit_experiment must not install the generic editable modal")
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

func TestTabCyclesPanelFocus(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	want := []panelFocus{focusJobs, focusDesigns, focusLab, focusChat}
	for i, w := range want {
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
		if m.focus != w {
			t.Errorf("Tab #%d: focus = %v, want %v", i+1, m.focus, w)
		}
	}
}

func TestFocusedPanelArrowsMoveSelection(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.jobs.setJobs([]domain.Job{{ID: "j1"}, {ID: "j2"}, {ID: "j3"}})
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus jobs
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.jobs.selected != 1 {
		t.Errorf("after Down, jobs.selected = %d, want 1", m.jobs.selected)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.jobs.selected != 0 {
		t.Errorf("after Up, jobs.selected = %d, want 0", m.jobs.selected)
	}
}

func TestFocusedPanelDimsInput(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus jobs
	if m.cmdbar.active {
		t.Error("the input should be inactive while a panel is focused")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // back to chat
	if !m.cmdbar.active {
		t.Error("Esc should return focus to the chat and reactivate the input")
	}
}

func TestAddTurnCostAccumulatesAndWarns(t *testing.T) {
	cat := assets.Catalog{
		Providers: []assets.Provider{{Name: "p", Kind: "anthropic"}},
		Models:    []assets.Model{{ID: "m", Provider: "p", InputPricePer1M: 100, OutputPricePer1M: 100}},
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
	if _, err := assets.LoadConfig(); err != nil {
		t.Fatalf("seed LoadConfig: %v", err)
	}

	m := newTestApp()
	m.configDir = dir

	m.runSlashCommand("theme", "dark")
	got, err := assets.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after /theme: %v", err)
	}
	if got.UI.Theme != "dark" {
		t.Errorf("UI.Theme not persisted: %q", got.UI.Theme)
	}

	// A bad argument must not touch the file.
	m.runSlashCommand("theme", "neon")
	got2, err := assets.LoadConfig()
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
	if _, err := assets.LoadConfig(); err != nil {
		t.Fatalf("seed LoadConfig: %v", err)
	}
	pre, err := assets.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	m := newTestApp()
	m.configDir = dir
	m.runSlashCommand("theme", "light")

	got, err := assets.LoadConfig()
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
	if _, err := assets.LoadConfig(); err != nil {
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

func TestOnboardingCommandOpensWizard(t *testing.T) {
	t.Setenv("FOVA_CONFIG_DIR", t.TempDir()) // isolate config I/O
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.runSlashCommand("onboarding", "")
	if m.overlay != overlayWizard {
		t.Fatalf("/onboarding should open the wizard overlay, got %v", m.overlay)
	}
	if m.wizard == nil {
		t.Error("/onboarding should construct the wizard model")
	}
}

func TestWizardOverlaySkipCloses(t *testing.T) {
	t.Setenv("FOVA_CONFIG_DIR", t.TempDir()) // isolate config I/O
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.runSlashCommand("onboarding", "")
	// Esc inside the overlay produces a command that yields a wizardDoneMsg;
	// run it and feed the message back so finishWizardOverlay runs.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc in the wizard overlay should produce a command")
	}
	m.Update(cmd())
	if m.overlay != overlayNone {
		t.Error("skipping the wizard overlay should close it")
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

func TestEnterOpensDetailOverlay(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.jobs.setJobs([]domain.Job{
		{ID: "j1", Tool: "design.bindcraft", Status: domain.JobRunning, Created: time.Now()},
	})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})   // focus jobs
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open detail
	if m.overlay != overlayDetail {
		t.Fatalf("Enter on a focused panel should open the detail overlay, got %v", m.overlay)
	}
	if !strings.Contains(m.View(), "design.bindcraft") {
		t.Error("the detail overlay should show the selected job")
	}
}

func TestDetailOverlayEscClosesKeepsFocus(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.jobs.setJobs([]domain.Job{{ID: "j1", Tool: "t", Status: domain.JobRunning, Created: time.Now()}})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != overlayNone {
		t.Error("Esc should close the detail overlay")
	}
	if m.focus != focusJobs {
		t.Error("Esc should keep the originating panel focus")
	}
}

func TestEnterOnEmptyPanelIsNoop(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus jobs (empty)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayNone {
		t.Error("Enter on an empty panel must not open an overlay")
	}
}

func TestClearKeepsPanelsVisible(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.focus = focusJobs
	m.runSlashCommand("clear", "")
	wantChatW := 120 - 38 - 2 // full width minus the panel column and gap
	if m.chat.width != wantChatW {
		t.Errorf("after /clear chat width = %d, want %d (panels pushed off-screen)", m.chat.width, wantChatW)
	}
	if m.focus != focusChat {
		t.Error("/clear should return focus to the chat")
	}
}

func TestDetailOverlayRefreshesLive(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.jobs.setJobs([]domain.Job{
		{ID: "j1", Tool: "design.bindcraft", Status: domain.JobRunning, Created: time.Now()},
	})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})   // focus jobs
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open detail
	// The job finishes; the panels reload with its new status.
	m.jobs.setJobs([]domain.Job{
		{ID: "j1", Tool: "design.bindcraft", Status: domain.JobSucceeded, Created: time.Now()},
	})
	m.refreshDetail()
	if !strings.Contains(m.View(), "succeeded") {
		t.Error("an open job detail should refresh to the job's new status")
	}
}

func TestDetailOverlayClosesWhenItemGone(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.jobs.setJobs([]domain.Job{{ID: "j1", Tool: "t", Status: domain.JobRunning, Created: time.Now()}})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.jobs.setJobs(nil) // the job disappears from the panel
	m.refreshDetail()
	if m.overlay != overlayNone {
		t.Error("the detail overlay should close when its item disappears")
	}
}

func TestEnterOpensDesignDetail(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.designs.setDesigns([]domain.Design{
		{ID: "d-xyz", Origin: domain.OriginBindCraft, Created: time.Now()},
	})
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus jobs
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus designs
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayDetail {
		t.Fatalf("Enter on the designs panel should open the detail overlay, got %v", m.overlay)
	}
	if !strings.Contains(m.View(), "d-xyz") {
		t.Error("the detail overlay should show the selected design")
	}
}

func TestEnterOpensLabDetail(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.lab.setExperiments([]domain.Experiment{
		{ID: "e1", TargetName: "PD-L1", AssayType: "binding", Status: "in_progress"},
	})
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus jobs
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus designs
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus lab
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayDetail {
		t.Fatalf("Enter on the lab panel should open the detail overlay, got %v", m.overlay)
	}
	if !strings.Contains(m.View(), "PD-L1") {
		t.Error("the detail overlay should show the selected experiment")
	}
}

func TestDetailOverlayTabAdvancesFocus(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.jobs.setJobs([]domain.Job{{ID: "j1", Tool: "t", Status: domain.JobRunning, Created: time.Now()}})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})   // focus jobs
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open detail
	m.Update(tea.KeyMsg{Type: tea.KeyTab})   // close detail + advance focus
	if m.overlay != overlayNone {
		t.Error("Tab in the detail overlay should close it")
	}
	if m.focus != focusDesigns {
		t.Errorf("Tab in the detail overlay should advance focus to designs, got %v", m.focus)
	}
}

// TestModelTextDeltaSchedulesStreamFlush verifies the first TextDeltaMsg of a
// streaming burst kicks off the 30-FPS tick chain (perf-batch-2 §6) and that
// subsequent deltas don't double-schedule.
func TestModelTextDeltaSchedulesStreamFlush(t *testing.T) {
	m := newTestApp()
	m.running = true

	_, cmd := m.Update(agent.TextDeltaMsg{Delta: "hi"})
	if cmd == nil {
		t.Fatal("first TextDeltaMsg did not return a cmd (expected stream-flush schedule)")
	}
	if !m.streamFlushScheduled {
		t.Error("streamFlushScheduled flag not set")
	}

	// A second delta does NOT schedule again.
	_, cmd2 := m.Update(agent.TextDeltaMsg{Delta: "there"})
	_ = cmd2 // cmd2 is waitForBus only; we just check the scheduled flag didn't toggle.
	if !m.streamFlushScheduled {
		t.Error("streamFlushScheduled flag flipped off after second delta")
	}
}

// TestModelStreamFlushChainsWhileRunning verifies a streamFlushMsg drains the
// chat buffer and chains the next tick while a turn is still running.
func TestModelStreamFlushChainsWhileRunning(t *testing.T) {
	m := newTestApp()
	m.running = true
	m.streamFlushScheduled = true
	m.chat.appendAgentDelta("buffered")

	_, cmd := m.Update(streamFlushMsg{})
	if cmd == nil {
		t.Error("streamFlushMsg returned nil cmd while running")
	}
	if len(m.chat.entries) == 0 || m.chat.entries[len(m.chat.entries)-1].text != "buffered" {
		t.Errorf("flushPendingDelta did not drain the buffer")
	}
}

// TestModelStreamFlushStopsAtTurnEnd verifies the tick chain halts itself once
// the turn is no longer running and the scheduled flag is cleared.
func TestModelStreamFlushStopsAtTurnEnd(t *testing.T) {
	m := newTestApp()
	m.running = false
	m.streamFlushScheduled = true

	_, cmd := m.Update(streamFlushMsg{})
	if cmd != nil {
		t.Errorf("streamFlushMsg returned a cmd after turn ended; want nil")
	}
	if m.streamFlushScheduled {
		t.Error("streamFlushScheduled flag not cleared at turn end")
	}
}

// TestModelTurnDoneFlushesPendingDelta verifies TurnDoneMsg forces a final
// flush so any tokens that arrived between the last tick and end-of-turn are
// rendered before the turn closes (perf-batch-2 §6).
func TestModelTurnDoneFlushesPendingDelta(t *testing.T) {
	m := newTestApp()
	m.running = true
	m.streamFlushScheduled = true
	m.chat.appendAgentDelta("tail tokens")

	_, _ = m.Update(agent.TurnDoneMsg{})

	if m.streamFlushScheduled {
		t.Error("streamFlushScheduled not cleared on TurnDoneMsg")
	}
	if got := m.chat.entries[len(m.chat.entries)-1].text; got != "tail tokens" {
		t.Errorf("entry text after TurnDone = %q, want %q", got, "tail tokens")
	}
}
