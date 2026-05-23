package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/domain"
)

func TestChatMouseWheelScrollsUp(t *testing.T) {
	c := newChatModel(NewTheme(), 40, 4)
	for i := 0; i < 30; i++ {
		c.appendAgentDeltaBlock(fmt.Sprintf("line %d", i))
	}
	c.viewport.GotoBottom()
	if !c.viewport.AtBottom() {
		t.Fatal("setup: chat should start at the bottom")
	}
	c.handleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if c.viewport.AtBottom() {
		t.Error("wheel-up should scroll the chat off the bottom")
	}
}

func TestChatRefreshKeepsScrollPositionWhenScrolledUp(t *testing.T) {
	c := newChatModel(NewTheme(), 40, 4)
	for i := 0; i < 30; i++ {
		c.appendAgentDeltaBlock(fmt.Sprintf("line %d", i))
	}
	c.viewport.GotoTop()
	c.appendAgentDeltaBlock("new content while scrolled up")
	if c.viewport.AtBottom() {
		t.Error("refresh must not snap a scrolled-up reader to the bottom")
	}
}

func TestChatRefreshFollowsWhenAtBottom(t *testing.T) {
	c := newChatModel(NewTheme(), 40, 4)
	for i := 0; i < 30; i++ {
		c.appendAgentDeltaBlock(fmt.Sprintf("line %d", i))
	}
	if !c.viewport.AtBottom() {
		t.Error("a reader at the bottom should keep following new content")
	}
}

func TestChatAppendUserJumpsToBottom(t *testing.T) {
	c := newChatModel(NewTheme(), 40, 4)
	for i := 0; i < 30; i++ {
		c.appendAgentDeltaBlock(fmt.Sprintf("line %d", i))
	}
	c.viewport.GotoTop()
	c.appendUser("my message")
	if !c.viewport.AtBottom() {
		t.Error("sending a message should jump the chat to the bottom")
	}
}

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

