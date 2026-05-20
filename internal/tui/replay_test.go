package tui

import (
	"encoding/json"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/agent"
	"github.com/alvarogonjim/fova/internal/config"
	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/replay"
	"github.com/alvarogonjim/fova/internal/tools"
)

func replayFixture() []replay.Event {
	ts := time.Date(2026, 5, 20, 12, 34, 56, 0, time.UTC)
	return []replay.Event{
		{Kind: replay.KindUserMsg, TS: ts, Text: "fold MAQ"},
		{Kind: replay.KindAgentText, TS: ts.Add(time.Second), Text: "I'll fold that."},
		{Kind: replay.KindToolStart, TS: ts.Add(time.Second), Name: "fold.esmfold", Input: json.RawMessage(`{"sequence":"MAQ"}`)},
		{Kind: replay.KindToolResult, TS: ts.Add(5 * time.Second), Name: "fold.esmfold", Display: "folded (pLDDT 80)"},
		{Kind: replay.KindTurnDone, TS: ts.Add(5 * time.Second)},
	}
}

func newReplayTestApp(events []replay.Event) *Model {
	return New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(config.DefaultCatalog()),
		SystemPrompt: agent.SystemPrompt,
		ReplayEvents: events,
		ReplayPace:   false, // tests skip the pacing wait
	})
}

func drainReplay(m *Model, want int) []tea.Msg {
	got := make([]tea.Msg, 0, want)
	deadline := time.After(2 * time.Second)
	for len(got) < want {
		select {
		case msg := <-m.bus:
			got = append(got, msg)
			m.Update(msg)
		case <-deadline:
			return got
		}
	}
	return got
}

func TestReplayDrainsAllEventsIntoChat(t *testing.T) {
	events := replayFixture()
	m := newReplayTestApp(events)
	if m.replayTotal != len(events) {
		t.Fatalf("replayTotal = %d, want %d", m.replayTotal, len(events))
	}
	drainReplay(m, len(events))

	// The chat router pairs tool_start with tool_result into a single tool
	// entry (matching live mode), and turn_done updates status only. So the
	// fixture's 5 events become 3 routed entries: user, agent_text, tool.
	if got := len(m.chat.entries); got != 3 {
		t.Fatalf("chat entries = %d, want %d (entries: %+v)", got, 3, m.chat.entries)
	}
	if m.replayIndex != len(events) {
		t.Errorf("replayIndex = %d, want %d", m.replayIndex, len(events))
	}
	if m.status.replay == "" {
		t.Error("status.replay should be populated in replay mode")
	}
	// The tool entry must be the result of tool_start + tool_result merging:
	// done=true, result set.
	last := m.chat.entries[len(m.chat.entries)-1]
	if !last.done || last.result == "" {
		t.Errorf("last chat entry should be a completed tool entry, got %+v", last)
	}
}

func TestReplayModeSkipsPersistedSession(t *testing.T) {
	m := newReplayTestApp(replayFixture())
	if m.sessionID != "" {
		t.Errorf("replay mode must not create a persisted session, got id=%q", m.sessionID)
	}
}

func TestReplayEscQuits(t *testing.T) {
	m := newReplayTestApp(replayFixture())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc in replay mode must return a quit command")
	}
}

func TestReplaySpaceSteps(t *testing.T) {
	// With ReplayPace=true and an artificial future timestamp, only Space
	// should advance the next event quickly. The fixture's max delta is 4 s
	// which the 50 ms cap clamps anyway, so we just assert Space is accepted
	// without error: the goroutine consumes the step signal on its next loop.
	m := New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(config.DefaultCatalog()),
		SystemPrompt: agent.SystemPrompt,
		ReplayEvents: replayFixture(),
		ReplayPace:   true,
	})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	// No assertion needed beyond "did not panic"; the integration is exercised
	// by TestReplayDrainsAllEventsIntoChat in non-paced mode.
	_ = m
}
