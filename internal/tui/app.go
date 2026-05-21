package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"github.com/alvarogonjim/fova/internal/agent"
	"github.com/alvarogonjim/fova/internal/assets"
	"github.com/alvarogonjim/fova/internal/backends/local"
	"github.com/alvarogonjim/fova/internal/config"
	"github.com/alvarogonjim/fova/internal/domain"
	jobmgr "github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/replay"
	"github.com/alvarogonjim/fova/internal/safety"
	"github.com/alvarogonjim/fova/internal/skills"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
	"github.com/alvarogonjim/fova/internal/tools/lab"
	"github.com/alvarogonjim/fova/internal/version"
)

// overlay identifies the active modal overlay, if any.
type overlay int

const (
	overlayNone overlay = iota
	overlayConfirm
	overlaySubmit
	overlayPicker
	overlayJobLog
	overlayKeys
)

// panelFocus is which pane Tab-cycling currently targets (used for the
// narrow-terminal single-pane layout).
type panelFocus int

const (
	focusChat panelFocus = iota
	focusJobs
	focusDesigns
	focusLab
)

// refreshMsg triggers a reload of the jobs and designs panels from the store.
type refreshMsg struct{}

// Model is the root Bubble Tea model.
type Model struct {
	width, height int

	theme Theme
	chat  *chatModel
	// graphics is the inline-graphics protocol resolved at startup from
	// Detect() and the [ui].inline_graphics override. Off → no inline rendering.
	graphics Protocol
	// pymolRender is the structure-to-PNG renderer SP-C plugs in. Nil while
	// SP-C has not wired its viz.pymol_render tool yet; RenderStructure is a
	// noop in that case.
	pymolRender func(pdbPath string) (pngPath string, err error)
	status      statusBarModel
	cmdbar      commandBarModel

	jobs    jobsModel
	designs designsModel
	lab     labModel
	focus   panelFocus

	jobLog       jobLogView   // full-screen log view for the Tab-focused job
	jobLogID     string       // ID of the job shown in jobLog ("" = none)
	sessionStart time.Time    // jobs created before this aren't blocked into chat
	sessionCost  float64      // running LLM cost for this TUI session, in USD
	budgetLimit  float64      // [budget].session_soft_limit_usd; 0 = no limit
	budgetWarned bool         // true once the soft-limit warning has been shown
	webhookURL   string       // Adaptyv callback URL shown in the submit modal
	guard        safety.Guard // content-filter guard consulted on every tool call

	registry     *tools.Registry
	models       *llm.ModelRegistry
	systemPrompt string
	skillLoader  *skills.Loader // backs the skills.list/read tools and /skills
	assetReport  assets.Report  // validation Report from the last assets.Load()
	// pendingAssetPath is the file an in-flight /skills or /config edit is
	// editing; pendingAssetReload requests a bundle reload when it closes.
	pendingAssetPath   string
	pendingAssetReload bool
	session            *agent.Session   // one session for the whole TUI lifetime
	store              *store.Store     // nil → persistence disabled
	sessionID          domain.SessionID // current persisted session

	jobMgr    *jobmgr.Manager // async job manager (install / deploy / design jobs)
	localReg  *local.Registry // installable-tool registry
	fovaHome  string          // $FOVA_HOME, used for setup log-file paths
	configDir string          // <ConfigDir> — where /theme writes config.toml

	// installFn runs a tool install, writing progress to log. Defaults to the
	// real local installer; tests override it.
	installFn func(ctx context.Context, name string, log io.Writer) error

	bus       chan tea.Msg // agent → TUI
	confirmCh chan bool    // TUI → agent (modal result)

	turnCancel context.CancelFunc
	running    bool

	overlay overlay
	modal   modalModel
	submit  submitModal // rich Adaptyv submit-confirmation overlay (SPECS §12.2)
	picker  *pickerModel
	keys    keysOverlay // /keys overlay state (just a placeholder marker)

	// pendingTool / pendingInput hold the tool context from a ConfirmContextMsg
	// until the paired ConfirmRequestMsg arrives.
	pendingTool  string
	pendingInput json.RawMessage

	thinking      thinkingModel   // animated "thinking" indicator (SPECS §10.7.4)
	slashMenu     *slashMenuModel // slash-command autocomplete popup (§10.7.3)
	showSlashMenu bool            // whether the popup is currently shown
	turnStart     time.Time       // start of the current turn, for the elapsed counter

	// Replay-mode state. When replayEvents != nil, the agent loop, store
	// writes, and webhook receiver are all skipped; the pump goroutine
	// posts replayTickMsg / replayDoneMsg on m.bus.
	replayEvents []replay.Event
	replayIndex  int
	replayTotal  int
	replayStep   chan struct{}
	replayPace   bool
}

