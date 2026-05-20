package tui

import (
	"fmt"
	"time"

	"github.com/alvarogonjim/fova/internal/replay"
)

// replayMaxPace caps how long the replay goroutine waits between events.
// A real session may have minutes between turns; replay slows the skim
// to 50 ms per event, so a 40-event document plays back in ~2 s.
const replayMaxPace = 50 * time.Millisecond

// replayTickMsg is the internal bus message that drives one replay event
// into the chat. Sent by the pump goroutine; consumed in Model.Update.
type replayTickMsg struct {
	event replay.Event
	index int // 1-based, for the "replay X/Y" status line
}

// replayDoneMsg signals the pump has played every event. The TUI uses it
// to update the status footer one last time so the user sees "replay N/N".
type replayDoneMsg struct{}

// startReplay launches the replay pump goroutine. It walks events, posts
// one replayTickMsg per event, then a final replayDoneMsg, all to the
// agent bus. step is buffered so a Space press never blocks the UI.
func (m *Model) startReplay(events []replay.Event, pace bool) {
	m.replayEvents = events
	m.replayTotal = len(events)
	m.replayIndex = 0
	m.replayStep = make(chan struct{}, 16)
	m.replayPace = pace
	go m.replayPump()
}

// replayPump walks the recorded events at the paced cadence, posting one
// replayTickMsg per event. It honours Space-step signals on m.replayStep:
// a single step collapses any in-flight wait.
func (m *Model) replayPump() {
	for i, ev := range m.replayEvents {
		m.bus <- replayTickMsg{event: ev, index: i + 1}
		if i+1 >= len(m.replayEvents) {
			break
		}
		if !m.replayPace {
			continue
		}
		next := m.replayEvents[i+1]
		delta := next.TS.Sub(ev.TS)
		if delta < 0 {
			delta = 0
		}
		if delta > replayMaxPace {
			delta = replayMaxPace
		}
		if delta == 0 {
			continue
		}
		select {
		case <-time.After(delta):
		case <-m.replayStep:
		}
	}
	m.bus <- replayDoneMsg{}
}

// applyReplayEvent routes one event into the chat. It mirrors the live
// bus's agent.* handlers but reads from the recorded event instead.
func (m *Model) applyReplayEvent(ev replay.Event) {
	switch ev.Kind {
	case replay.KindUserMsg:
		m.chat.appendUser(ev.Text)
	case replay.KindAgentText:
		m.chat.appendAgentDeltaBlock(ev.Text)
	case replay.KindToolStart:
		m.chat.appendToolStart(ev.Name)
	case replay.KindToolResult:
		display := ev.Display
		if ev.Err != "" {
			display = "error: " + ev.Err
		}
		m.chat.appendToolDone(ev.Name, display)
	case replay.KindTurnDone:
		// Replay turn_done is a separator; the live counterpart resets
		// the spinner and accumulates cost, but replay has neither.
	}
}

// updateReplayStatus refreshes the "replay X/Y" footer segment.
func (m *Model) updateReplayStatus() {
	m.status.replay = fmt.Sprintf("replay %d/%d", m.replayIndex, m.replayTotal)
}

// stepReplay collapses any in-flight pacing wait so the next event fires
// immediately. Multiple presses are absorbed by the buffered channel.
func (m *Model) stepReplay() {
	if m.replayStep == nil {
		return
	}
	select {
	case m.replayStep <- struct{}{}:
	default:
	}
}
