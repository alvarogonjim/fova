package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alvarogonjim/fova/internal/config"
)

// WizardResult is the set of choices the onboarding wizard collected.
type WizardResult struct {
	DataDir        string
	Provider       string
	APIKeyProvider string
	APIKeyEnv      string
	APIKey         string
	Theme          string
	ComputeBackend string
	KnowledgeEmail string
	BudgetUSD      float64
}

// wizardDoneMsg is emitted when the wizard finishes (Skipped false) or is
// skipped (Skipped true).
type wizardDoneMsg struct {
	Result  WizardResult
	Skipped bool
}

type wizardStepKind int

const (
	stepInfo wizardStepKind = iota
	stepPick
	stepInput
)

// wizardChoice is one option of a stepPick step.
type wizardChoice struct{ id, label, tag string }

// wizardStep is one screen of the wizard.
type wizardStep struct {
	id       string
	kind     wizardStepKind
	numbered bool
	title    string
	body     string
	choices  []wizardChoice          // stepPick
	masked   bool                    // stepInput: mask the entry
	validate func(string) error      // stepInput: nil = always valid
	active   func(WizardResult) bool // nil = always shown
}

// wizardModel is the onboarding wizard component.
type wizardModel struct {
	theme         Theme
	width, height int
	catalog       config.Catalog

	steps  []wizardStep
	idx    int
	result WizardResult

	input   textinput.Model
	pickCur int
	errMsg  string

	finished bool
	skipped  bool
}

// newWizardModel builds the wizard. ollamaUp is the result of the local-Ollama
// probe (injected so the constructor stays offline and testable).
func newWizardModel(th Theme, cat config.Catalog, ollamaUp bool) *wizardModel {
	ti := textinput.New()
	ti.Prompt = "  "
	m := &wizardModel{
		theme:   th,
		catalog: cat,
		steps:   buildWizardSteps(cat, ollamaUp),
		input:   ti,
		result: WizardResult{
			DataDir: "~/fova", Theme: "auto", ComputeBackend: "local", BudgetUSD: 5,
		},
	}
	m.enterStep()
	return m
}

// buildWizardSteps assembles the ordered step list.
func buildWizardSteps(cat config.Catalog, ollamaUp bool) []wizardStep {
	apiKeyActive := func(r WizardResult) bool {
		for _, p := range cat.Providers {
			if p.Name == r.Provider {
				return p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) == ""
			}
		}
		return false
	}
	return []wizardStep{
		{
			id: "welcome", kind: stepInfo, numbered: false,
			title: "fova — a terminal agent for de novo protein design",
			body: "First time here. This quick setup picks your LLM provider, " +
				"where fova keeps its files, and a few defaults.\n\n" +
				"fova is free by default — a local model needs no account.",
		},
		{
			id: "folder", kind: stepInput, numbered: true,
			title: "Where should fova keep its data?",
			body:  "Projects, design files, job logs and local model caches live here.",
			validate: func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("enter a folder path")
				}
				return nil
			},
		},
		{
			id: "provider", kind: stepPick, numbered: true,
			title:   "Choose your LLM provider",
			body:    "fova works fully free with a local model. Paid providers are optional.",
			choices: providerChoices(cat, ollamaUp),
		},
		{
			id: "apikey", kind: stepInput, numbered: true, masked: true,
			title:  "API key",
			body:   "Paste a key — fova stores it in your OS keychain, never in a plain file. Ctrl+S to set it up later.",
			active: apiKeyActive,
		},
		{
			id: "theme", kind: stepPick, numbered: true,
			title: "Colour theme",
			body:  "fova adapts to your terminal, or you can force light or dark.",
			choices: []wizardChoice{
				{id: "auto", label: "Auto", tag: "match the terminal"},
				{id: "dark", label: "Dark", tag: ""},
				{id: "light", label: "Light", tag: ""},
			},
		},
		{
			id: "compute", kind: stepPick, numbered: true,
			title: "Compute backend",
			body:  "Where design jobs run. Local uses uv-managed GPU tools (/install, /doctor); Modal needs the Modal CLI and /modal deploy.",
			choices: []wizardChoice{
				{id: "local", label: "Local", tag: "uv-managed GPU tools"},
				{id: "modal", label: "Modal", tag: "bring-your-own cloud GPU"},
			},
		},
		{
			id: "email", kind: stepInput, numbered: true,
			title: "Knowledge email (optional)",
			body:  "Used for the OpenAlex polite pool — it improves literature-API rate limits. Leave blank to skip.",
			validate: func(s string) error {
				s = strings.TrimSpace(s)
				if s == "" {
					return nil
				}
				if strings.ContainsAny(s, " \t") || !strings.Contains(s, "@") || !strings.Contains(s, ".") {
					return fmt.Errorf("that does not look like an email")
				}
				return nil
			},
		},
		{
			id: "budget", kind: stepInput, numbered: true,
			title: "Session budget",
			body:  "The per-session paid-LLM soft limit, in USD, that triggers a warning.",
			validate: func(s string) error {
				v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
				if err != nil || v < 0 {
					return fmt.Errorf("enter a number greater than or equal to 0")
				}
				return nil
			},
		},
		{
			id: "summary", kind: stepInfo, numbered: true,
			title: "All set — review and confirm",
			body:  "Enter writes config.toml to ~/.config/fova/ and opens fova.",
		},
	}
}

