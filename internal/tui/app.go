package tui

import (
	"context"
	"errors"
	"io"
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
)

// overlay identifies the active modal overlay, if any.
type overlay int

const (
	overlayNone overlay = iota
	overlayConfirm
	overlayPicker
)

// panelFocus is which pane Tab-cycling currently targets (used for the
// narrow-terminal single-pane layout).
type panelFocus int

const (
	focusChat panelFocus = iota
	focusJobs
	focusDesigns

	// numPanelFocus is the number of Tab-cyclable panes.
	numPanelFocus panelFocus = focusDesigns + 1
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
	focus   panelFocus

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
	picker  *pickerModel
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
	m.status.model = d.Models.ActiveModel()
	m.status.provider = d.Models.ActiveProviderName()
	if d.Local != nil {
		m.installFn = local.NewInstaller(d.Local).InstallLogged
	}
	m.beginPersistedSession()
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
		return m, m.scheduleRefresh()

	// --- agent bus messages ---
	case agent.TextDeltaMsg:
		m.chat.appendAgentDelta(msg.Delta)
		return m, m.waitForBus()
	case agent.ToolStartMsg:
		m.chat.appendToolStart(msg.Name)
		return m, m.waitForBus()
	case agent.ToolDoneMsg:
		if msg.Err != nil {
			m.chat.appendToolDone(msg.Name, "error: "+msg.Err.Error())
		} else {
			m.chat.appendToolDone(msg.Name, msg.Display)
		}
		return m, m.waitForBus()
	case agent.ConfirmRequestMsg:
		m.overlay = overlayConfirm
		m.modal = modalModel{prompt: msg.Prompt}
		return m, m.waitForBus()
	case agent.TurnDoneMsg:
		m.running = false
		m.turnCancel = nil
		return m, m.waitForBus()
	case agent.TurnErrorMsg:
		m.running = false
		m.turnCancel = nil
		if !errors.Is(msg.Err, context.Canceled) {
			m.chat.appendError(msg.Err.Error())
		}
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
	case overlayConfirm:
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
	}

	switch msg.Type {
	case tea.KeyTab:
		m.focus = (m.focus + 1) % numPanelFocus
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
	return m, cmd
}

// submitInput consumes the input line: a slash command or an agent turn.
func (m *Model) submitInput() (tea.Model, tea.Cmd) {
	line := m.cmdbar.value()
	if line == "" {
		return m, nil
	}
	m.cmdbar.reset()

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

	loop := agent.NewLoop(provider, m.models.ActiveModel(), m.registry, m.session,
		m.bus, m.confirmFn)
	go loop.Run(ctx, input)
	return m, nil
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
		m.chat.appendAgentDeltaBlock(helpText)
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
		m.openProviderPicker()
		return m, nil
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
	case "jobs", "designs", "plan", "lab", "export", "cost", "project", "skills":
		m.chat.appendAgentDeltaBlock("/" + cmd + " arrives in a later Proteus milestone.")
		return m, nil
	default:
		m.chat.appendError("unknown command: /" + cmd)
		return m, nil
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

func (m *Model) openProviderPicker() {
	seen := map[string]bool{}
	items := make([]pickerItem, 0)
	for _, mod := range m.models.Models() {
		if seen[mod.ProviderName] {
			continue
		}
		seen[mod.ProviderName] = true
		items = append(items, pickerItem{id: mod.ID, label: mod.ProviderName})
	}
	m.picker = newPicker("Select provider", items)
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

// View composes the screen.
func (m *Model) View() string {
	if m.width == 0 {
		return "starting proteus…"
	}
	var body string
	if m.width >= 100 {
		right := lipgloss.JoinVertical(lipgloss.Left,
			m.jobs.View(), "", m.designs.View())
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.chat.View(), "  ", right)
	} else {
		switch m.focus {
		case focusJobs:
			body = m.jobs.View()
		case focusDesigns:
			body = m.designs.View()
		default:
			body = m.chat.View()
		}
	}
	base := m.status.View() + "\n" +
		body + "\n" +
		m.theme.CommandBar.Render(m.cmdbar.hints) + "\n" +
		m.cmdbar.ta.View()

	switch m.overlay {
	case overlayConfirm:
		return base + "\n" + m.modal.view(m.theme, m.width)
	case overlayPicker:
		return base + "\n" + m.picker.view(m.theme, m.width)
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
	chatW := m.width
	if panelW > 0 {
		chatW = m.width - panelW - 2 // 2 spaces of gap
	}
	m.chat.resize(chatW, m.chatHeight())
}

func (m *Model) chatWidth() int { return m.width }

// chatHeight reserves rows for the status bar (1), hint line (1),
// the 3-line input, and separators.
func (m *Model) chatHeight() int {
	h := m.height - 7
	if h < 3 {
		h = 3
	}
	return h
}

const helpText = "Proteus v0.2 — type a message to talk to the agent.\n" +
	"Session: /model /provider /clear /help /quit.\n" +
	"Setup: /install /uninstall /tools /doctor /modal deploy.\n" +
	"Ctrl+C cancels the running turn · Ctrl+D quits."