// Deps are the dependencies the root model needs. Store, Jobs, and Local may
// be nil to disable persistence / job submission / setup commands respectively.
type Deps struct {
	Registry           *tools.Registry
	Models             *llm.ModelRegistry
	SystemPrompt       string
	SkillLoader        *skills.Loader
	AssetReport        assets.Report
	Store              *store.Store
	Jobs               *jobmgr.Manager
	Local              *local.Registry
	FovaHome           string
	ConfigDir          string       // <ConfigDir>, used by /theme writeback; "" falls back to config.ConfigDir()
	WebhookPort        int          // Adaptyv webhook receiver port; 0 disables it
	WebhookURL         string       // Adaptyv callback URL (config-derived)
	BudgetLimitUSD     float64      // [budget].session_soft_limit_usd; 0 = no limit
	InlineGraphicsMode string       // [ui].inline_graphics override: auto|kitty|sixel|iterm2|off
	Guard              safety.Guard // optional content-filter guard; nil disables inspection
	// ReplayEvents, when non-nil, switches the TUI into read-only replay
	// mode driven by the recorded events instead of a live agent loop.
	ReplayEvents []replay.Event
	// ReplayPace controls whether replayPump waits between events. True
	// (the user-facing default) paces by the recorded timestamps capped at
	// 50 ms; false (tests) flushes the stream as fast as the bus drains.
	ReplayPace bool
}

// New builds the root model from its dependencies.
func New(d Deps) *Model {
	th := NewTheme()
	m := &Model{
		theme:        th,
		chat:         newChatModel(th, 80, 20),
		status:       newStatusBarModel(th),
		cmdbar:       newCommandBarModel(th, 80),
		registry:     d.Registry,
		models:       d.Models,
		systemPrompt: d.SystemPrompt,
		skillLoader:  d.SkillLoader,
		assetReport:  d.AssetReport,
		session:      agent.NewSession(d.SystemPrompt),
		store:        d.Store,
		jobMgr:       d.Jobs,
		localReg:     d.Local,
		fovaHome:     d.FovaHome,
		configDir:    d.ConfigDir,
		bus:          make(chan tea.Msg, 256),
		confirmCh:    make(chan bool, 1),
	}
	m.jobs = newJobsModel(th)
	m.designs = newDesignsModel(th)
	m.lab = newLabModel(th)
	m.jobLog = newJobLogView(th)
	m.keys = newKeysOverlay()
	m.slashMenu = newSlashMenu()
	m.sessionStart = time.Now().UTC()
	m.status.model = d.Models.ActiveModel()
	m.status.provider = d.Models.ActiveProviderName()
	m.status.setProject(workspaceFromHome(d.FovaHome))
	m.budgetLimit = d.BudgetLimitUSD
	m.status.costLimit = d.BudgetLimitUSD
	m.webhookURL = d.WebhookURL
	m.graphics = OverrideMode(Detect(), d.InlineGraphicsMode)
	m.guard = d.Guard
	if d.Local != nil {
		m.installFn = local.NewInstaller(d.Local).InstallLogged
	}
	if d.ReplayEvents != nil {
		// Replay mode is read-only: no persisted session, no webhook receiver,
		// no live store writes. The pump goroutine drives the chat.
		m.startReplay(d.ReplayEvents, d.ReplayPace)
		m.updateReplayStatus()
		return m
	}
	m.beginPersistedSession()
	// The Adaptyv webhook receiver runs for the TUI's lifetime; a zero port
	// (the default in tests) disables it.
	if d.WebhookPort > 0 && d.Store != nil {
		go func() {
			_ = lab.StartReceiver(context.Background(), d.WebhookPort, d.Store, m.bus)
		}()
	}
	return m
}

// inReplay reports whether the TUI is in read-only replay mode.
func (m *Model) inReplay() bool { return m.replayEvents != nil }

// addTurnCost adds a finished turn's LLM cost to the running session total,
// syncs the status bar, and appends a one-time warning once the soft budget
// limit is crossed (budgetLimit 0 = no limit, so no warning).
func (m *Model) addTurnCost(u llm.Usage) {
	m.sessionCost += m.models.CostUSD(u)
	m.status.cost = m.sessionCost
	if m.budgetLimit > 0 && m.sessionCost > m.budgetLimit && !m.budgetWarned {
		m.budgetWarned = true
		m.chat.appendError(fmt.Sprintf(
			"budget: session cost $%.2f exceeded the $%.2f soft limit",
			m.sessionCost, m.budgetLimit))
	}
}

// RenderStructure asks the SP-C renderer for a PNG of the structure at
// pdbPath, encodes it with the active graphics protocol, and appends the
// resulting escape string as a chat entry. The method degrades gracefully:
//
//   - No renderer wired (pymolRender == nil) → noop. SP-C will plug a
//     renderer in when its viz.pymol_render tool is registered; until then
//     callers (job-done hooks) can call this freely without crashing.
//   - Renderer error → surface it as an error chat entry.
//   - graphics == Off or RenderImage failed → append a text fallback line
//     pointing at the PNG path so the user still has a clickable artefact.
func (m *Model) RenderStructure(pdbPath string) {
	if m.pymolRender == nil {
		return
	}
	pngPath, err := m.pymolRender(pdbPath)
	if err != nil {
		m.chat.appendError("inline render failed: " + err.Error())
		return
	}
	esc, ok := RenderImage(m.graphics, pngPath)
	if !ok {
		m.chat.appendRaw("structure rendered: " + pngPath)
		return
	}
	m.chat.appendRaw(esc)
}

