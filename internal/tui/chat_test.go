package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
)

func TestChatAppendAndRender(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendUser("fold MAQ")
	c.appendAgentDelta("Folding ")
	c.appendAgentDelta("now.")
	out := c.renderEntries()
	if !strings.Contains(out, "fold MAQ") {
		t.Errorf("user message missing: %q", out)
	}
	if !strings.Contains(out, "Folding now.") {
		t.Errorf("agent deltas not merged: %q", out)
	}
}

func TestChatToolTrace(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendToolStart("fold.esmfold")
	c.appendToolDone("fold.esmfold", "folded d_0001 (pLDDT mean 80.0)")
	out := c.renderEntries()
	if !strings.Contains(out, "fold.esmfold") || !strings.Contains(out, "pLDDT") {
		t.Errorf("tool trace not rendered: %q", out)
	}
}

func TestChatWelcomeEntry(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendWelcome("hi there")
	out := c.renderEntries()
	if !strings.Contains(out, "hi there") {
		t.Errorf("welcome text missing: %q", out)
	}
}

func TestChatToolTraceConnectors(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendToolStart("fold.esmfold")
	c.appendToolDone("fold.esmfold", "pLDDT 91.2\npath /tmp/x.pdb")
	out := c.renderEntries()
	if !strings.Contains(out, "⏺") {
		t.Errorf("tool header glyph ⏺ missing: %q", out)
	}
	if !strings.Contains(out, "fold.esmfold") {
		t.Errorf("tool name missing: %q", out)
	}
	if !strings.Contains(out, "⎿") {
		t.Errorf("result connector ⎿ missing: %q", out)
	}
	if !strings.Contains(out, "pLDDT 91.2") {
		t.Errorf("result body missing: %q", out)
	}
}

func TestChatToolTraceTruncation(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendToolStart("scan")
	display := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9"
	c.appendToolDone("scan", display)
	out := c.renderEntries()
	if !strings.Contains(out, "… +3 lines") {
		t.Errorf("truncation footer missing or wrong count: %q", out)
	}
	if strings.Contains(out, "l9") {
		t.Errorf("overflow line l9 should have been dropped: %q", out)
	}
}

func TestChatToolTraceError(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendToolStart("x")
	c.appendToolDone("x", "error: boom")
	out := c.renderEntries()
	if !strings.Contains(out, "✗") {
		t.Errorf("errored tool should render ✗: %q", out)
	}
}

// TestChatJobLogUpsertUpdatesInPlace verifies upsertJobLog creates a block the
// first time a job is seen and updates it in place thereafter — the entry
// count must not grow on the second call, and the new tail must show.
func TestChatJobLogUpsertUpdatesInPlace(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	started := time.Now().Add(-90 * time.Second)

	c.upsertJobLog("j1", "install bindcraft", domain.JobRunning, &started, []string{"line a"})
	if got := len(c.entries); got != 1 {
		t.Fatalf("first upsertJobLog: entry count = %d, want 1", got)
	}

	c.upsertJobLog("j1", "install bindcraft", domain.JobRunning, &started, []string{"line a", "line b"})
	if got := len(c.entries); got != 1 {
		t.Fatalf("second upsertJobLog grew entry count to %d, want 1", got)
	}

	out := c.renderEntries()
	if !strings.Contains(out, "line b") {
		t.Errorf("updated tail line missing: %q", out)
	}
}

// TestChatJobLogRender verifies renderEntries shows the tool name and a tail
// line for a job-log block.
func TestChatJobLogRender(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	started := time.Now().Add(-30 * time.Second)
	c.upsertJobLog("j_8f2a", "install bindcraft", domain.JobRunning, &started,
		[]string{"cloning repo", "building"})

	out := c.renderEntries()
	if !strings.Contains(out, "install bindcraft") {
		t.Errorf("job-log tool name missing: %q", out)
	}
	if !strings.Contains(out, "cloning repo") {
		t.Errorf("job-log tail line missing: %q", out)
	}
	if !strings.Contains(out, "⎿") {
		t.Errorf("job-log tail connector ⎿ missing: %q", out)
	}
}