// providerChoices maps the catalog's providers to pick choices.
func providerChoices(cat config.Catalog, ollamaUp bool) []wizardChoice {
	out := make([]wizardChoice, 0, len(cat.Providers))
	for _, p := range cat.Providers {
		tag := "free · local · no account"
		if p.APIKeyEnv != "" {
			tag = "paid · needs an API key"
		}
		if p.Name == "ollama" && ollamaUp {
			tag += " · detected"
		}
		out = append(out, wizardChoice{id: p.Name, label: capitalize(p.Name), tag: tag})
	}
	return out
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (m *wizardModel) Init() tea.Cmd { return textinput.Blink }

// gotoStep jumps directly to the step with the given id (used by tests and by
// the summary's "back" navigation). It re-runs enterStep.
func (m *wizardModel) gotoStep(id string) {
	for i, s := range m.steps {
		if s.id == id {
			m.idx = i
			m.enterStep()
			return
		}
	}
}

// enterStep initializes the per-step widget for the current step.
func (m *wizardModel) enterStep() {
	m.errMsg = ""
	step := m.steps[m.idx]
	switch step.kind {
	case stepInput:
		m.input.EchoMode = textinput.EchoNormal
		if step.masked {
			m.input.EchoMode = textinput.EchoPassword
		}
		m.input.SetValue(m.inputDefault(step.id))
		m.input.CursorEnd()
		m.input.Focus()
	case stepPick:
		m.pickCur = m.pickIndex(step)
	}
}

// inputDefault returns the pre-filled value for an input step.
func (m *wizardModel) inputDefault(id string) string {
	switch id {
	case "folder":
		return m.result.DataDir
	case "email":
		return m.result.KnowledgeEmail
	case "budget":
		return strconv.FormatFloat(m.result.BudgetUSD, 'f', -1, 64)
	default: // apikey
		return ""
	}
}

// pickIndex returns the choice index matching the already-collected value.
func (m *wizardModel) pickIndex(step wizardStep) int {
	var want string
	switch step.id {
	case "provider":
		want = m.result.Provider
	case "theme":
		want = m.result.Theme
	case "compute":
		want = m.result.ComputeBackend
	}
	for i, c := range step.choices {
		if c.id == want {
			return i
		}
	}
	return 0
}

// visible reports whether a step is shown given the collected result.
func (m *wizardModel) visible(s wizardStep) bool {
	return s.active == nil || s.active(m.result)
}

// advance moves to the next visible step, or finishes on the last one.
func (m *wizardModel) advance() tea.Cmd {
	for i := m.idx + 1; i < len(m.steps); i++ {
		if m.visible(m.steps[i]) {
			m.idx = i
			m.enterStep()
			return nil
		}
	}
	m.finished = true
	return m.done()
}

// back moves to the previous visible step.
func (m *wizardModel) back() {
	for i := m.idx - 1; i >= 0; i-- {
		if m.visible(m.steps[i]) {
			m.idx = i
			m.enterStep()
			return
		}
	}
}

// done emits the terminal wizardDoneMsg.
func (m *wizardModel) done() tea.Cmd {
	res, skipped := m.result, m.skipped
	return func() tea.Msg { return wizardDoneMsg{Result: res, Skipped: skipped} }
}

// commit validates and stores the current step's value into the result.
// It returns false (with m.errMsg set) when validation fails.
func (m *wizardModel) commit() bool {
	step := m.steps[m.idx]
	switch step.kind {
	case stepInput:
		val := m.input.Value()
		if step.validate != nil {
			if err := step.validate(val); err != nil {
				m.errMsg = err.Error()
				return false
			}
		}
		m.storeInput(step.id, val)
	case stepPick:
		if len(step.choices) > 0 {
			m.storePick(step.id, step.choices[m.pickCur].id)
		}
	}
	return true
}

func (m *wizardModel) storeInput(id, val string) {
	val = strings.TrimSpace(val)
	switch id {
	case "folder":
		m.result.DataDir = val
	case "apikey":
		m.result.APIKey = val
		m.result.APIKeyProvider = m.result.Provider
		m.result.APIKeyEnv = m.providerEnv(m.result.Provider)
	case "email":
		m.result.KnowledgeEmail = val
	case "budget":
		m.result.BudgetUSD, _ = strconv.ParseFloat(val, 64)
	}
}

func (m *wizardModel) storePick(id, choice string) {
	switch id {
	case "provider":
		m.result.Provider = choice
	case "theme":
		m.result.Theme = choice
		ApplyTheme(choice) // apply live so the change is visible immediately
	case "compute":
		m.result.ComputeBackend = choice
	}
}

// providerEnv returns a provider's API-key environment variable name.
func (m *wizardModel) providerEnv(name string) string {
	for _, p := range m.catalog.Providers {
		if p.Name == name {
			return p.APIKeyEnv
		}
	}
	return ""
}

func (m *wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	if m.steps[m.idx].kind == stepInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *wizardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	step := m.steps[m.idx]
	switch msg.Type {
	case tea.KeyEsc:
		m.skipped = true
		return m, m.done()
	case tea.KeyCtrlC:
		m.skipped = true
		return m, m.done()
	case tea.KeyShiftTab:
		m.back()
		return m, nil
	case tea.KeyCtrlS:
		if step.id == "apikey" { // defer: leave the key empty
			return m, m.advance()
		}
	case tea.KeyEnter:
		if m.commit() {
			return m, m.advance()
		}
		return m, nil
	case tea.KeyUp:
		if step.kind == stepPick && m.pickCur > 0 {
			m.pickCur--
		}
		return m, nil
	case tea.KeyDown:
		if step.kind == stepPick && m.pickCur < len(step.choices)-1 {
			m.pickCur++
		}
		return m, nil
	}
	if step.kind == stepInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// counter renders "step N of M" over the visible, numbered steps.
func (m *wizardModel) counter() string {
	total, pos := 0, 0
	for i, s := range m.steps {
		if !s.numbered || !m.visible(s) {
			continue
		}
		total++
		if i <= m.idx {
			pos++
		}
	}
	return fmt.Sprintf("step %d of %d", pos, total)
}

func (m *wizardModel) View() string {
	step := m.steps[m.idx]
	var b strings.Builder

	if step.numbered {
		b.WriteString(m.theme.Header.Render("fova setup"))
		b.WriteString("   " + m.theme.Muted.Render(m.counter()) + "\n\n")
	} else {
		b.WriteString(m.theme.Header.Render("fova — first-run setup") + "\n\n")
	}

	b.WriteString(m.theme.AgentText.Render(step.title) + "\n\n")
	b.WriteString(m.theme.Muted.Render(step.body) + "\n\n")

	switch step.kind {
	case stepPick:
		for i, c := range step.choices {
			row := "  " + c.label
			if c.tag != "" {
				row += "  " + m.theme.Subtle.Render(c.tag)
			}
			if i == m.pickCur {
				row = m.theme.PickerSel.Render("▸ " + c.label)
				if c.tag != "" {
					row += "  " + m.theme.Subtle.Render(c.tag)
				}
			}
			b.WriteString(row + "\n")
		}
	case stepInput:
		b.WriteString(m.input.View() + "\n")
	case stepInfo:
		if step.id == "summary" {
			b.WriteString(m.summaryView())
		}
	}

	if m.errMsg != "" {
		b.WriteString("\n" + m.theme.Error.Render("✗ "+m.errMsg) + "\n")
	}
	b.WriteString("\n" + m.theme.Subtle.Render(m.footer(step)))

	body := b.String()
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
	}
	return body
}

// summaryView renders the collected choices.
func (m *wizardModel) summaryView() string {
	r := m.result
	key := "not set"
	if r.APIKey != "" {
		key = "saved to keychain"
	}
	rows := []string{
		fmt.Sprintf("  data folder   %s", r.DataDir),
		fmt.Sprintf("  provider      %s", orDash(r.Provider)),
		fmt.Sprintf("  api key       %s", key),
		fmt.Sprintf("  theme         %s", r.Theme),
		fmt.Sprintf("  compute       %s", r.ComputeBackend),
		fmt.Sprintf("  email         %s", orDash(r.KnowledgeEmail)),
		fmt.Sprintf("  budget        $%.2f per session", r.BudgetUSD),
	}
	return m.theme.AgentText.Render(strings.Join(rows, "\n")) + "\n"
}

// footer renders the per-step key hints.
func (m *wizardModel) footer(step wizardStep) string {
	parts := []string{"esc skip setup"}
	if m.idx > 0 {
		parts = append([]string{"shift+tab back"}, parts...)
	}
	switch {
	case step.id == "summary":
		parts = append([]string{"enter finish"}, parts...)
	case step.id == "apikey":
		parts = append([]string{"enter store & next", "ctrl+s set up later"}, parts...)
	default:
		parts = append([]string{"enter next"}, parts...)
	}
	return strings.Join(parts, "  ·  ")
}