// beginPersistedSession creates a fresh session row in the store (if a store
// is configured) and attaches a sink so the session's messages are persisted.
func (m *Model) beginPersistedSession() {
	if m.store == nil {
		return
	}
	now := time.Now().UTC()
	m.sessionID = domain.SessionID(uuid.NewString())
	sess := domain.Session{
		ID:        m.sessionID,
		ProjectID: store.DefaultProjectID,
		Created:   now,
		Updated:   now,
		Model:     m.models.ActiveModel(),
		Provider:  m.models.ActiveProviderName(),
	}
	if err := m.store.InsertSession(sess); err != nil {
		m.sessionID = "" // persistence unavailable; degrade gracefully
		return
	}
	m.session.SetSink(storeSink{st: m.store, sessionID: m.sessionID})
}

// Init starts listening on the agent bus.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.waitForBus(), m.scheduleRefresh())
}

// scheduleRefresh returns a command that fires a refreshMsg after one second.
func (m *Model) scheduleRefresh() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return refreshMsg{} })
}

// reloadPanels repopulates the jobs and designs panels from the store.
func (m *Model) reloadPanels() {
	if m.store == nil {
		return
	}
	if jobs, err := m.store.ListJobs(store.DefaultProjectID); err == nil {
		m.jobs.setJobs(jobs)
	}
	if designs, err := m.store.ListDesigns(store.DefaultProjectID); err == nil {
		m.designs.setDesigns(designs)
	}
	if exps, err := m.store.ListExperiments(store.DefaultProjectID); err == nil {
		m.lab.setExperiments(exps)
	}
}

// waitForBus returns a command that delivers the next bus message.
func (m *Model) waitForBus() tea.Cmd {
	return func() tea.Msg { return <-m.bus }
}

// Update handles all messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case refreshMsg:
		m.reloadPanels()
		m.refreshJobLogs()
		return m, m.scheduleRefresh()

	case spinnerTickMsg:
		// Keep the thinking indicator animating only while a turn runs.
		if m.running {
			m.thinking.tick()
			return m, spinnerTick()
		}
		return m, nil

	// --- agent bus messages ---
	case agent.TextDeltaMsg:
		m.chat.appendAgentDelta(msg.Delta)
		return m, m.waitForBus()
	case agent.ToolStartMsg:
		m.thinking.verb = verbForTool(msg.Name)
		m.chat.appendToolStart(msg.Name)
		return m, m.waitForBus()
	case agent.ToolDoneMsg:
		if msg.Err != nil {
			m.chat.appendToolDone(msg.Name, "error: "+msg.Err.Error())
		} else {
			m.chat.appendToolDone(msg.Name, msg.Display)
		}
		return m, m.waitForBus()
	case agent.ConfirmContextMsg:
		m.pendingTool, m.pendingInput = msg.Tool, msg.Input
		return m, m.waitForBus()
	case agent.ConfirmRequestMsg:
		if m.pendingTool == "lab.submit_experiment" {
			m.submit = buildSubmitModal(m.pendingInput, m.webhookURL)
			m.overlay = overlaySubmit
		} else {
			m.modal = modalModel{prompt: msg.Prompt}
			m.overlay = overlayConfirm
		}
		m.pendingTool, m.pendingInput = "", nil
		return m, m.waitForBus()
	case agent.ReasoningDeltaMsg:
		// Chain-of-thought is dropped in v0.5 — the spinning "Thinking…"
		// indicator already signals it is in flight. A future build may
		// surface this in a collapsible block.
		return m, m.waitForBus()
	case agent.TurnDoneMsg:
		m.running = false
		m.turnCancel = nil
		m.thinking.stop()
		m.cmdbar.setRunning(false)
		m.addTurnCost(msg.Usage)
		return m, m.waitForBus()
	case agent.TurnErrorMsg:
		m.running = false
		m.turnCancel = nil
		m.thinking.stop()
		m.cmdbar.setRunning(false)
		if !errors.Is(msg.Err, context.Canceled) {
			m.chat.appendError(msg.Err.Error())
		}
		return m, m.waitForBus()
	case lab.WebhookEventMsg:
		m.reloadPanels()
		m.chat.appendAgentDeltaBlock("wet-lab update received for experiment " + string(msg.ExperimentID))
		return m, m.waitForBus()
	case replayTickMsg:
		m.applyReplayEvent(msg.event)
		m.replayIndex = msg.index
		m.updateReplayStatus()
		return m, m.waitForBus()
	case replayDoneMsg:
		m.updateReplayStatus()
		return m, m.waitForBus()
	case editorDoneMsg:
		if msg.Err != nil {
			m.chat.appendError("editor: " + msg.Err.Error())
		} else {
			m.cmdbar.ta.SetValue(msg.Contents)
			m.cmdbar.ta.CursorEnd()
			if m.cmdbar.refreshHeight() {
				m.layout()
			}
		}
		return m, nil
	case editorFileDoneMsg:
		if msg.Err != nil {
			m.chat.appendError("editor: " + msg.Err.Error())
			m.pendingAssetReload = false
			m.pendingAssetPath = ""
			return m, nil
		}
		if m.pendingAssetReload {
			m.pendingAssetReload = false
			m.pendingAssetPath = ""
			return m.cmdReload()
		}
		return m, nil
	}

	// Forward anything else to the text input.
	var cmd tea.Cmd
	m.cmdbar.ta, cmd = m.cmdbar.ta.Update(msg)
	if m.cmdbar.refreshHeight() {
		m.layout()
	}
	return m, cmd
}

