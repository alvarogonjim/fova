package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// drain collects all bus messages until the channel is closed.
func drain(bus chan tea.Msg) []tea.Msg {
	var out []tea.Msg
	for m := range bus {
		out = append(out, m)
	}
	return out
}

func TestLoopTextOnlyTurn(t *testing.T) {
	prov := &mockProvider{responses: []llm.ChatResponse{
		{Text: "Hello.", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", tools.NewRegistry(),
		NewSession("sys"), bus, func(string) bool { return true })

	go func() { loop.Run(context.Background(), "hi"); close(bus) }()
	msgs := drain(bus)

	var gotText, gotDone bool
	for _, m := range msgs {
		switch v := m.(type) {
		case TextDeltaMsg:
			if v.Delta == "Hello." {
				gotText = true
			}
		case TurnDoneMsg:
			gotDone = true
		}
	}
	if !gotText || !gotDone {
		t.Fatalf("text=%v done=%v", gotText, gotDone)
	}
}

func TestLoopExecutesToolThenFinishes(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(echoTool{})
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Input: map[string]any{"text": "x"}}}},
		{Text: "done", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string) bool { return true })

	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	msgs := drain(bus)

	var toolStarted, toolDone, turnDone bool
	for _, m := range msgs {
		switch m.(type) {
		case ToolStartMsg:
			toolStarted = true
		case ToolDoneMsg:
			toolDone = true
		case TurnDoneMsg:
			turnDone = true
		}
	}
	if !toolStarted || !toolDone || !turnDone {
		t.Fatalf("toolStarted=%v toolDone=%v turnDone=%v", toolStarted, toolDone, turnDone)
	}
	if prov.calls != 2 {
		t.Fatalf("provider called %d times, want 2", prov.calls)
	}
}

// blockingTool blocks in Execute until the context is cancelled.
type blockingTool struct{}

func (blockingTool) Name() string                                    { return "block" }
func (blockingTool) Description() string                             { return "blocks until cancelled" }
func (blockingTool) InputSchema() map[string]any                     { return map[string]any{"type": "object"} }
func (blockingTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (blockingTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (blockingTool) EstimatedDuration(json.RawMessage) time.Duration { return time.Hour }
func (blockingTool) Execute(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
	<-ctx.Done()
	return tools.Result{}, ctx.Err()
}

func TestLoopCancellationMidTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(blockingTool{})
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "block", Input: map[string]any{}}}},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string) bool { return true })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // cleanup; the mid-tool cancel below is the meaningful one
	go func() { loop.Run(ctx, "go"); close(bus) }()

	var sawToolStart, sawTurnError bool
	for msg := range bus {
		switch msg.(type) {
		case ToolStartMsg:
			sawToolStart = true
			cancel() // cancel while the tool is blocked inside Execute
		case TurnErrorMsg:
			sawTurnError = true
		}
	}
	if !sawToolStart {
		t.Fatal("tool never started")
	}
	if !sawTurnError {
		t.Fatal("cancellation mid-tool did not produce TurnErrorMsg")
	}
}

// echoTool is a trivial tool used by the loop test.
type echoTool struct{}

func (echoTool) Name() string                                    { return "echo" }
func (echoTool) Description() string                             { return "echo" }
func (echoTool) InputSchema() map[string]any                     { return map[string]any{"type": "object"} }
func (echoTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (echoTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (echoTool) EstimatedDuration(json.RawMessage) time.Duration { return time.Millisecond }
func (echoTool) Execute(_ context.Context, in json.RawMessage) (tools.Result, error) {
	return tools.Result{Display: "echoed " + string(in)}, nil
}
