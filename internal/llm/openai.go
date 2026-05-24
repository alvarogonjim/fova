package llm

import (
	"context"
	"encoding/json"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/respjson"
	"github.com/openai/openai-go/shared"
)

// openAIProvider talks to any OpenAI-compatible Chat Completions endpoint.
type openAIProvider struct {
	name   string
	client openai.Client
}

// NewOpenAIProvider builds a provider for an OpenAI-compatible endpoint.
// name is a label (openai|ollama|vllm); baseURL selects the backend.
func NewOpenAIProvider(name, baseURL, apiKey string) Provider {
	client := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
	)
	return &openAIProvider{name: name, client: client}
}

func (p *openAIProvider) Name() string { return p.name }

func (p *openAIProvider) Models(ctx context.Context) ([]ModelDescriptor, error) {
	// v0.1 surfaces models via the static ModelRegistry, not a live list.
	return nil, nil
}

func (p *openAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openai.SystemMessage(req.System))
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			msgs = append(msgs, openai.UserMessage(m.Content))
		case "assistant":
			if len(m.ToolCalls) > 0 {
				am := openai.ChatCompletionAssistantMessageParam{}
				for _, tc := range m.ToolCalls {
					args, _ := json.Marshal(tc.Input)
					am.ToolCalls = append(am.ToolCalls, openai.ChatCompletionMessageToolCallParam{
						ID: tc.ID,
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      tc.Name,
							Arguments: string(args),
						},
					})
				}
				if m.Content != "" {
					am.Content.OfString = openai.String(m.Content)
				}
				msgs = append(msgs, openai.ChatCompletionMessageParamUnion{OfAssistant: &am})
			} else {
				msgs = append(msgs, openai.AssistantMessage(m.Content))
			}
		case "tool":
			msgs = append(msgs, openai.ToolMessage(m.Content, m.ToolCallID))
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(req.Model),
		Messages: msgs,
	}
	if req.MaxTokens > 0 {
		params.MaxTokens = openai.Int(int64(req.MaxTokens))
	}
	if req.Temperature > 0 {
		params.Temperature = openai.Float(float64(req.Temperature))
	}
	for _, ts := range req.Tools {
		params.Tools = append(params.Tools, openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        ts.Name,
				Description: openai.String(ts.Description),
				Parameters:  shared.FunctionParameters(ts.InputSchema),
			},
		})
	}

	completion, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}

	resp := &ChatResponse{
		Usage: Usage{
			InputTokens:  int(completion.Usage.PromptTokens),
			OutputTokens: int(completion.Usage.CompletionTokens),
		},
	}
	if len(completion.Choices) > 0 {
		choice := completion.Choices[0]
		resp.Text = choice.Message.Content
		resp.Reasoning = extractReasoning(choice.Message.JSON.ExtraFields)
		resp.StopReason = string(choice.FinishReason)
		for _, tc := range choice.Message.ToolCalls {
			var input map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
	}
	return resp, nil
}

// extractReasoning pulls vLLM's (and any other OpenAI-compatible) reasoning
// payload out of the message's ExtraFields. vLLM started with
// `--enable-reasoning --reasoning-parser <name>` returns a `reasoning_content`
// string on the message; older or alternative implementations may use
// `reasoning` instead. Both shapes are accepted. An empty / missing field
// yields "" and is harmless.
//
// NB: respjson.Field.Valid() returns false for ExtraFields because they have
// no matching typed struct field to validate against — we therefore only
// look at Raw() and unmarshal it directly.
func extractReasoning(extras map[string]respjson.Field) string {
	for _, key := range []string{"reasoning_content", "reasoning"} {
		f, ok := extras[key]
		if !ok {
			continue
		}
		raw := f.Raw()
		if raw == "" || raw == "null" {
			continue
		}
		var s string
		if err := json.Unmarshal([]byte(raw), &s); err == nil && s != "" {
			return s
		}
	}
	return ""
}

// StreamChat wraps Chat: it performs a blocking call and emits the whole
// response as one text_delta plus a done event (streaming-deferred design).
func (p *openAIProvider) StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
	ch := make(chan ChatEvent, 8)
	go func() {
		defer close(ch)
		resp, err := p.Chat(ctx, req)
		if err != nil {
			ch <- ChatEvent{Kind: "error", Err: err}
			return
		}
		if resp.Reasoning != "" {
			ch <- ChatEvent{Kind: "reasoning_delta", Delta: resp.Reasoning}
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

func (p *openAIProvider) EstimateCost(req ChatRequest, resp *ChatResponse) float64 {
	return 0 // priced via the ModelRegistry in a later milestone
}