// handleKey routes key presses, honouring the active overlay.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayConfirm, overlaySubmit:
		switch msg.String() {
		case "y", "Y":
			m.overlay = overlayNone
			m.confirmCh <- true
		case "n", "N", "esc":
			m.overlay = overlayNone
			m.confirmCh <- false
		case "ctrl+c":
			m.overlay = overlayNone
			m.chat.appendError("cancelled")
			if m.turnCancel != nil {
				m.turnCancel()
			}
			m.confirmCh <- false
		}
		return m, nil
	case overlayPicker:
		switch msg.String() {
		case "up", "k":
			m.picker.prev()
		case "down", "j":
			m.picker.next()
		case "enter":
			m.applyPickerSelection()
			m.overlay = overlayNone
		case "esc":
			m.overlay = overlayNone
		case "ctrl+c":
			m.overlay = overlayNone
			if m.running && m.turnCancel != nil {
				m.chat.appendError("cancelled")
				m.turnCancel()
			}
		}
		return m, nil
	case overlayJobLog:
		switch msg.Type {
		case tea.KeyTab:
			m.cycleFocus()
		case tea.KeyEsc:
			m.overlay, m.focus, m.jobLogID = overlayNone, focusChat, ""
		case tea.KeyCtrlD:
			return m, tea.Quit
		case tea.KeyCtrlC:
			if m.running && m.turnCancel != nil {
				m.turnCancel()
				m.chat.appendError("cancelled")
			}
		default:
			m.jobLog = m.jobLog.update(msg)
		}
		return m, nil
	case overlayKeys:
		switch msg.String() {
		case "esc", "?", "q":
			m.overlay = overlayNone
		case "ctrl+c":
			m.overlay = overlayNone
		case "ctrl+d":
			return m, tea.Quit
		}
		return m, nil
	}

	// The slash-command autocomplete popup, when open, captures navigation keys.
	if m.showSlashMenu {
		switch msg.Type {
		case tea.KeyUp:
			m.slashMenu.prev()
			return m, nil
		case tea.KeyDown:
			m.slashMenu.next()
			return m, nil
		case tea.KeyTab, tea.KeyEnter:
			m.completeSlashCommand()
			return m, nil
		case tea.KeyEsc:
			m.showSlashMenu = false
			return m, nil
		}
	}

	switch msg.Type {
	case tea.KeyTab:
		m.cycleFocus()
		return m, nil
	case tea.KeyCtrlD:
		return m, tea.Quit
	case tea.KeyCtrlC:
		if m.running && m.turnCancel != nil {
			m.turnCancel()
			m.chat.appendError("cancelled")
		}
		return m, nil
	case tea.KeyEsc:
		if m.inReplay() {
			return m, tea.Quit
		}
		if m.running && m.turnCancel != nil {
			m.turnCancel()
			m.chat.appendError("cancelled")
		}
		return m, nil
	case tea.KeySpace:
		if m.inReplay() {
			m.stepReplay()
			return m, nil
		}
	case tea.KeyEnter:
		if msg.Alt { // Alt+Enter → newline
			break
		}
		return m.submitInput()
	case tea.KeyPgUp:
		m.chat.viewport.PageUp()
		return m, nil
	case tea.KeyPgDown:
		m.chat.viewport.PageDown()
		return m, nil
	case tea.KeyHome:
		m.chat.viewport.GotoTop()
		return m, nil
	case tea.KeyEnd:
		m.chat.viewport.GotoBottom()
		return m, nil
	case tea.KeyCtrlL:
		return m.runSlashCommand("clear", "")
	case tea.KeyCtrlR:
		return m.cmdReload()
	case tea.KeyCtrlX:
		return m, openEditorCmd(m.cmdbar.value())
	}

	// `?` typed on an empty input opens the /keys overlay. A non-empty input
	// (the user is composing a message) means `?` is a literal character.
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '?' &&
		strings.TrimSpace(m.cmdbar.value()) == "" {
		m.overlay = overlayKeys
		return m, nil
	}

	var cmd tea.Cmd
	m.cmdbar.ta, cmd = m.cmdbar.ta.Update(msg)
	if m.cmdbar.refreshHeight() {
		m.layout()
	}
	m.refreshSlashMenu()
	return m, cmd
}

// refreshSlashMenu shows or hides the autocomplete popup based on the current
// input line. Bare "/foo" filters the top-level catalogue; "/foo " (trailing
// space) or "/foo b" switches to per-command mode and surfaces sub-commands
// or live argument lists (installed tools, model IDs, auth providers).
func (m *Model) refreshSlashMenu() {
	line := m.cmdbar.value()
	// Strip the leading whitespace only — a trailing space is the trigger
	// for per-command mode, so it must be preserved.
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "/") {
		m.showSlashMenu = false
		return
	}
	rows := MatchSlash(trimmed, slashCommands,
		m.installedToolNames(), m.modelIDs(), knownAuthProviders)
	m.slashMenu.setRows(rows)
	m.showSlashMenu = m.slashMenu.visible()
}

// installedToolNames returns the names of every tool in the local registry,
// or nil when the registry is unavailable. Resolved per keystroke so newly
// installed tools appear in the menu without a restart.
func (m *Model) installedToolNames() []string {
	if m.localReg == nil {
		return nil
	}
	recs := m.localReg.Tools()
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Name)
	}
	return out
}

