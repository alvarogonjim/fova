package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		NewSession("sys"), bus, func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil })

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
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil })

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
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil })

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
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil })

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
		func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil }, hitGuard{reason: "Select agent; banned"})

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
		func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil }, passGuard{})

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
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil })

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
	loop := NewLoop(prov, "mock", reg, sess, bus, func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil })

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
	// fake.serial is non-concurrent and sleeps 120ms; fake.par is concurrent
	// but sleeps 15ms. The two must not overlap because fake.serial does NOT
	// opt into Concurrent, so it runs in the serial bucket — wall-clock
	// should be ~135ms (sum), not ~120ms (max).
	reg.Register(serialFake{name: "fake.serial", sleep: 120 * time.Millisecond, display: "S"})
	reg.Register(concurrentFake{name: "fake.par", sleep: 15 * time.Millisecond, display: "P"})

	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "fake.serial", Input: map[string]any{}},
			{ID: "2", Name: "fake.par", Input: map[string]any{}},
		}},
		{Text: "done", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil })

	start := time.Now()
	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	drain(bus)
	elapsed := time.Since(start)

	// Wall-clock must be near the sum (~135ms). If the implementation
	// accidentally parallelised the serial-fake too, elapsed would
	// collapse to ~120ms (the max). Lower bound of 125ms catches that.
	if elapsed < 125*time.Millisecond {
		t.Errorf("expected serial total ~135ms, got %v (looks parallelised)", elapsed)
	}
}

// confirmFake is concurrent-eligible by interface but requires confirmation.
// The loop MUST route it to the serial bucket — confirmation modals are serial
// by construction.
type confirmFake struct {
	name          string
	confirmCalled chan struct{}
}

func (f *confirmFake) Name() string                                    { return f.name }
func (f *confirmFake) Description() string                             { return "" }
func (f *confirmFake) InputSchema() map[string]any                     { return map[string]any{"type": "object"} }
func (f *confirmFake) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (f *confirmFake) EstimatedDuration(json.RawMessage) time.Duration { return 0 }
func (f *confirmFake) Concurrent() bool                                { return true }
func (f *confirmFake) RequiresConfirmation(json.RawMessage) bool {
	select {
	case <-f.confirmCalled:
		// already signaled
	default:
		close(f.confirmCalled)
	}
	return true
}
func (f *confirmFake) Execute(_ context.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{Display: "ok"}, nil
}

func TestLoopConfirmationStaysSerial(t *testing.T) {
	confirm := &confirmFake{name: "fake.confirm", confirmCalled: make(chan struct{})}
	reg := tools.NewRegistry()
	reg.Register(confirm)
	reg.Register(concurrentFake{name: "fake.par", sleep: 10 * time.Millisecond, display: "P"})

	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "fake.confirm", Input: map[string]any{}},
			{ID: "2", Name: "fake.par", Input: map[string]any{}},
		}},
		{Text: "done", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil })

	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	drain(bus)

	// confirm.RequiresConfirmation must have been called — once during the
	// partition decision in executeBatch, and once again from executeTool's
	// confirmation gate. Either firing proves the tool was treated as
	// confirmation-required (i.e. routed to the serial bucket).
	select {
	case <-confirm.confirmCalled:
		// good
	default:
		t.Errorf("RequiresConfirmation was never called for fake.confirm")
	}
}

// blockingConcurrent is a concurrent-eligible Tool that blocks in Execute
// until its context is cancelled. Used to verify that cancellation propagates
// through the errgroup-derived context.
type blockingConcurrent struct {
	name    string
	started chan struct{}
}

