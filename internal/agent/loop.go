package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/safety"
	"github.com/alvarogonjim/fova/internal/tools"
)

// ErrBiosecurity is returned (wrapped in ToolDoneMsg.Err) when the safety
// guard refuses a tool call. Callers can distinguish a refusal from a generic
// tool failure with errors.Is.
var ErrBiosecurity = errors.New("refused by biosecurity guard")

// --- bus message types (delivered to the TUI as tea.Msg) ---

// TextDeltaMsg is a chunk of assistant text destined for the chat pane.
// It excludes any reasoning content the model emits in <think>...</think>
// blocks — that flows through ReasoningDeltaMsg instead.
type TextDeltaMsg struct{ Delta string }

// ReasoningDeltaMsg is a chunk of the model's chain-of-thought, stripped
// from the text stream. The TUI currently drops it (the spinning "Thinking…"
// indicator already signals reasoning is in progress); future builds may
// surface it in a collapsible block.
type ReasoningDeltaMsg struct{ Delta string }

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

// TurnDoneMsg signals the agent finished its turn. Usage is the turn's total
// token consumption, summed across every LLM call the turn made.
type TurnDoneMsg struct{ Usage llm.Usage }

// TurnErrorMsg reports a fatal error that ended the turn.
type TurnErrorMsg struct{ Err error }

// ConfirmRequestMsg asks the TUI to confirm a sensitive tool call.
type ConfirmRequestMsg struct{ Prompt string }

// ConfirmContextMsg precedes a ConfirmRequestMsg on the bus, carrying the tool
// name and raw input so the TUI can render a tool-specific confirmation (such
// as the rich Adaptyv submit modal) instead of the generic prompt.
type ConfirmContextMsg struct {
	Tool  string
	Input json.RawMessage
}

// Loop is the ReAct agent loop.
type Loop struct {
	provider llm.Provider
	model    string
	registry *tools.Registry
	session  *Session
	bus      chan<- tea.Msg
	confirm  func(prompt string) bool
	guard    safety.Guard // optional; nil = no inspection (used in tests)
}

// NewLoop builds an agent loop. confirm is called synchronously when a tool
// requires confirmation; the TUI implementation blocks on a modal. The
// returned loop has no biosecurity guard; production callers should use
// NewLoopWithGuard.
func NewLoop(p llm.Provider, model string, r *tools.Registry, s *Session,
	bus chan<- tea.Msg, confirm func(string) bool) *Loop {
	return NewLoopWithGuard(p, model, r, s, bus, confirm, nil)
}

// NewLoopWithGuard builds an agent loop with a content-filter guard. The
// guard is consulted on every tool call; nil disables inspection (used only
// in tests).
func NewLoopWithGuard(p llm.Provider, model string, r *tools.Registry, s *Session,
	bus chan<- tea.Msg, confirm func(string) bool, g safety.Guard) *Loop {
	return &Loop{provider: p, model: model, registry: r, session: s, bus: bus, confirm: confirm, guard: g}
}

// Run executes one user turn: it streams model output, dispatches tool calls,
// and loops until the model stops requesting tools.
func (l *Loop) Run(ctx context.Context, userInput string) {
	l.session.AddUserMessage(userInput)

	var turnUsage llm.Usage

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
		// A fresh reasoning filter per LLM call — reasoning blocks never
		// span streaming iterations.
		var rf reasoningFilter
		for ev := range events {
			switch ev.Kind {
			case "text_delta":
				vis, rea := rf.process(ev.Delta)
				if vis != "" {
					resp.Text += vis
					l.bus <- TextDeltaMsg{Delta: vis}
				}
				if rea != "" {
					l.bus <- ReasoningDeltaMsg{Delta: rea}
				}
			case "reasoning_delta":
				// Provider already separated reasoning from content (vLLM
				// reasoning_content path). No in-band filtering needed.
				l.bus <- ReasoningDeltaMsg{Delta: ev.Delta}
			case "tool_call":
				resp.ToolCalls = append(resp.ToolCalls, *ev.Call)
			case "done":
				resp.Usage = ev.Usage
				resp.StopReason = ev.StopReason
				turnUsage.InputTokens += ev.Usage.InputTokens
				turnUsage.OutputTokens += ev.Usage.OutputTokens
			case "error":
				l.bus <- TurnErrorMsg{Err: ev.Err}
				return
			}
		}
		// Flush any buffered tail the filter held back; if a "<thi" never
		// completed it gets emitted as visible text, never swallowed.
		if leftover := rf.flush(); leftover != "" {
			resp.Text += leftover
			l.bus <- TextDeltaMsg{Delta: leftover}
		}

		l.session.AddAssistantMessage(resp)

		if len(resp.ToolCalls) == 0 {
			l.bus <- TurnDoneMsg{Usage: turnUsage}
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
		l.bus <- ConfirmContextMsg{Tool: tc.Name, Input: input}
		if !l.confirm("Run " + tc.Name + "? " + string(input)) {
			l.bus <- ToolDoneMsg{Name: tc.Name, Display: "declined by user"}
			return "error: user declined to run " + tc.Name
		}
	}

	// Content-filter guard: a refusal short-circuits before any tool work.
	// The refusal text is shown to the user AND fed back to the model so it
	// stops retrying with a tweak. SPECS §20 #3.
	if l.guard != nil {
		if r, refused := l.guard.Inspect(tc.Name, input); refused {
			msg := "refused by biosecurity guard: " + r.Reason
			l.bus <- ToolDoneMsg{Name: tc.Name, Display: msg, Err: ErrBiosecurity}
			return msg
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
