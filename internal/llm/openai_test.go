package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/packages/respjson"
)

// cannedChatCompletion is a minimal valid OpenAI Chat Completions response.
const cannedChatCompletion = `{
  "id": "chatcmpl-1",
  "object": "chat.completion",
  "created": 1,
  "model": "test",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "Hello from the model."},
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 5, "completion_tokens": 4, "total_tokens": 9}
}`

func TestOpenAIChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedChatCompletion))
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test", srv.URL, "key")
	resp, err := p.Chat(context.Background(), ChatRequest{
		Model:    "test",
		System:   "You are a test.",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.Text != "Hello from the model." {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.Usage.OutputTokens != 4 {
		t.Errorf("OutputTokens = %d, want 4", resp.Usage.OutputTokens)
	}
}

func TestOpenAIStreamWrapsChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedChatCompletion))
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test", srv.URL, "key")
	events, err := p.StreamChat(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var sawDone bool
	for ev := range events {
		switch ev.Kind {
		case "text_delta":
			text += ev.Delta
		case "done":
			sawDone = true
		case "error":
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if text != "Hello from the model." || !sawDone {
		t.Fatalf("stream gave text=%q done=%v", text, sawDone)
	}
}

// vllmReasoningCompletion is a vLLM-shape response carrying reasoning_content
// alongside content (what vllm --enable-reasoning --reasoning-parser qwen3
// emits).
const vllmReasoningCompletion = `{
  "id": "chatcmpl-2",
  "object": "chat.completion",
  "created": 1,
  "model": "Qwen/Qwen3.6-27B",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Hello!",
      "reasoning_content": "The user said hi. I should greet them."
    },
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 3, "completion_tokens": 11, "total_tokens": 14}
}`

func TestOpenAIChatCapturesReasoningContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(vllmReasoningCompletion))
	}))
	defer srv.Close()

	p := NewOpenAIProvider("vllm", srv.URL, "")
	resp, err := p.Chat(context.Background(), ChatRequest{
		Model:    "Qwen/Qwen3.6-27B",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Text != "Hello!" {
		t.Errorf("Text = %q, want Hello!", resp.Text)
	}
	if resp.Reasoning != "The user said hi. I should greet them." {
		t.Errorf("Reasoning = %q", resp.Reasoning)
	}
}

func TestOpenAIStreamEmitsReasoningDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(vllmReasoningCompletion))
	}))
	defer srv.Close()

	p := NewOpenAIProvider("vllm", srv.URL, "")
	events, err := p.StreamChat(context.Background(), ChatRequest{
		Model:    "Qwen/Qwen3.6-27B",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sawReasoning, sawText, sawDone bool
	var reasoning, text string
	for ev := range events {
		switch ev.Kind {
		case "reasoning_delta":
			sawReasoning = true
			reasoning += ev.Delta
		case "text_delta":
			sawText = true
			text += ev.Delta
		case "done":
			sawDone = true
		}
	}
	if !sawReasoning || reasoning == "" {
		t.Errorf("expected a reasoning_delta event, got reasoning=%q", reasoning)
	}
	if !sawText || text != "Hello!" {
		t.Errorf("expected a text_delta of %q, got %q", "Hello!", text)
	}
	if !sawDone {
		t.Error("missing done event")
	}
}

func TestExtractReasoningPrefersReasoningContent(t *testing.T) {
	extras := map[string]respjson.Field{
		"reasoning_content": respjson.NewField(`"I should answer concisely."`),
		"reasoning":         respjson.NewField(`"old key"`),
	}
	if got := extractReasoning(extras); got != "I should answer concisely." {
		t.Errorf("extractReasoning = %q, want the reasoning_content value", got)
	}
}

func TestExtractReasoningFallsBackToReasoning(t *testing.T) {
	extras := map[string]respjson.Field{
		"reasoning": respjson.NewField(`"older shape"`),
	}
	if got := extractReasoning(extras); got != "older shape" {
		t.Errorf("extractReasoning = %q, want older shape", got)
	}
}

func TestExtractReasoningEmptyWhenMissing(t *testing.T) {
	if got := extractReasoning(map[string]respjson.Field{}); got != "" {
		t.Errorf("extractReasoning(empty) = %q, want empty", got)
	}
}

func TestExtractReasoningEmptyWhenNullOrEmpty(t *testing.T) {
	cases := map[string]map[string]respjson.Field{
		"explicit null": {"reasoning_content": respjson.NewField(`null`)},
		"empty string":  {"reasoning_content": respjson.NewField(`""`)},
		"omitted":       {"reasoning_content": respjson.NewField("")},
	}
	for name, extras := range cases {
		if got := extractReasoning(extras); got != "" {
			t.Errorf("extractReasoning(%s) = %q, want empty", name, got)
		}
	}
}

func TestExtractReasoningSurvivesInvalidJSON(t *testing.T) {
	extras := map[string]respjson.Field{
		"reasoning_content": respjson.NewField(`not-json`),
	}
	if got := extractReasoning(extras); got != "" {
		t.Errorf("extractReasoning(bad json) = %q, want empty", got)
	}
}
