package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/safety"
	"github.com/alvarogonjim/fova/internal/tools"
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

func TestLoopAccumulatesTurnUsage(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(echoTool{})
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Input: map[string]any{"text": "x"}}},
			Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}},
		{Text: "done", StopReason: "end_turn",
			Usage: llm.Usage{InputTokens: 20, OutputTokens: 7}},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string) bool { return true })

	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	msgs := drain(bus)

	for _, m := range msgs {
		if d, ok := m.(TurnDoneMsg); ok {
			if d.Usage.InputTokens != 30 || d.Usage.OutputTokens != 12 {
				t.Fatalf("turn usage = %+v, want {InputTokens:30 OutputTokens:12}", d.Usage)
			}
			return
		}
	}
	t.Fatal("no TurnDoneMsg on the bus")
}

// hitGuard is a stub safety.Guard that always refuses with a fixed reason.
type hitGuard struct{ reason string }

func (h hitGuard) Inspect(_ string, _ json.RawMessage) (safety.Refusal, bool) {
	return safety.Refusal{ID: "test", Reason: h.reason}, true
}

// passGuard never refuses — used to verify the loop still works when a
// guard is supplied but the input is safe.
type passGuard struct{}

func (passGuard) Inspect(_ string, _ json.RawMessage) (safety.Refusal, bool) {
	return safety.Refusal{}, false
}

func TestLoopRefusesBiosecurityHit(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(echoTool{})
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Input: map[string]any{"target_sequence": "x"}}}},
		{Text: "ok", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoopWithGuard(prov, "mock", reg, NewSession("sys"), bus,
		func(string) bool { return true }, hitGuard{reason: "Select agent; banned"})

	go func() { loop.Run(context.Background(), "go"); close(bus) }()

	var refusal *ToolDoneMsg
	for msg := range bus {
		if d, ok := msg.(ToolDoneMsg); ok {
			d := d
			refusal = &d
			break
		}
	}
	if refusal == nil {
		t.Fatal("loop produced no ToolDoneMsg")
	}
	if refusal.Err == nil {
		t.Fatal("biosecurity refusal must set ToolDoneMsg.Err")
	}
	if !errors.Is(refusal.Err, ErrBiosecurity) {
		t.Errorf("ToolDoneMsg.Err = %v, want errors.Is(ErrBiosecurity)", refusal.Err)
	}
	if !strings.Contains(refusal.Display, "biosecurity") {
		t.Errorf("Display = %q, want it to mention 'biosecurity'", refusal.Display)
	}
	if !strings.Contains(refusal.Display, "Select agent; banned") {
		t.Errorf("Display = %q, want the entry's reason verbatim", refusal.Display)
	}
}

func TestLoopWithGuardPassesSafeCalls(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(echoTool{})
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Input: map[string]any{"text": "x"}}}},
		{Text: "ok", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoopWithGuard(prov, "mock", reg, NewSession("sys"), bus,
		func(string) bool { return true }, passGuard{})

	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	msgs := drain(bus)

	var sawTurnDone bool
	for _, m := range msgs {
		switch v := m.(type) {
		case ToolDoneMsg:
			if v.Err != nil {
				t.Fatalf("safe call produced an error ToolDoneMsg: %+v", v)
			}
		case TurnDoneMsg:
			_ = v
			sawTurnDone = true
		}
	}
	if !sawTurnDone {
		t.Fatal("expected the turn to complete normally")
	}
}

// concurrentFake is a Tool that opts into Concurrent and sleeps for `sleep`
// before returning `display`. Used to measure parallelism vs serialism.
type concurrentFake struct {
	name    string
	sleep   time.Duration
	display string
}

func (f concurrentFake) Name() string                              { return f.name }
func (f concurrentFake) Description() string                       { return "" }
func (f concurrentFake) InputSchema() map[string]any               { return map[string]any{"type": "object"} }
func (f concurrentFake) RequiresConfirmation(json.RawMessage) bool { return false }
func (f concurrentFake) EstimatedCostUSD(json.RawMessage) float64  { return 0 }
func (f concurrentFake) EstimatedDuration(json.RawMessage) time.Duration {
	return f.sleep
}
func (f concurrentFake) Concurrent() bool { return true }
func (f concurrentFake) Execute(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
	select {
	case <-time.After(f.sleep):
		return tools.Result{Display: f.display}, nil
	case <-ctx.Done():
		return tools.Result{}, ctx.Err()
	}
}

