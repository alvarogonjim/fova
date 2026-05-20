package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/proteus/internal/agent"
	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// overlay identifies the active modal overlay, if any.
type overlay int

const (
	overlayNone overlay = iota
	overlayConfirm
	overlayPicker
)

// Model is the root Bubble Tea model.
type Model struct {
	width, height int

	theme  Theme
	chat   *chatModel
	status statusBarModel
	cmdbar commandBarModel

	registry     *tools.Registry
	models       *llm.ModelRegistry
	systemPrompt string
	session      *agent.Session // one session for the whole TUI lifetime

	bus       chan tea.Msg // agent → TUI
	confirmCh chan bool    // TUI → agent (modal result)

	turnCancel context.CancelFunc
	running    bool

	overlay overlay
	modal   modalModel
	picker  *pickerModel
}

// New builds the root model.
func New(reg *tools.Registry, models *llm.ModelRegistry, systemPrompt string) *Model {
	th := NewTheme()
	m := &Model{
		theme:        th,
		chat:         newChatModel(th, 80, 20),
		status:       newStatusBarModel(th),
		cmdbar:       newCommandBarModel(th, 80),
		registry:     reg,
		models:       models,
		systemPrompt: systemPrompt,
		session:      agent.NewSession(systemPrompt),
		bus:          make(chan tea.Msg, 256),
		confirmCh:    make(chan bool, 1),
	}
	m.status.model = models.ActiveModel()
	m.status.provider = models.ActiveProviderName()
	return m
}

// Init starts listening on the agent bus.
func (m *Model) Init() tea.Cmd {
	return m.waitForBus()
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
	base := m.status.View() + "\n" +
		m.chat.View() + "\n" +
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
	m.chat.resize(m.chatWidth(), m.chatHeight())
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

const helpText = "Proteus v0.1 — type a message to talk to the agent.\n" +
	"Slash commands: /model /provider /clear /help /quit.\n" +
	"Ctrl+C cancels the running turn · Ctrl+D quits."
