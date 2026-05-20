// Package llm defines the LLM provider abstraction used by the agent.
package llm

import "context"

// Provider is one LLM backend (Anthropic, OpenAI, Ollama, ...).
type Provider interface {
	Name() string
	Models(ctx context.Context) ([]ModelDescriptor, error)
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
	EstimateCost(req ChatRequest, resp *ChatResponse) float64
}

// ModelDescriptor describes one model offered by a provider.
type ModelDescriptor struct {
	ID               string
	DisplayName      string
	ContextTokens    int
	SupportsTools    bool
	InputPricePer1M  float64
	OutputPricePer1M float64
}

// ChatRequest is one request to a model.
type ChatRequest struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSpec
	Temperature float32
	MaxTokens   int
}

// Message is one turn in the conversation. Role is user | assistant | tool.
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
}

// ToolCall is a model request to invoke a tool.
type ToolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolSpec advertises a tool to the model.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ChatResponse is a completed model response.
type ChatResponse struct {
	Text       string
	ToolCalls  []ToolCall
	Usage      Usage
	StopReason string
}

// Usage reports token consumption.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// ChatEvent is one streamed event. Kind is text_delta | tool_call | done | error.
// Usage and StopReason are set on the "done" event (v0.1 extension over SPECS §6.1).
type ChatEvent struct {
	Kind       string
	Delta      string
	Call       *ToolCall
	Err        error
	Usage      Usage
	StopReason string
}
