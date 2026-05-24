package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sync/errgroup"

	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/safety"
	"github.com/alvarogonjim/fova/internal/tools"
)

// ErrBiosecurity is returned (wrapped in ToolDoneMsg.Err) when the safety
// guard refuses a tool call. Callers can distinguish a refusal from a generic
// tool failure with errors.Is.
var ErrBiosecurity = errors.New("refused by biosecurity guard")

// ErrMaxIterations is returned (wrapped in TurnErrorMsg.Err) when a turn
// exceeds Loop.maxIterations LLM round-trips.
var ErrMaxIterations = errors.New("turn exceeded maximum tool-call iterations")

// ErrModelTruncated is returned (wrapped in TurnErrorMsg.Err) when the
// model finished mid-output (max_tokens, length, content_filter). Acting
// on a turn that was cut short risks dispatching malformed tool calls.
var ErrModelTruncated = errors.New("model output truncated; consider raising MaxTokens or simplifying the prompt")

// defaultMaxTokens caps a single LLM response. Mirrors Anthropic's existing
// per-provider default to OpenAI/vLLM so server-side defaults (which can be
// 16k or unbounded) don't allow a runaway response.
const defaultMaxTokens = 4096

// defaultTemperature is fova's tool-use default — low enough to keep tool
// invocations deterministic, high enough that planning/brainstorming turns
// are not sterile. Callers may override via ChatRequest.Temperature.
const defaultTemperature = 0.2

// defaultMaxIterations bounds one turn at 25 LLM round-trips. A well-formed
// turn finishes in 2-6 (plan → call tools → answer). Spinning past 25 is
// almost always a model-confusion loop.
const defaultMaxIterations = 25

// maxModelPayloadBytes caps the size of structured tool Output sent to the
// model. Above this threshold the model sees the human-readable Display
// summary instead, to avoid crowding out conversation history. 8KB fits
// every list-style result currently in fova while keeping corpus_map /
// web_fetch dumps summarized.
const maxModelPayloadBytes = 8 * 1024

// modelPayload picks what the model sees from a tool result. Prefers the
// structured Output when present and within the size budget; otherwise
// falls back to the human-readable Display.
func modelPayload(r tools.Result) string {
	if len(r.Output) > 0 && len(r.Output) <= maxModelPayloadBytes {
		return string(r.Output)
	}
	return r.Display
}

// stopMeansTruncated is true when the model finished because it ran out of
// output tokens or was content-filtered, NOT because it voluntarily stopped.
// Anthropic uses "max_tokens"; OpenAI uses "length"; both/either may use
// "content_filter" / "content-filter".
func stopMeansTruncated(stop string) bool {
	switch stop {
	case "max_tokens", "length", "content_filter", "content-filter":
		return true
	}
	return false
}

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

