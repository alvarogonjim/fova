// Package agent implements the Proteus ReAct agent loop.
package agent

import "github.com/alvarogonjim/proteus/internal/llm"

// Session holds the conversation history for one agent run.
// v0.1 keeps everything in memory with no compaction.
type Session struct {
	system   string
	messages []llm.Message
}

// NewSession starts a session with the given system prompt.
func NewSession(systemPrompt string) *Session {
	return &Session{system: systemPrompt}
}

// SystemPrompt returns the system prompt.
func (s *Session) SystemPrompt() string { return s.system }

// Messages returns a copy of the conversation so far.
func (s *Session) Messages() []llm.Message {
	out := make([]llm.Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// AddUserMessage appends a user turn.
func (s *Session) AddUserMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "user", Content: content})
}

// AddAssistantMessage appends an assistant turn from a model response.
func (s *Session) AddAssistantMessage(resp llm.ChatResponse) {
	s.messages = append(s.messages, llm.Message{
		Role:      "assistant",
		Content:   resp.Text,
		ToolCalls: resp.ToolCalls,
	})
}

// AddToolResult appends a tool-result turn answering a tool call.
func (s *Session) AddToolResult(toolCallID, content string) {
	s.messages = append(s.messages, llm.Message{
		Role:       "tool",
		Content:    content,
		ToolCallID: toolCallID,
	})
}
