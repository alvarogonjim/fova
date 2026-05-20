package agent

import (
	"strings"
	"testing"

	"github.com/alvarogonjim/proteus/internal/llm"
)

func TestSystemPromptEmbedded(t *testing.T) {
	if !strings.Contains(SystemPrompt, "You are Proteus") {
		t.Fatal("system prompt not embedded")
	}
}

func TestSessionAccumulatesMessages(t *testing.T) {
	s := NewSession("SYSTEM")
	if s.SystemPrompt() != "SYSTEM" {
		t.Fatal("system prompt not stored")
	}
	s.AddUserMessage("fold MAQ")
	s.AddAssistantMessage(llm.ChatResponse{
		Text:      "ok",
		ToolCalls: []llm.ToolCall{{ID: "t1", Name: "fold.esmfold", Input: map[string]any{"sequence": "MAQ"}}},
	})
	s.AddToolResult("t1", "folded")

	msgs := s.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" || msgs[2].Role != "tool" {
		t.Fatalf("unexpected roles: %v %v %v", msgs[0].Role, msgs[1].Role, msgs[2].Role)
	}
	if msgs[2].ToolCallID != "t1" || msgs[2].Content != "folded" {
		t.Fatalf("tool result wrong: %+v", msgs[2])
	}
}