// modelIDs returns every registered model ID, or nil when the model registry
// is unavailable.
func (m *Model) modelIDs() []string {
	if m.models == nil {
		return nil
	}
	mods := m.models.Models()
	out := make([]string, 0, len(mods))
	for _, mod := range mods {
		out = append(out, mod.ID)
	}
	return out
}

// completeSlashCommand handles Tab / Enter in the popup. With a unique row,
// the row's Insert is written to the input. With several rows, the longest
// common prefix is written so the user sees the popup narrow without
// committing to any one row.
func (m *Model) completeSlashCommand() {
	rows := m.slashMenu.rows()
	if len(rows) == 0 {
		return
	}
	row, ok := m.slashMenu.selected()
	if !ok {
		return
	}
	if len(rows) == 1 {
		m.cmdbar.ta.SetValue(row.Insert)
		m.cmdbar.ta.CursorEnd()
		m.showSlashMenu = false
		return
	}
	pref := LongestCommonPrefix(rows)
	current := m.cmdbar.value()
	// If the common prefix isn't strictly longer than the input, the user
	// has already typed past it: commit the highlighted row instead.
	if len(pref) <= len(current) {
		m.cmdbar.ta.SetValue(row.Insert)
		m.cmdbar.ta.CursorEnd()
		m.showSlashMenu = false
		return
	}
	m.cmdbar.ta.SetValue(pref)
	m.cmdbar.ta.CursorEnd()
	// Re-filter so the user sees the narrowed list.
	m.refreshSlashMenu()
}

// submitInput consumes the input line: a slash command or an agent turn.
func (m *Model) submitInput() (tea.Model, tea.Cmd) {
	line := m.cmdbar.value()
	if m.inReplay() {
		// Replay is read-only: the input bar is a no-op.
		m.cmdbar.reset()
		return m, nil
	}
	if line == "" {
		return m, nil
	}
	m.cmdbar.reset()
	m.showSlashMenu = false

	if cmd, arg, isSlash := parseSlashCommand(line); isSlash {
		return m.runSlashCommand(cmd, arg)
	}
	if m.running {
		m.chat.appendError("agent is busy — wait for the current turn")
		return m, nil
	}
	m.chat.appendUser(line)
	return m.startTurn(line)
}

