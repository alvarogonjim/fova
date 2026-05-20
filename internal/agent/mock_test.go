package agent

import (
	"context"

	"github.com/alvarogonjim/fova/internal/llm"
)

// mockProvider returns scripted responses, one per Chat/StreamChat call.
type mockProvider struct {
	responses []llm.ChatResponse
	calls     int
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Models(context.Context) ([]llm.ModelDescriptor, error) {
	return nil, nil
}
func (m *mockProvider) EstimateCost(llm.ChatRequest, *llm.ChatResponse) float64 { return 0 }

func (m *mockProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	r := m.responses[m.calls]
	m.calls++
	return &r, nil
}

func (m *mockProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	ch := make(chan llm.ChatEvent, 8)
	go func() {
		defer close(ch)
		resp, _ := m.Chat(ctx, req)
		if resp.Text != "" {
			ch <- llm.ChatEvent{Kind: "text_delta", Delta: resp.Text}
		}
		for i := range resp.ToolCalls {
			ch <- llm.ChatEvent{Kind: "tool_call", Call: &resp.ToolCalls[i]}
		}
		ch <- llm.ChatEvent{Kind: "done", StopReason: resp.StopReason, Usage: resp.Usage}
	}()
	return ch, nil
}
