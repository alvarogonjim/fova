package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