// startTurn launches the agent loop for one user message.
func (m *Model) startTurn(input string) (tea.Model, tea.Cmd) {
	provider, err := m.models.Provider()
	if err != nil {
		m.chat.appendError(err.Error())
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.turnCancel = cancel
	m.running = true
	m.turnStart = time.Now()
	m.thinking.start("Thinking", m.turnStart)
	m.cmdbar.setRunning(true)

	loop := agent.NewLoopWithGuard(provider, m.models.ActiveModel(), m.registry, m.session,
		m.bus, m.confirmFn, m.guard)
	go loop.Run(ctx, input)
	return m, spinnerTick()
}

// confirmFn is passed to the loop; it asks the TUI and blocks for the answer.
func (m *Model) confirmFn(prompt string) bool {
	m.bus <- agent.ConfirmRequestMsg{Prompt: prompt}
	return <-m.confirmCh
}

// runSlashCommand handles the v0.1 slash-command set.
func (m *Model) runSlashCommand(cmd, arg string) (tea.Model, tea.Cmd) {
	switch cmd {
	case "quit":
		return m, tea.Quit
	case "help":
		m.chat.appendAgentDeltaBlock(helpText())
		return m, nil
	case "clear":
		m.chat = newChatModel(m.theme, m.chatWidth(), m.chatHeight())
		m.session = agent.NewSession(m.systemPrompt)
		m.beginPersistedSession()
		return m, nil
	case "model":
		if arg != "" {
			m.applyModel(arg)
			return m, nil
		}
		m.openModelPicker()
		return m, nil
	case "provider":
		// /provider was merged into /model — selecting a model switches the
		// provider too. Keep a redirect so existing muscle memory still lands.
		m.chat.appendAgentDeltaBlock("use /model — it switches the provider too.")
		return m, nil
	case "plan":
		return m.runPlanCommand(arg)
	case "doctor":
		return m.cmdDoctor()
	case "tools":
		return m.cmdTools()
	case "install":
		return m.cmdInstall(arg)
	case "uninstall":
		return m.cmdUninstall(arg)
	case "modal":
		return m.cmdModalDeploy(arg)
	case "auth":
		return m.cmdAuth(arg)
	case "theme":
		return m.cmdTheme(arg)
	case "reload":
		return m.cmdReload()
	case "skills":
		return m.cmdSkills(arg)
	case "config":
		return m.cmdConfig(arg)
	case "keys":
		return m.cmdKeys()
	case "jobs", "designs", "lab", "export", "cost", "project":
		m.chat.appendAgentDeltaBlock("/" + cmd + " arrives in a later fova milestone.")
		return m, nil
	default:
		m.chat.appendError("unknown command: /" + cmd)
		return m, nil
	}
}

// runPlanCommand handles /plan and its sub-arguments.
func (m *Model) runPlanCommand(arg string) (tea.Model, tea.Cmd) {
	switch arg {
	case "":
		if m.store == nil {
			m.chat.appendAgentDeltaBlock(renderNoPlan())
			return m, nil
		}
		p, ok, err := m.store.LatestPlan(store.DefaultProjectID)
		if err != nil {
			m.chat.appendError("could not load the design plan: " + err.Error())
			return m, nil
		}
		if !ok {
			m.chat.appendAgentDeltaBlock(renderNoPlan())
			return m, nil
		}
		m.chat.appendSlashOutput(renderPlan(p))
		return m, nil

	case "approve":
		if m.store == nil {
			m.chat.appendAgentDeltaBlock("No design plan to approve — ask the agent to plan from a target first.")
			return m, nil
		}
		p, ok, err := m.store.LatestPlan(store.DefaultProjectID)
		if err != nil {
			m.chat.appendError("could not load the design plan: " + err.Error())
			return m, nil
		}
		if !ok {
			m.chat.appendAgentDeltaBlock("No design plan to approve — ask the agent to plan from a target first.")
			return m, nil
		}
		if err := m.store.SetPlanApproved(p.ID); err != nil {
			m.chat.appendError("could not approve the design plan: " + err.Error())
			return m, nil
		}
		m.chat.appendAgentDeltaBlock("plan " + string(p.ID) + " approved")
		return m, nil

	case "cancel":
		m.chat.appendAgentDeltaBlock("plan cancelled — ask the agent to plan from a target again")
		return m, nil

	default:
		m.chat.appendError("unknown /plan argument; use /plan, /plan approve, or /plan cancel")
		return m, nil
	}
}

// cmdAuth handles /auth <provider> <token>. Only "adaptyv" is supported; the
// token is written to the OS keychain.
func (m *Model) cmdAuth(arg string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(arg)
	if len(fields) < 2 || fields[0] != "adaptyv" {
		m.chat.appendError("usage: /auth adaptyv <token>")
		return m, nil
	}
	if err := lab.StoreToken(strings.Join(fields[1:], " ")); err != nil {
		m.chat.appendError("could not store the Adaptyv token: " + err.Error())
		return m, nil
	}
	m.chat.appendAgentDeltaBlock("Adaptyv token stored in the OS keychain.")
	return m, nil
}

// cmdTheme implements /theme <auto|light|dark>: it applies the mode live via
// lipgloss.SetHasDarkBackground and persists the choice to config.toml so the
// next launch starts in the same mode.
func (m *Model) cmdTheme(arg string) (tea.Model, tea.Cmd) {
	mode := strings.TrimSpace(arg)
	if mode != "auto" && mode != "light" && mode != "dark" {
		m.chat.appendError("usage: /theme auto|light|dark")
		return m, nil
	}
	// Persist first so a save failure doesn't leave the UI out of sync.
	if err := m.saveThemeChoice(mode); err != nil {
		m.chat.appendError("could not save theme: " + err.Error())
		return m, nil
	}
	ApplyTheme(mode)
	m.chat.appendAgentDeltaBlock("theme set to " + mode + " (persisted to config.toml)")
	return m, nil
}

// saveThemeChoice loads the on-disk config, sets UI.Theme to mode, and writes
// it back via config.SaveConfig. The lipgloss.SetHasDarkBackground call lives
// in the caller — saveThemeChoice owns disk I/O only.
func (m *Model) saveThemeChoice(mode string) error {
	dir := m.configDir
	if dir == "" {
		dir = config.ConfigDir()
	}
	// FOVA_CONFIG_DIR is what LoadConfig/SaveConfig consult; setting it
	// here keeps the save targeted at the Deps-supplied directory without
	// reaching into config's private state.
	prev, hadPrev := lookupEnv("FOVA_CONFIG_DIR")
	_ = os.Setenv("FOVA_CONFIG_DIR", dir)
	defer func() {
		if hadPrev {
			_ = os.Setenv("FOVA_CONFIG_DIR", prev)
		} else {
			_ = os.Unsetenv("FOVA_CONFIG_DIR")
		}
	}()
	c, err := config.LoadConfig()
	if err != nil {
		return err
	}
	c.UI.Theme = mode
	return config.SaveConfig(c)
}

// lookupEnv is a tiny wrapper so saveThemeChoice can be read top-to-bottom.
func lookupEnv(key string) (string, bool) { return os.LookupEnv(key) }

// cmdReload re-reads every asset (config.toml, models.toml, system.md,
// skills) without restarting the TUI. The theme is applied live, the model
// registry and skill set are swapped, and the running agent's system prompt
// is hot-swapped. Conversation history is untouched.
func (m *Model) cmdReload() (tea.Model, tea.Cmd) {
	dir := m.configDir
	if dir == "" {
		dir = assets.Dir()
	}
	prev, hadPrev := lookupEnv("FOVA_CONFIG_DIR")
	_ = os.Setenv("FOVA_CONFIG_DIR", dir)
	defer func() {
		if hadPrev {
			_ = os.Setenv("FOVA_CONFIG_DIR", prev)
		} else {
			_ = os.Unsetenv("FOVA_CONFIG_DIR")
		}
	}()
	bundle, err := assets.Load()
	if err != nil {
		m.chat.appendError("reload: " + err.Error())
		return m, nil
	}
	if m.models == nil {
		m.models = llm.NewModelRegistry(bundle.Models)
	} else {
		m.models.Reload(bundle.Models)
	}
	if err := m.models.SelectDefault(bundle.Config.Defaults); err != nil {
		m.chat.appendError("apply [defaults] from config.toml: " + err.Error())
	}
	ApplyTheme(bundle.Config.UI.Theme)
	m.budgetLimit = bundle.Config.Budget.SessionSoftLimitUSD
	m.status.costLimit = bundle.Config.Budget.SessionSoftLimitUSD
	m.webhookURL = bundle.Config.Webhook.EffectiveURL()
	m.status.model = m.models.ActiveModel()
	m.status.provider = m.models.ActiveProviderName()

	// Swap the skill set and re-register the skills.list/read tools.
	m.skillLoader = skills.NewLoader(bundle.Skills)
	if m.registry != nil {
		m.registry.Register(m.skillLoader.ListTool())
		m.registry.Register(m.skillLoader.ReadTool())
	}
	// Hot-swap the system prompt for the next agent turn.
	m.systemPrompt = agent.BuildSystemPrompt(Commands(), bundle.SystemPrompt)
	if m.session != nil {
		m.session.SetSystemPrompt(m.systemPrompt)
	}
	m.assetReport = bundle.Report

	msg := "reloaded config.toml, models.toml, system.md and skills"
	if s := bundle.Report.Summary(); s != "" {
		msg += " — " + s
	}
	m.chat.appendAgentDeltaBlock(msg)
	return m, nil
}

// cmdKeys opens the /keys overlay listing every keybinding from the
// keybindings() table.
func (m *Model) cmdKeys() (tea.Model, tea.Cmd) {
	m.overlay = overlayKeys
	return m, nil
}

// buildSubmitModal parses a lab.submit_experiment tool input into the rich
// confirmation overlay (SPECS §12.2). defaultURL is shown when the request
// carries no webhook_url of its own.
func buildSubmitModal(input json.RawMessage, defaultURL string) submitModal {
	var req lab.SubmitRequest
	_ = json.Unmarshal(input, &req)
	seqs := make([]string, 0, len(req.Sequences))
	for _, s := range req.Sequences {
		seqs = append(seqs, s.Sequence)
	}
	url := req.WebhookURL
	if url == "" {
		url = defaultURL
	}
	return submitModal{
		TargetName: orDash(req.TargetID),
		AssayType:  orDash(req.AssayType),
		Sequences:  seqs,
		WebhookURL: url,
	}
}

func (m *Model) openModelPicker() {
	items := make([]pickerItem, 0)
	for _, mod := range m.models.Models() {
		items = append(items, pickerItem{id: mod.ID, label: mod.DisplayName + "  (" + mod.ProviderName + ")"})
	}
	m.picker = newPicker("Select model", items)
	m.overlay = overlayPicker
}

func (m *Model) applyPickerSelection() {
	if m.picker == nil {
		return
	}
	m.applyModel(m.picker.selected().id)
}

func (m *Model) applyModel(id string) {
	if err := m.models.SetModel(id); err != nil {
		m.chat.appendError(err.Error())
		return
	}
	m.status.model = m.models.ActiveModel()
	m.status.provider = m.models.ActiveProviderName()
	m.chat.appendAgentDeltaBlock("Switched to " + m.status.model + " (" + m.status.provider + ").")
}

// runningJobIDs returns the IDs of currently-running jobs, in panel order.
func (m *Model) runningJobIDs() []string {
	var ids []string
	for _, j := range m.jobs.jobs {
		if j.Status == domain.JobRunning {
			ids = append(ids, string(j.ID))
		}
	}
	return ids
}

// refreshJobLogs upserts an in-chat log block for every job submitted during
// this session, and refreshes the full-screen view when one is open.
func (m *Model) refreshJobLogs() {
	for _, j := range m.jobs.jobs {
		if j.LogFile == "" || j.Created.Before(m.sessionStart) {
			continue
		}
		m.chat.upsertJobLog(string(j.ID), j.Tool, j.Status, j.Started, tailLines(j.LogFile, 6))
	}
	if m.overlay == overlayJobLog && m.jobLogID != "" {
		m.openJobLog(m.jobLogID)
	}
}

// openJobLog loads job id's complete log into the full-screen view.
func (m *Model) openJobLog(id string) {
	var job domain.Job
	for _, j := range m.jobs.jobs {
		if string(j.ID) == id {
			job = j
			break
		}
	}
	body := readLog(job.LogFile)
	if strings.TrimSpace(body) == "" {
		body = "(no output yet)"
	}
	m.jobLog.setSize(m.width, m.height)
	m.jobLog.setContent(glyph(job.Status)+" "+job.Tool+" · "+id, body)
}

// cycleFocus advances the unified Tab focus ring: chat → each running job's
// full-screen log → the jobs / designs / lab panels → back to chat.
func (m *Model) cycleFocus() {
	jobs := m.runningJobIDs()
	total := 1 + len(jobs) + 3 // chat + running jobs + 3 panels
	cur := 0
	switch {
	case m.overlay == overlayJobLog:
		for i, id := range jobs {
			if id == m.jobLogID {
				cur = 1 + i
			}
		}
	case m.focus == focusJobs:
		cur = 1 + len(jobs)
	case m.focus == focusDesigns:
		cur = 2 + len(jobs)
	case m.focus == focusLab:
		cur = 3 + len(jobs)
	}
	switch next := (cur + 1) % total; {
	case next == 0:
		m.overlay, m.focus, m.jobLogID = overlayNone, focusChat, ""
	case next <= len(jobs):
		m.jobLogID = jobs[next-1]
		m.overlay = overlayJobLog
		m.openJobLog(m.jobLogID)
	default:
		m.overlay, m.jobLogID = overlayNone, ""
		switch next - 1 - len(jobs) {
		case 0:
			m.focus = focusJobs
		case 1:
			m.focus = focusDesigns
		default:
			m.focus = focusLab
		}
	}
}

// View composes the screen (SPECS §10.2 / §10.7): a slim header, the chat /
// panels body, the thinking indicator, an optional slash-command popup, the
// bordered message input, and the status footer.
func (m *Model) View() string {
	if m.width == 0 {
		return "starting fova…"
	}
	var body string
	if m.width >= 100 {
		right := lipgloss.JoinVertical(lipgloss.Left,
			m.jobs.View(), "", m.designs.View(), "", m.lab.View())
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.chat.View(), "  ", right)
	} else {
		switch m.focus {
		case focusJobs:
			body = m.jobs.View()
		case focusDesigns:
			body = m.designs.View()
		case focusLab:
			body = m.lab.View()
		default:
			body = m.chat.View()
		}
	}

	// The thinking line renders empty when idle, holding its row steady.
	parts := []string{m.renderHeader(), body, m.thinking.view(m.theme, time.Now())}
	if m.showSlashMenu {
		parts = append(parts, m.slashMenu.view(m.theme, m.width))
	}
	parts = append(parts, m.cmdbar.View(), m.status.footerView())
	base := strings.Join(parts, "\n")

	switch m.overlay {
	case overlayConfirm:
		return base + "\n" + m.modal.view(m.theme, m.width)
	case overlaySubmit:
		return base + "\n" + m.submit.view(m.theme, m.width)
	case overlayPicker:
		return base + "\n" + m.picker.view(m.theme, m.width)
	case overlayJobLog:
		return m.jobLog.View()
	case overlayKeys:
		return base + "\n" + m.keys.view(m.theme, m.width)
	}
	return base
}

