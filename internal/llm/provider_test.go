package llm

import "testing"

func TestChatEventCarriesUsageAndStopReason(t *testing.T) {
	// SPECS §11's loop reads ev.Usage and ev.StopReason on the "done" event;
	// SPECS §6.1's struct omits them. v0.1 adds them — this guards that.
	ev := ChatEvent{Kind: "done", Usage: Usage{InputTokens: 1, OutputTokens: 2}, StopReason: "end_turn"}
	if ev.Usage.OutputTokens != 2 || ev.StopReason != "end_turn" {
		t.Fatal("ChatEvent must carry Usage and StopReason")
	}
}

func TestMessageRolesCompile(t *testing.T) {
	_ = Message{Role: "user", Content: "hi"}
	_ = Message{Role: "assistant", ToolCalls: []ToolCall{{ID: "1", Name: "x"}}}
	_ = Message{Role: "tool", ToolCallID: "1", Content: "result"}
}
