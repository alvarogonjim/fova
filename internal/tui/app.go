package tui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"github.com/alvarogonjim/proteus/internal/agent"
	"github.com/alvarogonjim/proteus/internal/backends/local"
	"github.com/alvarogonjim/proteus/internal/domain"
	jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
	"github.com/alvarogonjim/proteus/internal/tools/lab"
	"github.com/alvarogonjim/proteus/internal/version"
)

// overlay identifies the active modal overlay, if any.
type overlay int

const (
	overlayNone overlay = iota
	overlayConfirm
	overlaySubmit
	overlayPicker
	overlayJobLog
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

	theme  Theme
	chat   *chatModel
	status statusBarModel
	cmdbar commandBarModel

	jobs    jobsModel
	designs designsModel
	lab     labModel
	focus   panelFocus

	jobLog       jobLogView // full-screen log view for the Tab-focused job
	jobLogID     string     // ID of the job shown in jobLog ("" = none)
	sessionStart time.Time  // jobs created before this aren't blocked into chat

	registry     *tools.Registry
	models       *llm.ModelRegistry
	systemPrompt string
	session      *agent.Session   // one session for the whole TUI lifetime
	store        *store.Store     // nil → persistence disabled
	sessionID    domain.SessionID // current persisted session

	jobMgr      *jobmgr.Manager // async job manager (install / deploy / design jobs)
	localReg    *local.Registry // installable-tool registry
	proteusHome string          // $PROTEUS_HOME, used for setup log-file paths

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

	// pendingTool / pendingInput hold the tool context from a ConfirmContextMsg
	// until the paired ConfirmRequestMsg arrives.
	pendingTool  string
	pendingInput json.RawMessage

	thinking      thinkingModel   // animated "thinking" indicator (SPECS §10.7.4)
	slashMenu     *slashMenuModel // slash-command autocomplete popup (§10.7.3)
	showSlashMenu bool            // whether the popup is currently shown
	turnStart     time.Time       // start of the current turn, for the elapsed counter
}

// Deps are the dependencies the root model needs. Store, Jobs, and Local may
// be nil to disable persistence / job submission / setup commands respectively.
type Deps struct {
	Registry     *tools.Registry
	Models       *llm.ModelRegistry
	SystemPrompt string
	Store        *store.Store
	Jobs         *jobmgr.Manager
	Local        *local.Registry
	ProteusHome  string
	WebhookPort  int // Adaptyv webhook receiver port; 0 disables it
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
		session:      agent.NewSession(d.SystemPrompt),
		store:        d.Store,
		jobMgr:       d.Jobs,
		localReg:     d.Local,
		proteusHome:  d.ProteusHome,
		bus:          make(chan tea.Msg, 256),
		confirmCh:    make(chan bool, 1),
	}
	m.jobs = newJobsModel(th)
	m.designs = newDesignsModel(th)
	m.lab = newLabModel(th)
	m.jobLog = newJobLogView(th)
	m.slashMenu = newSlashMenu()
	m.sessionStart = time.Now().UTC()
	m.status.model = d.Models.ActiveModel()
	m.status.provider = d.Models.ActiveProviderName()
	m.chat.appendWelcome(welcomeText(d.Models.ActiveModel()))
	if d.Local != nil {
		m.installFn = local.NewInstaller(d.Local).InstallLogged
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
			m.submit = buildSubmitModal(m.pendingInput)
			m.overlay = overlaySubmit
		} else {
			m.modal = modalModel{prompt: msg.Prompt}
			m.overlay = overlayConfirm
		}
		m.pendingTool, m.pendingInput = "", nil
		return m, m.waitForBus()
	case agent.TurnDoneMsg:
		m.running = false
		m.turnCancel = nil
		m.thinking.stop()
		m.cmdbar.setRunning(false)
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
	}

	// Forward anything else to the text input.
	var cmd tea.Cmd
	m.cmdbar.ta, cmd = m.cmdbar.ta.Update(msg)
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
		case tea.KeyTab:
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
	case tea.KeyEnter:
		if msg.Alt { // Alt+Enter → newline
			break
		}
		return m.submitInput()
	}

	var cmd tea.Cmd
	m.cmdbar.ta, cmd = m.cmdbar.ta.Update(msg)
	m.refreshSlashMenu()
	return m, cmd
}

// refreshSlashMenu shows or hides the autocomplete popup based on the current
// input line: it is visible while the user is typing a slash-command word
// (a leading "/" with no space yet).
func (m *Model) refreshSlashMenu() {
	line := strings.TrimSpace(m.cmdbar.value())
	if strings.HasPrefix(line, "/") && !strings.Contains(line, " ") {
		m.slashMenu.setFilter(strings.TrimPrefix(line, "/"))
		m.showSlashMenu = m.slashMenu.visible()
		return
	}
	m.showSlashMenu = false
}

// completeSlashCommand replaces the input with the highlighted command and
// closes the popup (SPECS §10.7.3).
func (m *Model) completeSlashCommand() {
	cmd, ok := m.slashMenu.selected()
	if !ok {
		return
	}
	m.cmdbar.ta.SetValue("/" + cmd.Name + " ")
	m.cmdbar.ta.CursorEnd()
	m.showSlashMenu = false
}

// submitInput consumes the input line: a slash command or an agent turn.
func (m *Model) submitInput() (tea.Model, tea.Cmd) {
	line := m.cmdbar.value()
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

	loop := agent.NewLoop(provider, m.models.ActiveModel(), m.registry, m.session,
		m.bus, m.confirmFn)
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
	case "jobs", "designs", "lab", "export", "cost", "project", "skills":
		m.chat.appendAgentDeltaBlock("/" + cmd + " arrives in a later Proteus milestone.")
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
		m.chat.appendAgentDeltaBlock(renderPlan(p))
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

// buildSubmitModal parses a lab.submit_experiment tool input into the rich
// confirmation overlay (SPECS §12.2).
func buildSubmitModal(input json.RawMessage) submitModal {
	var req lab.SubmitRequest
	_ = json.Unmarshal(input, &req)
	seqs := make([]string, 0, len(req.Sequences))
	for _, s := range req.Sequences {
		seqs = append(seqs, s.Sequence)
	}
	url := req.WebhookURL
	if url == "" {
		url = "http://localhost:9876/webhooks/adaptyv"
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
		return "starting proteus…"
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
	parts := []string{m.status.headerView(), body, m.thinking.view(m.theme, time.Now())}
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

// chatHeight reserves rows for the header (1), the thinking line (1), the
// bordered message input (6: a label line plus a 3-line textarea in a border),
// and the footer (1).
func (m *Model) chatHeight() int {
	h := m.height - 9
	if h < 3 {
		h = 3
	}
	return h
}

// welcomeText is the compact startup block shown in the chat pane on launch
// (SPECS §10.7.7).
func welcomeText(model string) string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "?"
	}
	return "proteus " + version.String() + " · de novo protein design\n" +
		"cwd: " + cwd + "   model: " + orDash(model) + "\n" +
		"Type a message, or / for commands.  /help for keybindings."
}

// helpText renders the /help block from the slash-command catalogue.
func helpText() string {
	var b strings.Builder
	b.WriteString("proteus " + version.String() + " — type a message to talk to the agent.\n")
	b.WriteString("Slash commands:\n")
	for _, c := range slashCommands {
		b.WriteString("  /" + c.Name + " — " + c.Description + "\n")
	}
	b.WriteString("Keys: Ctrl+C cancels the running turn · Ctrl+D quits · Tab cycles panels.")
	return b.String()
}