// layout recomputes child sizes for the current terminal dimensions.
func (m *Model) layout() {
	m.status.width = m.width
	m.cmdbar.setWidth(m.width)
	panelW := 0
	if m.width >= 100 {
		panelW = 38
	}
	m.jobs.setWidth(panelW)
	m.designs.setWidth(panelW)
	m.lab.setWidth(panelW)
	m.jobLog.setSize(m.width, m.height)
	chatW := m.width
	if panelW > 0 {
		chatW = m.width - panelW - 2 // 2 spaces of gap
	}
	m.chat.resize(chatW, m.chatHeight())
}

func (m *Model) chatWidth() int { return m.width }

// chatHeight reserves rows for the fova header (6, rebrand spec §3.1), the
// thinking line (1), the bordered message input (label + top border + N
// textarea rows + bottom border), and the footer (1) — so 11 fixed rows
// plus the current input height. As the input grows / shrinks, the chat
// pane absorbs the change.
func (m *Model) chatHeight() int {
	h := m.height - 11 - m.cmdbar.inputHeight()
	if h < 3 {
		h = 3
	}
	return h
}

// helpText renders the /help block from the slash-command catalogue. Each
// command is followed by its keyword sub-commands so users can discover
// /plan approve / /plan cancel without invoking the popup.
func helpText() string {
	var b strings.Builder
	b.WriteString("fova " + version.String() + " — type a message to talk to the agent.\n")
	b.WriteString("Slash commands:\n")
	for _, c := range slashCommands {
		b.WriteString("  /" + c.Name + " — " + c.Description + "\n")
		for _, sc := range c.Subcommands {
			b.WriteString("      /" + c.Name + " " + sc.Name + " — " + sc.Description + "\n")
		}
	}
	b.WriteString("Keys: Esc or Ctrl+C cancels the running turn · Ctrl+D quits · Tab cycles panels.")
	return b.String()
}