// TestChatSlashOutputPreservesNewlines guards against the v0.6 regression
// where slash-command output (/plan, /doctor, /tools) collapsed into a single
// paragraph because the agent renderer ran multi-line plain text through
// glamour, which folds intra-paragraph newlines into spaces. Multi-line slash
// output must keep at least its original line count when rendered.
func TestChatSlashOutputPreservesNewlines(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	multi := "Target:        1LYZ chain A\nApplication:   binder\nMethod:        BindCraft\nFilters:       (none set)\nShortlist:     50"
	c.appendSlashOutput(multi)
	out := c.renderEntries()
	for _, want := range []string{"Target:", "Application:", "Method:", "Filters:", "Shortlist:"} {
		if !strings.Contains(out, want) {
			t.Errorf("label %q missing from rendered slash output: %q", want, out)
		}
	}
	// All five labels must each appear on their own line — count newlines that
	// sit between the labels.
	idxs := []int{
		strings.Index(out, "Target:"),
		strings.Index(out, "Application:"),
		strings.Index(out, "Method:"),
		strings.Index(out, "Filters:"),
		strings.Index(out, "Shortlist:"),
	}
	for i := 1; i < len(idxs); i++ {
		if idxs[i] <= idxs[i-1] {
			t.Fatalf("labels out of order in output: %v\n%s", idxs, out)
		}
		between := out[idxs[i-1]:idxs[i]]
		if !strings.Contains(between, "\n") {
			t.Errorf("no newline between %q and %q:\n%s",
				out[idxs[i-1]:idxs[i-1]+10], out[idxs[i]:idxs[i]+10], out)
		}
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

// countingRenderer wraps a real glamour renderer and counts how many times
// Render is called so cache tests can assert reuse.
type countingRenderer struct {
	inner mdRenderer
	calls int
}

func (r *countingRenderer) Render(s string) (string, error) {
	r.calls++
	return r.inner.Render(s)
}

func TestChatCacheReusesRenderForUnchangedEntries(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	cr := &countingRenderer{inner: c.renderer}
	c.renderer = cr

	c.appendAgentDeltaBlock("first answer")
	first := cr.calls
	if first == 0 {
		t.Fatalf("expected at least one render call for the first entry, got 0")
	}

	c.appendAgentDeltaBlock("second answer")
	// Only the new entry should have been rendered; the first one is cached.
	if cr.calls != first+1 {
		t.Errorf("render calls = %d, want %d (one new entry only)", cr.calls, first+1)
	}
}

func TestChatInvalidateRenderCacheClearsAllEntries(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	cr := &countingRenderer{inner: c.renderer}
	c.renderer = cr

	c.appendUser("hi")
	c.appendAgentDeltaBlock("hello")
	// Both entries are now rendered (refresh was called during append).
	callsAfterAppend := cr.calls

	// A second call to renderEntries must not re-render (cache is warm).
	_ = c.renderEntries()
	if cr.calls != callsAfterAppend {
		t.Fatalf("unexpected re-render before invalidate: calls went from %d to %d",
			callsAfterAppend, cr.calls)
	}

	// invalidateRenderCache must cause both entries to be re-rendered.
	c.invalidateRenderCache()
	if cr.calls <= callsAfterAppend {
		t.Errorf("invalidateRenderCache did not trigger re-render: calls = %d, want > %d",
			cr.calls, callsAfterAppend)
	}
}

func TestChatCacheInvalidatedOnResize(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendAgentDeltaBlock("hello world")
	before := c.entries[0].rendered
	if before == "" {
		t.Fatalf("entry cache not warmed before resize")
	}

	c.resize(60, 20)
	after := c.entries[0].rendered
	if after == before {
		t.Errorf("entry cache should have been re-rendered after resize")
	}
	if after == "" {
		t.Errorf("entry cache should be re-populated by refresh, got empty")
	}
}

func TestChatCacheInvalidatedOnToolDone(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendToolStart("fs.read")
	_ = c.renderEntries() // warm
	before := c.entries[0].rendered
	if before == "" {
		t.Fatalf("tool entry not cached")
	}

	c.appendToolDone("fs.read", "ok")
	after := c.entries[0].rendered
	if after == before {
		t.Errorf("tool entry cache should change on toolDone (running → done)")
	}
}

func TestChatCacheInvalidatedOnUpsertJobLog(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	started := time.Now()
	c.upsertJobLog("job-1", "fold.esmfold", domain.JobRunning, &started, []string{"line one"})
	_ = c.renderEntries() // warm
	before := c.entries[0].rendered
	if before == "" {
		t.Fatalf("job-log entry not cached")
	}

	c.upsertJobLog("job-1", "fold.esmfold", domain.JobRunning, &started, []string{"line one", "line two"})
	after := c.entries[0].rendered
	if after == before {
		t.Errorf("job-log entry cache should change on tail update")
	}
}

func TestChatAppendToolDoneMatchesByID(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendToolStartWithID("call-a", "fs.read")
	c.appendToolStartWithID("call-b", "knowledge.uniprot")

	// Complete the second one first.
	c.appendToolDoneWithID("call-b", "knowledge.uniprot", "ok B")

	if !c.entries[1].done {
		t.Errorf("entry for call-b should be done")
	}
	if c.entries[0].done {
		t.Errorf("entry for call-a should still be running")
	}
}

func TestChatCacheStreamingHotPathIsOPerEntry(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	cr := &countingRenderer{inner: c.renderer}
	c.renderer = cr

	// First streaming token creates the entry; subsequent deltas append to it.
	c.appendAgentDelta("tok-0 ")
	baseline := cr.calls
	if baseline == 0 {
		t.Fatalf("first token did not render the entry")
	}

	// Stream 49 more tokens into the same entry.
	for i := 1; i < 50; i++ {
		c.appendAgentDelta("tok-" + fmt.Sprint(i) + " ")
	}

	// Each delta invalidates that one entry's cache and triggers exactly one
	// glamour Render call. Other entries (none, here) stay cached.
	// Total renders should be 50 (one per delta on the same entry), not >50.
	if cr.calls != baseline+49 {
		t.Errorf("streaming renders = %d, want %d (one per delta)",
			cr.calls, baseline+49)
	}

	// Now append a second, separate entry. That should cost exactly one
	// additional render (the new entry); the first entry stays cached.
	c.appendAgentDeltaBlock("a separate block")
	if cr.calls != baseline+49+1 {
		t.Errorf("new entry rendered = %d, want %d (one new entry only)",
			cr.calls, baseline+49+1)
	}
}