// ToolStartMsg announces a tool call is about to run. ID is the tool-call's
// unique identifier (from the LLM response); chat trace matching uses it so
// concurrently-running tools don't collide.
type ToolStartMsg struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolDoneMsg reports a finished tool call. ID matches its ToolStartMsg.
type ToolDoneMsg struct {
	ID      string
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

// ConfirmFunc is the agent-loop's bridge to the user-facing confirmation
// surface. It is called synchronously when a tool requires confirmation; the
// TUI implementation blocks on a modal and may let the user edit the proposed
// input before approval.
//
// Returns (accepted, finalInput). finalInput == nil means "user accepted
// without editing — submit the original bytes"; a non-nil finalInput means
// "user edited — submit these bytes instead". The original input is also
// passed through ConfirmContextMsg on the bus, so the implementation can
// render a tool-specific review without re-parsing the prompt string.
type ConfirmFunc func(prompt, name string, input json.RawMessage) (accepted bool, finalInput json.RawMessage)

// Loop is the ReAct agent loop.
type Loop struct {
	provider      llm.Provider
	model         string
	registry      *tools.Registry
	session       *Session
	bus           chan<- tea.Msg
	confirm       ConfirmFunc
	guard         safety.Guard // optional; nil = no inspection (used in tests)
	maxIterations int
}

// NewLoop builds an agent loop. confirm is called synchronously when a tool
// requires confirmation; the TUI implementation blocks on a modal. The
// returned loop has no biosecurity guard; production callers should use
// NewLoopWithGuard.
func NewLoop(p llm.Provider, model string, r *tools.Registry, s *Session,
	bus chan<- tea.Msg, confirm ConfirmFunc) *Loop {
	return NewLoopWithGuard(p, model, r, s, bus, confirm, nil)
}

// NewLoopWithGuard builds an agent loop with a content-filter guard. The
// guard is consulted on every tool call; nil disables inspection (used only
// in tests).
func NewLoopWithGuard(p llm.Provider, model string, r *tools.Registry, s *Session,
	bus chan<- tea.Msg, confirm ConfirmFunc, g safety.Guard) *Loop {
	return &Loop{
		provider:      p,
		model:         model,
		registry:      r,
		session:       s,
		bus:           bus,
		confirm:       confirm,
		guard:         g,
		maxIterations: defaultMaxIterations,
	}
}

// SetMaxIterations overrides the per-turn iteration cap. Test-only;
// production callers use the default.
func (l *Loop) SetMaxIterations(n int) { l.maxIterations = n }

// Run executes one user turn: it streams model output, dispatches tool calls,
// and loops until the model stops requesting tools.
func (l *Loop) Run(ctx context.Context, userInput string) {
	l.session.AddUserMessage(userInput)

	var turnUsage llm.Usage
	iterations := 0

	for {
		if iterations >= l.maxIterations {
			l.bus <- TurnErrorMsg{Err: fmt.Errorf("%w (%d)", ErrMaxIterations, l.maxIterations)}
			return
		}
		iterations++

		if err := ctx.Err(); err != nil {
			l.bus <- TurnErrorMsg{Err: err}
			return
		}

		req := llm.ChatRequest{
			Model:       l.model,
			System:      l.session.SystemPrompt(),
			Messages:    l.session.Messages(),
			Tools:       l.registry.Specs(),
			Temperature: defaultTemperature,
			MaxTokens:   defaultMaxTokens,
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

		if stopMeansTruncated(resp.StopReason) {
			l.bus <- TurnErrorMsg{Err: fmt.Errorf("%w (stop_reason=%q)", ErrModelTruncated, resp.StopReason)}
			return
		}

		if len(resp.ToolCalls) == 0 {
			l.bus <- TurnDoneMsg{Usage: turnUsage}
			return
		}

		results := l.executeBatch(ctx, resp.ToolCalls)
		for i, tc := range resp.ToolCalls {
			l.session.AddToolResult(tc.ID, results[i])
		}
	}
}

// executeBatch dispatches one assistant turn's batched tool calls. Tools that
// implement tools.Concurrent and do not require confirmation run in parallel
// via errgroup; everything else runs serially. Results are returned in the
// original tool-call order so the model sees a stable sequence regardless of
// completion order.
func (l *Loop) executeBatch(ctx context.Context, calls []llm.ToolCall) []string {
	results := make([]string, len(calls))
	concurrentIdx := make([]int, 0, len(calls))
	serialIdx := make([]int, 0, len(calls))

	for i, tc := range calls {
		t, ok := l.registry.Get(tc.Name)
		if !ok {
			// Unknown tool: executeTool reports the error message itself.
			serialIdx = append(serialIdx, i)
			continue
		}
		// json.Marshal on map[string]any never returns an error for well-formed
		// inputs; the error is ignored here as elsewhere in this file.
		input, _ := json.Marshal(tc.Input)
		if tools.IsConcurrent(t) && !t.RequiresConfirmation(input) {
			concurrentIdx = append(concurrentIdx, i)
		} else {
			serialIdx = append(serialIdx, i)
		}
	}

	if len(concurrentIdx) > 0 {
		g, gctx := errgroup.WithContext(ctx)
		for _, idx := range concurrentIdx {
			idx := idx
			tc := calls[idx]
			g.Go(func() error {
				results[idx] = l.executeTool(gctx, tc)
				return nil
			})
		}
		_ = g.Wait() // errors are already surfaced via ToolDoneMsg / display text
	}

	for _, idx := range serialIdx {
		results[idx] = l.executeTool(ctx, calls[idx])
	}

	return results
}

// executeTool dispatches one tool call and returns the result text the model
// will see. Errors are returned as text so the model can recover.
func (l *Loop) executeTool(ctx context.Context, tc llm.ToolCall) string {
	input, _ := json.Marshal(tc.Input)
	l.bus <- ToolStartMsg{ID: tc.ID, Name: tc.Name, Input: input}

	tool, ok := l.registry.Get(tc.Name)
	if !ok {
		msg := "error: unknown tool " + tc.Name
		l.bus <- ToolDoneMsg{ID: tc.ID, Name: tc.Name, Display: msg, Err: fmt.Errorf("unknown tool %q", tc.Name)}
		return msg
	}
	if err := ctx.Err(); err != nil {
		l.bus <- ToolDoneMsg{ID: tc.ID, Name: tc.Name, Display: "cancelled", Err: err}
		return "error: cancelled by user"
	}
	if tool.RequiresConfirmation(input) {
		l.bus <- ConfirmContextMsg{Tool: tc.Name, Input: input}
		accepted, edited := l.confirm("Run "+tc.Name+"?", tc.Name, input)
		if !accepted {
			l.bus <- ToolDoneMsg{ID: tc.ID, Name: tc.Name, Display: "declined by user"}
			return "error: user declined to run " + tc.Name
		}
		if len(edited) > 0 {
			input = edited
		}
	}

	// Content-filter guard: a refusal short-circuits before any tool work.
	// The refusal text is shown to the user AND fed back to the model so it
	// stops retrying with a tweak. SPECS §20 #3.
	if l.guard != nil {
		if r, refused := l.guard.Inspect(tc.Name, input); refused {
			msg := "refused by biosecurity guard: " + r.Reason
			l.bus <- ToolDoneMsg{ID: tc.ID, Name: tc.Name, Display: msg, Err: ErrBiosecurity}
			return msg
		}
	}

	res, err := l.registry.Execute(ctx, tc.Name, input)
	if err != nil {
		msg := "error: " + err.Error()
		l.bus <- ToolDoneMsg{ID: tc.ID, Name: tc.Name, Display: msg, Err: err}
		return msg
	}
	payload := modelPayload(res)
	l.bus <- ToolDoneMsg{ID: tc.ID, Name: tc.Name, Display: res.Display}
	return payload
}