// loadConfigForTest is a re-export of config.LoadConfig used by tests in this
// package that do not import internal/config directly. Not part of the public
// API; safe to remove if no test refers to it.
func loadConfigForTest() (any, error) { return config.LoadConfig() }

// workspaceFromHome mirrors cmd/fova.defaultWorkspace: the active project's
// workspace lives at $FOVA_HOME/projects/default. An empty home (tests that
// don't set it) returns "" so the header omits the project segment instead of
// showing a meaningless "/projects/default" prefix.
func workspaceFromHome(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(home, "projects", "default")
}

// renderHeader builds the 6-line fova header (rebrand spec §3.1) from the
// model's live state: version (build-info), active model id, and project
// workspace paths. Empty inputs render gracefully — the header always
// occupies six lines so the layout below it doesn't shift around.
func (m *Model) renderHeader() string {
	model := ""
	if m.models != nil {
		model = m.models.ActiveModel()
	}
	full := workspaceFromHome(m.fovaHome)
	return RenderHeader(m.theme, HeaderInput{
		Version:   "fova " + version.String(),
		Model:     model,
		FullPath:  full,
		ShortPath: tildeShorten(full),
	})
}

// tildeShorten collapses the user's home-directory prefix in path into "~".
// It is the cosmetic short form shown on header line 3 and is purely a
// display helper — callers that need the canonical path use the original.
func tildeShorten(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}