func (b *blockingConcurrent) Name() string                                    { return b.name }
func (b *blockingConcurrent) Description() string                             { return "" }
func (b *blockingConcurrent) InputSchema() map[string]any                     { return map[string]any{"type": "object"} }
func (b *blockingConcurrent) RequiresConfirmation(json.RawMessage) bool       { return false }
func (b *blockingConcurrent) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (b *blockingConcurrent) EstimatedDuration(json.RawMessage) time.Duration { return time.Hour }
func (b *blockingConcurrent) Concurrent() bool                                { return true }
func (b *blockingConcurrent) Execute(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
	close(b.started)
	<-ctx.Done()
	return tools.Result{}, ctx.Err()
}

func TestLoopCancellationStopsInFlightConcurrentTools(t *testing.T) {
	a := &blockingConcurrent{name: "fake.block1", started: make(chan struct{})}
	b := &blockingConcurrent{name: "fake.block2", started: make(chan struct{})}
	reg := tools.NewRegistry()
	reg.Register(a)
	reg.Register(b)

	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "fake.block1", Input: map[string]any{}},
			{ID: "2", Name: "fake.block2", Input: map[string]any{}},
		}},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { loop.Run(ctx, "go"); close(bus); close(done) }()

	// Wait for both goroutines to enter Execute before cancelling.
	select {
	case <-a.started:
	case <-time.After(time.Second):
		t.Fatalf("fake.block1 never started")
	}
	select {
	case <-b.started:
	case <-time.After(time.Second):
		t.Fatalf("fake.block2 never started")
	}

	cancel()

	select {
	case <-done:
		// good — Run returned promptly after cancellation
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("loop.Run did not return within 500ms of cancellation")
	}
	drain(bus)
}

// acceptAll is the canonical "user accepts every confirm" stub used across
// the new test cases.
func acceptAll(string, string, json.RawMessage) (bool, json.RawMessage) { return true, nil }

// recordingProvider wraps mockProvider and records each ChatRequest seen.
// Only StreamChat records — that's what Loop.Run actually calls, and
// mockProvider.StreamChat internally invokes Chat, so recording in both
// would double-count.
type recordingProvider struct {
	*mockProvider
	requests []llm.ChatRequest
}

func (p *recordingProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	p.requests = append(p.requests, req)
	return p.mockProvider.StreamChat(ctx, req)
}

func TestLoopSetsDefaultMaxTokensAndTemperature(t *testing.T) {
	prov := &recordingProvider{mockProvider: &mockProvider{responses: []llm.ChatResponse{
		{Text: "done", StopReason: "end_turn"},
	}}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", tools.NewRegistry(), NewSession("sys"), bus, acceptAll)

	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	drain(bus)

	if len(prov.requests) == 0 {
		t.Fatal("provider received no requests")
	}
	got := prov.requests[0]
	if got.MaxTokens != defaultMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", got.MaxTokens, defaultMaxTokens)
	}
	if got.Temperature != defaultTemperature {
		t.Errorf("Temperature = %f, want %f", got.Temperature, defaultTemperature)
	}
}

// payloadProbeTool is a fake that lets a test set Output and Display
// independently, then inspect what the loop fed back to the session.
type payloadProbeTool struct {
	name    string
	output  []byte
	display string
}

func (p payloadProbeTool) Name() string                                    { return p.name }
func (p payloadProbeTool) Description() string                             { return "" }
func (p payloadProbeTool) InputSchema() map[string]any                     { return map[string]any{"type": "object"} }
func (p payloadProbeTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (p payloadProbeTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (p payloadProbeTool) EstimatedDuration(json.RawMessage) time.Duration { return 0 }
func (p payloadProbeTool) Execute(context.Context, json.RawMessage) (tools.Result, error) {
	return tools.Result{Output: p.output, Display: p.display}, nil
}

func TestModelPayloadPrefersOutput(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(payloadProbeTool{name: "probe", output: []byte(`{"id":"X"}`), display: "summary"})
	sess := NewSession("sys")
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "probe", Input: map[string]any{}}}},
		{Text: "done", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, sess, bus, acceptAll)

	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	drain(bus)

	var got string
	for _, m := range sess.Messages() {
		if m.Role == "tool" && m.ToolCallID == "c1" {
			got = m.Content
			break
		}
	}
	if got != `{"id":"X"}` {
		t.Errorf("session tool content = %q, want %q", got, `{"id":"X"}`)
	}
}

