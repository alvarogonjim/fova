package llm

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const defaultAnthropicMaxTokens = 4096

type anthropicProvider struct {
	client anthropic.Client
}

// NewAnthropicProvider builds the Anthropic provider.
func NewAnthropicProvider(apiKey string) Provider {
	return &anthropicProvider{client: anthropic.NewClient(option.WithAPIKey(apiKey))}
}

// newAnthropicProviderWithBaseURL is used by tests to target an httptest server.
func newAnthropicProviderWithBaseURL(apiKey, baseURL string) Provider {
	return &anthropicProvider{
		client: anthropic.NewClient(
			option.WithAPIKey(apiKey),
			option.WithBaseURL(baseURL),
		),
	}
}

func (p *anthropicProvider) Name() string { return "anthropic" }

func (p *anthropicProvider) Models(ctx context.Context) ([]ModelDescriptor, error) {
	return nil, nil
}

func (p *anthropicProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}

	msgs := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			blocks := []anthropic.ContentBlockParamUnion{}
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, tc.Input, tc.Name))
			}
			msgs = append(msgs, anthropic.NewAssistantMessage(blocks...))
		case "tool":
			msgs = append(msgs, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false),
			))
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(maxTokens),
		Messages:  msgs,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if req.Temperature > 0 {
		params.Temperature = anthropic.Float(float64(req.Temperature))
	}
	for _, ts := range req.Tools {
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        ts.Name,
				Description: anthropic.String(ts.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: ts.InputSchema["properties"],
					Required:   toStringSlice(ts.InputSchema["required"]),
				},
			},
		})
	}

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, err
	}

	resp := &ChatResponse{
		StopReason: string(msg.StopReason),
		Usage: Usage{
			InputTokens:  int(msg.Usage.InputTokens),
			OutputTokens: int(msg.Usage.OutputTokens),
		},
	}
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			resp.Text += b.Text
		case anthropic.ToolUseBlock:
			var input map[string]any
			_ = json.Unmarshal(b.Input, &input)
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:    b.ID,
				Name:  b.Name,
				Input: input,
			})
		}
	}
	return resp, nil
}

// StreamChat wraps Chat (streaming-deferred design; see design doc).
func (p *anthropicProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	ch := make(chan ChatEvent, 8)
	go func() {
		defer close(ch)
		resp, err := p.Chat(ctx, req)
		if err != nil {
			ch <- ChatEvent{Kind: "error", Err: err}
			return
		}
		if resp.Text != "" {
			ch <- ChatEvent{Kind: "text_delta", Delta: resp.Text}
		}
		for i := range resp.ToolCalls {
			ch <- ChatEvent{Kind: "tool_call", Call: &resp.ToolCalls[i]}
		}
		ch <- ChatEvent{Kind: "done", Usage: resp.Usage, StopReason: resp.StopReason}
	}()
	return ch, nil
}

func (p *anthropicProvider) EstimateCost(req ChatRequest, resp *ChatResponse) float64 {
	return 0
}

// toStringSlice converts a JSON-schema "required" value into a []string.
// It accepts a native []string, a []any (produced when a schema is
// round-tripped through JSON), or a missing/nil value (→ empty slice).
func toStringSlice(v any) []string {
	switch vs := v.(type) {
	case []string:
		return vs
	case []any:
		out := make([]string, 0, len(vs))
		for _, e := range vs {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