// serialFake is identical to concurrentFake but does NOT implement Concurrent.
type serialFake struct {
	name    string
	sleep   time.Duration
	display string
}

func (f serialFake) Name() string                              { return f.name }
func (f serialFake) Description() string                       { return "" }
func (f serialFake) InputSchema() map[string]any               { return map[string]any{"type": "object"} }
func (f serialFake) RequiresConfirmation(json.RawMessage) bool { return false }
func (f serialFake) EstimatedCostUSD(json.RawMessage) float64  { return 0 }
func (f serialFake) EstimatedDuration(json.RawMessage) time.Duration {
	return f.sleep
}
func (f serialFake) Execute(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
	select {
	case <-time.After(f.sleep):
		return tools.Result{Display: f.display}, nil
	case <-ctx.Done():
		return tools.Result{}, ctx.Err()
	}
}

func TestLoopRunsConcurrentToolsInParallel(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(concurrentFake{name: "fake.a", sleep: 100 * time.Millisecond, display: "A"})
	reg.Register(concurrentFake{name: "fake.b", sleep: 100 * time.Millisecond, display: "B"})

	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "fake.a", Input: map[string]any{}},
			{ID: "2", Name: "fake.b", Input: map[string]any{}},
		}},
		{Text: "done", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string) bool { return true })

	start := time.Now()
	done := make(chan struct{})
	go func() { loop.Run(context.Background(), "go"); close(bus); close(done) }()
	drain(bus)
	<-done
	elapsed := time.Since(start)

	// Two 100ms calls in parallel should finish in ~100ms, well under
	// the 200ms serial wall-clock. Give 80ms of slack for CI overhead.
	if elapsed > 180*time.Millisecond {
		t.Errorf("two 100ms concurrent calls took %v, expected ~100ms", elapsed)
	}
}

func TestLoopPreservesOrderingOfToolResults(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(concurrentFake{name: "fake.slow", sleep: 80 * time.Millisecond, display: "slow"})
	reg.Register(concurrentFake{name: "fake.fast", sleep: 10 * time.Millisecond, display: "fast"})

	sess := NewSession("sys")
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "fake.slow", Input: map[string]any{}},
			{ID: "2", Name: "fake.fast", Input: map[string]any{}},
		}},
		{Text: "done", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, sess, bus, func(string) bool { return true })

	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	drain(bus)

	// Pull tool messages from the session in order.
	var results []string
	for _, m := range sess.Messages() {
		if m.Role == "tool" {
			results = append(results, m.Content)
		}
	}
	want := []string{"slow", "fast"}
	if len(results) != 2 || results[0] != want[0] || results[1] != want[1] {
		t.Errorf("tool result order = %v, want %v", results, want)
	}
}

func TestLoopSerialFallbackForNonConcurrentTools(t *testing.T) {
	reg := tools.NewRegistry()
	// fake.serial is non-concurrent and sleeps 80ms; fake.par is concurrent
	// but sleeps 10ms. The two must not overlap because fake.serial does NOT
	// opt into Concurrent, so it runs in the serial bucket — wall-clock
	// should be ~90ms (sum), not ~80ms (max).
	reg.Register(serialFake{name: "fake.serial", sleep: 80 * time.Millisecond, display: "S"})
	reg.Register(concurrentFake{name: "fake.par", sleep: 10 * time.Millisecond, display: "P"})

	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "fake.serial", Input: map[string]any{}},
			{ID: "2", Name: "fake.par", Input: map[string]any{}},
		}},
		{Text: "done", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string) bool { return true })

	start := time.Now()
	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	drain(bus)
	elapsed := time.Since(start)

	// Wall-clock must be near the sum (~90ms). If the implementation
	// accidentally parallelised the serial-fake too, elapsed would
	// collapse to ~80ms (the max). Lower bound of 85ms catches that.
	if elapsed < 85*time.Millisecond {
		t.Errorf("expected serial total ~90ms, got %v (looks parallelised)", elapsed)
	}
}