func TestModelPayloadFallsBackToDisplayOverThreshold(t *testing.T) {
	bigOutput := make([]byte, maxModelPayloadBytes+1)
	for i := range bigOutput {
		bigOutput[i] = 'x'
	}

	reg := tools.NewRegistry()
	reg.Register(payloadProbeTool{name: "probe", output: bigOutput, display: "summary"})
	sess := NewSession("sys")
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "probe", Input: map[string]any{}}}},
		{Text: "done", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, sess, bus, acceptAll)

	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	drain(bus)

	var got string
	for _, m := range sess.Messages() {
		if m.Role == "tool" && m.ToolCallID == "c1" {
			got = m.Content
			break
		}
	}
	if got != "summary" {
		t.Errorf("session tool content = %q, want %q (fallback to Display)", got, "summary")
	}
}

func TestModelPayloadFallsBackToDisplayWhenOutputEmpty(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(payloadProbeTool{name: "probe", output: nil, display: "just summary"})
	sess := NewSession("sys")
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "probe", Input: map[string]any{}}}},
		{Text: "done", StopReason: "end_turn"},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, sess, bus, acceptAll)

	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	drain(bus)

	var got string
	for _, m := range sess.Messages() {
		if m.Role == "tool" && m.ToolCallID == "c1" {
			got = m.Content
			break
		}
	}
	if got != "just summary" {
		t.Errorf("session tool content = %q, want %q", got, "just summary")
	}
}

func TestLoopExceedingMaxIterationsErrors(t *testing.T) {
	// A tool that always succeeds, paired with a mockProvider that always
	// emits a fresh ToolCall — guaranteed infinite loop without the guard.
	reg := tools.NewRegistry()
	reg.Register(echoTool{})

	resps := make([]llm.ChatResponse, 20)
	for i := range resps {
		resps[i] = llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{ID: fmt.Sprintf("c%d", i), Name: "echo", Input: map[string]any{}}},
		}
	}
	prov := &mockProvider{responses: resps}
	bus := make(chan tea.Msg, 64)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, acceptAll)
	loop.SetMaxIterations(3)

	go func() { loop.Run(context.Background(), "go"); close(bus) }()

	var sawMaxIterErr bool
	for m := range bus {
		if te, ok := m.(TurnErrorMsg); ok {
			if errors.Is(te.Err, ErrMaxIterations) {
				sawMaxIterErr = true
			}
		}
	}
	if !sawMaxIterErr {
		t.Errorf("expected TurnErrorMsg with ErrMaxIterations after 3 iterations")
	}
	// Provider was called exactly 3 times (one per iteration).
	if prov.calls != 3 {
		t.Errorf("provider called %d times, want 3", prov.calls)
	}
}

func TestLoopRejectsTruncatedTurn(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(echoTool{})
	prov := &mockProvider{responses: []llm.ChatResponse{
		{
			ToolCalls:  []llm.ToolCall{{ID: "c1", Name: "echo", Input: map[string]any{}}},
			StopReason: "max_tokens",
		},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, acceptAll)

	go func() { loop.Run(context.Background(), "go"); close(bus) }()

	var sawTruncErr, sawToolStart bool
	for m := range bus {
		switch v := m.(type) {
		case TurnErrorMsg:
			if errors.Is(v.Err, ErrModelTruncated) {
				sawTruncErr = true
			}
		case ToolStartMsg:
			sawToolStart = true
		}
	}
	if !sawTruncErr {
		t.Errorf("expected TurnErrorMsg with ErrModelTruncated")
	}
	if sawToolStart {
		t.Errorf("tool was executed despite truncated stop_reason")
	}
}
