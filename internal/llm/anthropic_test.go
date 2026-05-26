package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// cannedMessage is a minimal valid Anthropic Messages API response.
const cannedMessage = `{
  "id": "msg_1",
  "type": "message",
  "role": "assistant",
  "model": "claude-test",
  "content": [{"type": "text", "text": "Hello from Claude."}],
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 7, "output_tokens": 5}
}`

func TestAnthropicChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedMessage))
	}))
	defer srv.Close()

	p := newAnthropicProviderWithBaseURL("key", srv.URL)
	resp, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-test",
		System:   "You are a test.",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.Text != "Hello from Claude." {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", resp.Usage.OutputTokens)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
}

// TestAnthropicChatToolConversion guards against two regressions: dropping the
// tool description and dropping the JSON-schema "required" array when
// converting llm.ToolSpec into Anthropic SDK tool params.
func TestAnthropicChatToolConversion(t *testing.T) {
	var sentBody struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema struct {
				Required []string `json:"required"`
			} `json:"input_schema"`
		} `json:"tools"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		if err := json.Unmarshal(body, &sentBody); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedMessage))
	}))
	defer srv.Close()

	p := newAnthropicProviderWithBaseURL("key", srv.URL)
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: "Hi"}},
		Tools: []ToolSpec{{
			Name:        "lookup",
			Description: "Look up a record by id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x": map[string]any{"type": "string"},
				},
				"required": []string{"x"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if len(sentBody.Tools) != 1 {
		t.Fatalf("sent %d tools, want 1", len(sentBody.Tools))
	}
	tool := sentBody.Tools[0]
	if tool.Name != "lookup" {
		t.Errorf("tool name = %q, want %q", tool.Name, "lookup")
	}
	if tool.Description != "Look up a record by id." {
		t.Errorf("tool description = %q, want %q", tool.Description, "Look up a record by id.")
	}
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "x" {
		t.Errorf("tool input_schema.required = %v, want [x]", tool.InputSchema.Required)
	}
}

// TestAnthropicProviderPassesTemperature guards that ChatRequest.Temperature
// is forwarded onto the Anthropic Messages request body. Without this wiring
// the field is dead code and every turn ships at the SDK default.
func TestAnthropicProviderPassesTemperature(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedMessage))
	}))
	defer srv.Close()

	p := newAnthropicProviderWithBaseURL("key", srv.URL)
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:       "claude-test",
		Messages:    []Message{{Role: "user", Content: "Hi"}},
		Temperature: 0.3,
		MaxTokens:   100,
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	got, ok := captured["temperature"].(float64)
	if !ok {
		t.Fatalf("temperature missing from request body: %+v", captured)
	}
	// req.Temperature is float32 so widening to float64 introduces a tiny
	// representational error (0.3 → 0.300000011...). Compare with tolerance.
	if diff := got - 0.3; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("temperature in request = %v, want ~0.3", got)
	}
}
