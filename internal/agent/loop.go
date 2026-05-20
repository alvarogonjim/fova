package agent

import (
	"context"
	"encoding/json"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// --- bus message types (delivered to the TUI as tea.Msg) ---

// TextDeltaMsg is a chunk of assistant text.
type TextDeltaMsg struct{ Delta string }

// ToolStartMsg announces a tool call is about to run.
type ToolStartMsg struct {
	Name  string
	Input json.RawMessage
}

// ToolDoneMsg reports a finished tool call.
type ToolDoneMsg struct {
	Name    string
	Display string
	Err     error
}

// TurnDoneMsg signals the agent finished its turn.
type TurnDoneMsg struct{}

// TurnErrorMsg reports a fatal error that ended the turn.
type TurnErrorMsg struct{ Err error }

// ConfirmRequestMsg asks the TUI to confirm a sensitive tool call.
type ConfirmRequestMsg struct{ Prompt string }

// Loop is the ReAct agent loop.
type Loop struct {
	provider llm.Provider
	model    string
	registry *tools.Registry
	session  *Session
	bus      chan<- tea.Msg
	confirm  func(prompt string) bool
}

// NewLoop builds an agent loop. confirm is called synchronously when a tool
// requires confirmation; the TUI implementation blocks on a modal.
func NewLoop(p llm.Provider, model string, r *tools.Registry, s *Session,
	bus chan<- tea.Msg, confirm func(string) bool) *Loop {
	return &Loop{provider: p, model: model, registry: r, session: s, bus: bus, confirm: confirm}
}

// Run executes one user turn: it streams model output, dispatches tool calls,
// and loops until the model stops requesting tools.
func (l *Loop) Run(ctx context.Context, userInput string) {
	l.session.AddUserMessage(userInput)

	for {
		if err := ctx.Err(); err != nil {
			l.bus <- TurnErrorMsg{Err: err}
			return
		}

		req := llm.ChatRequest{
			Model:    l.model,
			System:   l.session.SystemPrompt(),
			Messages: l.session.Messages(),
			Tools:    l.registry.Specs(),
		}
		events, err := l.provider.StreamChat(ctx, req)
		if err != nil {
			l.bus <- TurnErrorMsg{Err: err}
			return
		}

		var resp llm.ChatResponse
		for ev := range events {
			switch ev.Kind {
			case "text_delta":
				resp.Text += ev.Delta
				l.bus <- TextDeltaMsg{Delta: ev.Delta}
			case "tool_call":
				resp.ToolCalls = append(resp.ToolCalls, *ev.Call)
			case "done":
				resp.Usage = ev.Usage
				resp.StopReason = ev.StopReason
			case "error":
				l.bus <- TurnErrorMsg{Err: ev.Err}
				return
			}
		}

		l.session.AddAssistantMessage(resp)

		if len(resp.ToolCalls) == 0 {
			l.bus <- TurnDoneMsg{}
			return
		}

		for _, tc := range resp.ToolCalls {
			display := l.executeTool(ctx, tc)
			l.session.AddToolResult(tc.ID, display)
		}
	}
}

// executeTool dispatches one tool call and returns the result text the model
// will see. Errors are returned as text so the model can recover.
func (l *Loop) executeTool(ctx context.Context, tc llm.ToolCall) string {
	input, _ := json.Marshal(tc.Input)
	l.bus <- ToolStartMsg{Name: tc.Name, Input: input}

	tool, ok := l.registry.Get(tc.Name)
	if !ok {
		msg := "error: unknown tool " + tc.Name
		l.bus <- ToolDoneMsg{Name: tc.Name, Display: msg, Err: fmt.Errorf("unknown tool %q", tc.Name)}
		return msg
	}
	if err := ctx.Err(); err != nil {
		l.bus <- ToolDoneMsg{Name: tc.Name, Display: "cancelled", Err: err}
		return "error: cancelled by user"
	}
	if tool.RequiresConfirmation(input) {
		if !l.confirm("Run " + tc.Name + "? " + string(input)) {
			l.bus <- ToolDoneMsg{Name: tc.Name, Display: "declined by user"}
			return "error: user declined to run " + tc.Name
		}
	}

	res, err := l.registry.Execute(ctx, tc.Name, input)
	if err != nil {
		msg := "error: " + err.Error()
		l.bus <- ToolDoneMsg{Name: tc.Name, Display: msg, Err: err}
		return msg
	}
	l.bus <- ToolDoneMsg{Name: tc.Name, Display: res.Display}
	return res.Display
}
