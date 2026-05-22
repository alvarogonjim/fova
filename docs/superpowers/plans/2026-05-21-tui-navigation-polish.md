# TUI Navigation Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the fova chat scrollable with the mouse, make the jobs/designs/wet-lab side panels focusable with full-screen detail views, and fix `/clear` so the panels stop disappearing.

**Architecture:** All work is in the Bubble Tea TUI (`internal/tui`) plus two one-line changes in `cmd/fova`. The chat viewport gains mouse-wheel routing and a "follow only when at bottom" refresh. Each panel model gains a selection cursor and a `focused` flag. The existing single-job-log full-screen view is generalised into one `detailView` driven by three pure body-renderers. No store, agent-loop, or job-execution changes.

**Tech Stack:** Go 1.22, `charmbracelet/bubbletea` v1.3.10, `charmbracelet/bubbles` v1.0.0 (viewport, textarea), `charmbracelet/lipgloss` v1.1.x.

**Spec:** `docs/superpowers/specs/2026-05-21-tui-navigation-polish-design.md`

**Branch:** `feat/tui-nav-polish` (already created, spec already committed there).

---

## File Structure

**Modified**
- `cmd/fova/main.go` — enable mouse capture.
- `cmd/fova/replay.go` — enable mouse capture.
- `internal/tui/chat.go` — mouse forwarding; follow-only-at-bottom refresh.
- `internal/tui/statusbar.go` — "scrolled up" footer hint.
- `internal/tui/app.go` — `tea.MouseMsg` case; `/clear` layout fix; focus ring; focused-panel key routing; detail-overlay wiring; live refresh.
- `internal/tui/commandbar.go` — `setActive` + dimmed inactive input.
- `internal/tui/jobs.go` — selection cursor, `focused` flag, `panelHeader` helper.
- `internal/tui/designs.go` — selection cursor, `focused` flag.
- `internal/tui/lab.go` — selection cursor, `focused` flag.
- `internal/tui/keybindings.go` — refreshed bindings table.

**Renamed**
- `internal/tui/joblog.go` → `internal/tui/detail.go` (`jobLogView`→`detailView`); gains the three `render*Detail` body builders.
- `internal/tui/joblog_test.go` → `internal/tui/detail_test.go`.

## Task Dependency Notes (for parallel execution)

- Tasks 1 → 2 → 3 are sequential (all touch `app.go` / `chat.go`).
- Task 4 (rename) must complete before Task 5.
- **Tasks 5, 6, 7, 8 touch four disjoint files (`detail.go`, `jobs.go`, `designs.go`, `lab.go`) and can run fully in parallel** once Task 4 is done.
- Task 9 needs Tasks 6, 7, 8. Task 10 needs Tasks 5 and 9.

---

## Task 1: Enable the mouse and route the wheel to the chat

**Files:**
- Modify: `cmd/fova/main.go:166`
- Modify: `cmd/fova/replay.go:99`
- Modify: `internal/tui/chat.go` (imports + new `handleMouse` method)
- Modify: `internal/tui/app.go` (new `tea.MouseMsg` case in `Update`)
- Test: `internal/tui/chat_test.go`, `internal/tui/app_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/tui/chat_test.go`, ensure the imports include `"fmt"` and `tea "github.com/charmbracelet/bubbletea"`, then add:

```go
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
```

In `internal/tui/app_test.go` add (its imports already have `tea` and `time`; add `"fmt"` if absent):

```go
func TestAppMouseWheelScrollsChat(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	for i := 0; i < 40; i++ {
		m.chat.appendAgentDeltaBlock(fmt.Sprintf("entry %d", i))
	}
	m.chat.viewport.GotoBottom()
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if m.chat.viewport.AtBottom() {
		t.Error("a MouseMsg wheel-up should scroll the chat")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'MouseWheel' -v`
Expected: FAIL — `c.handleMouse undefined` (compile error).

- [ ] **Step 3: Add `handleMouse` to the chat model**

In `internal/tui/chat.go`, add `tea "github.com/charmbracelet/bubbletea"` to the import block, then add this method (place it next to `View`):

```go
// handleMouse forwards a mouse event to the chat viewport. The viewport's
// built-in MouseWheelEnabled handling scrolls it on wheel-up / wheel-down;
// non-wheel events (clicks, motion) are ignored by the viewport.
func (c *chatModel) handleMouse(msg tea.MouseMsg) {
	c.viewport, _ = c.viewport.Update(msg)
}
```

- [ ] **Step 4: Add the `tea.MouseMsg` case to `Update`**

In `internal/tui/app.go`, inside the `Update` `switch`, add this case immediately after the `case spinnerTickMsg:` block:

```go
	case tea.MouseMsg:
		m.chat.handleMouse(msg)
		return m, nil
```

(The wheel scrolls the chat only — panels stay keyboard-driven. `MouseMsg` is not a bus message, so no `waitForBus()`.)

- [ ] **Step 5: Enable mouse capture in both programs**

In `cmd/fova/main.go:166` change:

```go
	p := tea.NewProgram(app, tea.WithAltScreen())
```
to
```go
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
```

Make the identical change in `cmd/fova/replay.go:99`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'MouseWheel' -v`
Expected: PASS (both tests).
Run: `go build ./...`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add cmd/fova/main.go cmd/fova/replay.go internal/tui/chat.go internal/tui/app.go internal/tui/chat_test.go internal/tui/app_test.go
git commit -m "$(cat <<'EOF'
feat(tui): enable mouse-wheel scrolling for the chat

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Preserve the scroll position; add the "scrolled up" footer hint

**Files:**
- Modify: `internal/tui/chat.go` (`refresh`, `appendUser`, new `atBottom`)
- Modify: `internal/tui/statusbar.go` (`chatScrolledUp` field, `footerView`)
- Modify: `internal/tui/app.go` (`View` sets `chatScrolledUp`)
- Test: `internal/tui/chat_test.go`, `internal/tui/statusbar_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/tui/chat_test.go` add:

```go
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
```

In `internal/tui/statusbar_test.go` add (ensure `"strings"` and `"testing"` are imported):

```go
func TestFooterShowsScrolledUpHint(t *testing.T) {
	s := newStatusBarModel(NewTheme())
	s.width = 200
	if strings.Contains(s.footerView(), "End for latest") {
		t.Error("footer must not show the hint when the chat is at the bottom")
	}
	s.chatScrolledUp = true
	if !strings.Contains(s.footerView(), "End for latest") {
		t.Error("footer should show the scrolled-up hint")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'ChatRefresh|AppendUserJumps|ScrolledUpHint' -v`
Expected: FAIL — `s.chatScrolledUp undefined`, and the refresh tests fail because `refresh` still calls `GotoBottom` unconditionally.

- [ ] **Step 3: Make `refresh` follow only when at the bottom**

In `internal/tui/chat.go`, replace the `refresh` method:

```go
func (c *chatModel) refresh() {
	follow := c.viewport.AtBottom()
	c.viewport.SetContent(c.renderEntries())
	if follow {
		c.viewport.GotoBottom()
	}
}

// atBottom reports whether the chat is scrolled to the latest entry.
func (c *chatModel) atBottom() bool { return c.viewport.AtBottom() }
```

Replace `appendUser` so sending a message always jumps to the latest:

```go
func (c *chatModel) appendUser(text string) {
	c.entries = append(c.entries, chatEntry{kind: entryUser, text: text})
	c.refresh()
	c.viewport.GotoBottom()
}
```

- [ ] **Step 4: Add the footer hint**

In `internal/tui/statusbar.go`, add a field to `statusBarModel` (after `replay string`):

```go
	chatScrolledUp bool // chat viewport is not at the bottom — show a hint
```

Replace `footerView`:

```go
func (s statusBarModel) footerView() string {
	hint := footerHintText
	if s.replay != "" {
		hint += "  ·  " + s.replay
	}
	if s.chatScrolledUp {
		hint += "  ·  ↓ End for latest"
	}
	if s.width > 0 {
		hint = clipRunes(hint, s.width)
	}
	return s.theme.Footer.Render(hint)
}
```

- [ ] **Step 5: Feed the chat scroll state into the footer**

In `internal/tui/app.go`, in `View`, add this line immediately after the `if m.width == 0 { return "starting fova…" }` guard:

```go
	m.status.chatScrolledUp = !m.chat.atBottom()
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'ChatRefresh|AppendUserJumps|ScrolledUpHint' -v`
Expected: PASS.
Run: `go test ./internal/tui/`
Expected: PASS (no regressions).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/chat.go internal/tui/statusbar.go internal/tui/app.go internal/tui/chat_test.go internal/tui/statusbar_test.go
git commit -m "$(cat <<'EOF'
feat(tui): keep chat scroll position; add scrolled-up footer hint

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Fix the `/clear` panel disappearance

**Files:**
- Modify: `internal/tui/app.go` (`runSlashCommand` "clear" case)
- Test: `internal/tui/app_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/tui/app_test.go` add:

```go
func TestClearKeepsPanelsVisible(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.focus = focusJobs
	m.runSlashCommand("clear", "")
	wantChatW := 120 - 38 - 2 // full width minus the panel column and gap
	if m.chat.width != wantChatW {
		t.Errorf("after /clear chat width = %d, want %d (panels pushed off-screen)", m.chat.width, wantChatW)
	}
	if m.focus != focusChat {
		t.Error("/clear should return focus to the chat")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'ClearKeepsPanels' -v`
Expected: FAIL — `m.chat.width` is 120 (full width), not 80, because `/clear` rebuilds the chat at `chatWidth()` (full width) and never re-layouts.

- [ ] **Step 3: Add the layout call to `/clear`**

In `internal/tui/app.go`, in `runSlashCommand`, replace the `case "clear":` block:

```go
	case "clear":
		m.chat = newChatModel(m.theme, m.chatWidth(), m.chatHeight())
		m.session = agent.NewSession(m.systemPrompt)
		m.beginPersistedSession()
		m.focus = focusChat // reset focus to the chat
		m.layout()          // re-size the chat for the panel column
		return m, nil
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run 'ClearKeepsPanels' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "$(cat <<'EOF'
fix(tui): /clear no longer pushes the side panels off-screen

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Rename `jobLogView` → `detailView` (mechanical, no behaviour change)

This is a pure rename so the next tasks can build the generalised detail view. Behaviour is unchanged; the existing tests are the regression check.

**Files:**
- Rename: `internal/tui/joblog.go` → `internal/tui/detail.go`
- Rename: `internal/tui/joblog_test.go` → `internal/tui/detail_test.go`
- Modify: `internal/tui/app.go` (every reference to the renamed identifiers)

- [ ] **Step 1: Move the files with git**

```bash
git mv internal/tui/joblog.go internal/tui/detail.go
git mv internal/tui/joblog_test.go internal/tui/detail_test.go
```

- [ ] **Step 2: Apply the identifier rename across the package**

Apply this exact mapping in `internal/tui/detail.go`, `internal/tui/detail_test.go`, and `internal/tui/app.go` (every occurrence):

| Old | New |
|---|---|
| `jobLogView` (type) | `detailView` |
| `newJobLogView` | `newDetailView` |
| `m.jobLog` (Model field) | `m.detail` |
| `jobLog jobLogView` (struct field decl in `Model`) | `detail detailView` |
| `jobLogID` (Model field) | `detailID` |
| `overlayJobLog` (const) | `overlayDetail` |
| `openJobLog` (method) | `openDetail` |

Do **not** change `readLog`, `tailLines`, `refreshJobLogs`, or any `entryJobLog` / `upsertJobLog` / `renderJobLogEntry` identifier — those belong to the separate in-chat job-log feature and keep their names. The `openDetail` method keeps its current `(id string)` signature and job-only body for now (Task 10 replaces the body).

- [ ] **Step 3: Verify the rename compiles and behaviour is unchanged**

Run: `go build ./...`
Expected: no errors.
Run: `go test ./internal/tui/`
Expected: PASS — every pre-existing test still passes (this is the regression check; no test was added or removed).

- [ ] **Step 4: Commit**

```bash
git add internal/tui/detail.go internal/tui/detail_test.go internal/tui/app.go
git commit -m "$(cat <<'EOF'
refactor(tui): rename jobLogView to detailView

Mechanical rename ahead of generalising the full-screen view to
designs and experiments. No behaviour change.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Detail-body renderers

Three pure functions that build the `(header, body)` strings for the detail view. Independent of `app.go` — can run in parallel with Tasks 6/7/8.

**Files:**
- Modify: `internal/tui/detail.go` (add renderers + helpers)
- Test: `internal/tui/detail_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/tui/detail_test.go`, ensure imports include `"strings"`, `"testing"`, `"time"`, and `"github.com/alvarogonjim/fova/internal/domain"`, then add:

```go
func TestRenderJobDetailRunning(t *testing.T) {
	started := time.Now().Add(-2 * time.Minute)
	j := domain.Job{
		ID: "job-abc", Tool: "design.bindcraft", Status: domain.JobRunning,
		Backend: "modal", Progress: 0.5, Created: time.Now(), Started: &started,
	}
	header, body := renderJobDetail(NewTheme(), j)
	if !strings.Contains(header, "design.bindcraft") || !strings.Contains(header, "job-abc") {
		t.Errorf("header missing job identity: %q", header)
	}
	for _, want := range []string{"status", "backend", "modal", "log"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}

func TestRenderJobDetailFailedShowsError(t *testing.T) {
	j := domain.Job{ID: "j", Tool: "t", Status: domain.JobFailed, Error: "boom", Created: time.Now()}
	_, body := renderJobDetail(NewTheme(), j)
	if !strings.Contains(body, "error") || !strings.Contains(body, "boom") {
		t.Errorf("failed job body should show the error: %q", body)
	}
}

func TestRenderDesignDetail(t *testing.T) {
	d := domain.Design{
		ID: "d-1", Origin: domain.OriginBindCraft, Application: domain.AppBinder,
		Created: time.Now(),
		Sequence: domain.Sequence{Chains: map[string]string{"A": "MKTAYIAKQR"}},
		Scores:   map[string]float64{"ipsae": 0.71, "plddt_mean": 88.4},
	}
	header, body := renderDesignDetail(NewTheme(), d)
	if !strings.Contains(header, "d-1") {
		t.Errorf("header missing design id: %q", header)
	}
	for _, want := range []string{"scores", "ipsae", "sequence", "MKTAYIAKQR", "provenance"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}

func TestRenderExperimentDetailNoResults(t *testing.T) {
	e := domain.Experiment{ID: "e1", TargetName: "PD-L1", AssayType: "binding", Status: "in_progress"}
	_, body := renderExperimentDetail(NewTheme(), e)
	if !strings.Contains(body, "no results yet") {
		t.Errorf("an experiment with no results should say so: %q", body)
	}
}

func TestRenderExperimentDetailWithResults(t *testing.T) {
	kd := 12.0
	e := domain.Experiment{
		ID: "e1", TargetName: "PD-L1", AssayType: "binding", Status: "done",
		Results: []domain.ExperimentResult{
			{DesignID: "d-1", Kd: &kd, KdUnits: "nM", BindingStrength: "strong"},
		},
	}
	_, body := renderExperimentDetail(NewTheme(), e)
	for _, want := range []string{"results", "d-1", "strong"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'RenderJobDetail|RenderDesignDetail|RenderExperimentDetail' -v`
Expected: FAIL — `renderJobDetail` etc. undefined (compile error).

- [ ] **Step 3: Add the renderers and helpers to `detail.go`**

In `internal/tui/detail.go`, add `"fmt"` and `"sort"` to the import block and `"github.com/alvarogonjim/fova/internal/domain"`. Append:

```go
// renderJobDetail builds the full-screen detail view for a job — a header
// line plus a metadata + log body. It works for any job status.
func renderJobDetail(th Theme, j domain.Job) (header, body string) {
	header = glyph(j.Status) + " " + j.Tool + " · " + string(j.ID) + " · " + string(j.Status)

	var b strings.Builder
	fmt.Fprintf(&b, " status     %-16s kind     %s\n", j.Status, j.Kind)
	fmt.Fprintf(&b, " backend    %-16s cost     $%.2f\n", orDash(j.Backend), j.CostUSD)
	fmt.Fprintf(&b, " created    %s\n", j.Created.Format("15:04:05"))
	if j.Started != nil {
		fmt.Fprintf(&b, " started    %s\n", j.Started.Format("15:04:05"))
	}
	if j.Finished != nil {
		fmt.Fprintf(&b, " finished   %s\n", j.Finished.Format("15:04:05"))
	}
	if elapsed, eta, ok := jobETA(j); ok {
		fmt.Fprintf(&b, " progress   %s  %d%%\n", progressBar(elapsed, eta, 24), int(j.Progress*100))
	}
	if n := len(j.ProducedDesigns); n > 0 {
		fmt.Fprintf(&b, " designs    %d produced\n", n)
	} else {
		b.WriteString(" designs    none yet\n")
	}
	if j.Status == domain.JobFailed && j.Error != "" {
		b.WriteString("\n")
		b.WriteString(th.Error.Render(" error ─────────────────────────────────") + "\n")
		b.WriteString(th.Error.Render(" "+j.Error) + "\n")
	}
	b.WriteString("\n" + th.SectionRule.Render(" log ───────────────────────────────────") + "\n")
	log := readLog(j.LogFile)
	if strings.TrimSpace(log) == "" {
		log = "(no output yet)"
	}
	b.WriteString(log)
	return header, b.String()
}

// renderDesignDetail builds the detail view for a design — scores, sequence
// chains, provenance, and lab status.
func renderDesignDetail(th Theme, d domain.Design) (header, body string) {
	header = string(d.ID) + " · " + string(d.Origin) + " · " + string(d.Application)
	if isShortlisted(d) {
		header += "    ★ shortlisted"
	}

	var b strings.Builder
	fmt.Fprintf(&b, " created    %s\n", d.Created.Format("15:04:05"))
	fmt.Fprintf(&b, " structure  %s\n", orDash(d.StructureFile))
	if len(d.Tags) > 0 {
		fmt.Fprintf(&b, " tags       %s\n", strings.Join(d.Tags, ", "))
	}

	b.WriteString("\n" + th.SectionRule.Render(" scores ────────────────────────────────") + "\n")
	if len(d.Scores) == 0 {
		b.WriteString(" (none)\n")
	} else {
		for _, k := range sortedScoreKeys(d.Scores) {
			fmt.Fprintf(&b, " %-16s %.2f\n", k, d.Scores[k])
		}
	}

	b.WriteString("\n" + th.SectionRule.Render(" sequence ──────────────────────────────") + "\n")
	b.WriteString(renderSequenceChains(d.Sequence))
	b.WriteString("\n")

	b.WriteString("\n" + th.SectionRule.Render(" provenance ────────────────────────────") + "\n")
	if len(d.Provenance) == 0 {
		b.WriteString(" (none)\n")
	} else {
		for _, p := range d.Provenance {
			fmt.Fprintf(&b, " %-14s %-8s %s  #%s\n",
				p.Tool, p.Version, p.Timestamp.Format("15:04"), shortHash(p.InputHash))
		}
	}

	lab := "not submitted"
	if len(d.LabResults) > 0 {
		lab = fmt.Sprintf("%d result(s)", len(d.LabResults))
	}
	fmt.Fprintf(&b, "\n lab        %s\n", lab)
	if d.Notes != "" {
		fmt.Fprintf(&b, " notes      %s\n", d.Notes)
	}
	return header, b.String()
}

// renderExperimentDetail builds the detail view for a wet-lab experiment —
// metadata plus a per-design results table.
func renderExperimentDetail(th Theme, e domain.Experiment) (header, body string) {
	header = orDash(e.TargetName) + " · " + orDash(e.AssayType) + " · " + orDash(e.Status)

	var b strings.Builder
	fmt.Fprintf(&b, " backend    %-16s external %s\n", orDash(e.Backend), orDash(e.ExternalID))
	fmt.Fprintf(&b, " submitted  %-16s cost     $%.2f\n", e.SubmittedAt.Format("Jan 2 15:04"), e.CostUSD)
	fmt.Fprintf(&b, " designs    %d submitted\n", len(e.Designs))

	b.WriteString("\n" + th.SectionRule.Render(" results ───────────────────────────────") + "\n")
	if len(e.Results) == 0 {
		b.WriteString(" no results yet\n")
		return header, b.String()
	}
	fmt.Fprintf(&b, " %-14s %-12s %-12s %s\n", "design", "Kd", "binding", "R²")
	for _, r := range e.Results {
		kd := "—"
		if r.Kd != nil {
			kd = fmt.Sprintf("%.3g %s", *r.Kd, r.KdUnits)
		}
		rsq := "—"
		if r.RSquared != nil {
			rsq = fmt.Sprintf("%.2f", *r.RSquared)
		}
		fmt.Fprintf(&b, " %-14s %-12s %-12s %s\n",
			shortID(string(r.DesignID)), kd, orDash(r.BindingStrength), rsq)
	}
	return header, b.String()
}

// sortedScoreKeys returns a design's score keys in deterministic order.
func sortedScoreKeys(scores map[string]float64) []string {
	keys := make([]string, 0, len(scores))
	for k := range scores {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// shortHash truncates a provenance input hash for compact display.
func shortHash(h string) string {
	if len(h) > 6 {
		return h[:6]
	}
	return h
}

// renderSequenceChains formats every chain of a design sequence in 10-residue
// groups, labelled by chain id. domain.Sequence is multi-chain, so each chain
// is shown separately.
func renderSequenceChains(seq domain.Sequence) string {
	if len(seq.Chains) == 0 {
		return " (no sequence)"
	}
	ids := make([]string, 0, len(seq.Chains))
	for id := range seq.Chains {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteString("\n")
		}
		chain := seq.Chains[id]
		fmt.Fprintf(&b, " chain %s (%d aa)\n", id, len(chain))
		b.WriteString(wrapResidues(chain))
	}
	return b.String()
}

// wrapResidues groups a chain into 10-residue blocks, five blocks per line.
func wrapResidues(seq string) string {
	if seq == "" {
		return "  (empty)"
	}
	var b strings.Builder
	for i := 0; i < len(seq); i += 10 {
		end := i + 10
		if end > len(seq) {
			end = len(seq)
		}
		if i%50 == 0 && i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(" " + seq[i:end])
	}
	return b.String()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'RenderJobDetail|RenderDesignDetail|RenderExperimentDetail' -v`
Expected: PASS (all five tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/detail.go internal/tui/detail_test.go
git commit -m "$(cat <<'EOF'
feat(tui): detail-view body renderers for jobs, designs, experiments

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Jobs panel selection cursor

**Files:**
- Modify: `internal/tui/jobs.go`
- Test: `internal/tui/jobs_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/tui/jobs_test.go` add:

```go
func TestJobsSelectionMovesAndClamps(t *testing.T) {
	m := newJobsModel(NewTheme())
	m.setJobs([]domain.Job{{ID: "j1"}, {ID: "j2"}, {ID: "j3"}})
	if m.selected != 0 {
		t.Fatalf("selection starts at 0, got %d", m.selected)
	}
	m.selectUp() // clamps at the top
	if m.selected != 0 {
		t.Errorf("selectUp at top: selected = %d, want 0", m.selected)
	}
	m.selectDown()
	m.selectDown()
	m.selectDown() // clamps at the bottom
	if m.selected != 2 {
		t.Errorf("selectDown past end: selected = %d, want 2", m.selected)
	}
}

func TestJobsSelectedJob(t *testing.T) {
	m := newJobsModel(NewTheme())
	if _, ok := m.selectedJob(); ok {
		t.Error("an empty panel has no selected job")
	}
	m.setJobs([]domain.Job{{ID: "j1"}, {ID: "j2"}})
	m.selectDown()
	j, ok := m.selectedJob()
	if !ok || j.ID != "j2" {
		t.Errorf("selectedJob = %v, %v; want j2, true", j.ID, ok)
	}
}

func TestJobsSetJobsReclampsSelection(t *testing.T) {
	m := newJobsModel(NewTheme())
	m.setJobs([]domain.Job{{ID: "j1"}, {ID: "j2"}, {ID: "j3"}})
	m.selectDown()
	m.selectDown() // selected == 2
	m.setJobs([]domain.Job{{ID: "j1"}}) // list shrank
	if m.selected != 0 {
		t.Errorf("after the list shrank, selected = %d, want 0", m.selected)
	}
}

func TestJobsFocusedRowHighlight(t *testing.T) {
	m := newJobsModel(NewTheme())
	m.setWidth(38)
	m.setJobs([]domain.Job{{ID: "j1", Tool: "a", Status: domain.JobRunning}})
	if strings.Contains(m.View(), "▸") {
		t.Error("an unfocused panel must not show the selection marker")
	}
	m.setFocused(true)
	if !strings.Contains(m.View(), "▸") {
		t.Error("a focused panel should mark the selected row")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'JobsSelection|JobsSelectedJob|JobsSetJobsReclamps|JobsFocusedRow' -v`
Expected: FAIL — `m.selected`, `m.selectUp`, `m.selectedJob`, `m.setFocused` undefined.

- [ ] **Step 3: Add selection state to `jobsModel`**

In `internal/tui/jobs.go`, add `"github.com/charmbracelet/lipgloss"` to the import block. Add two fields to the `jobsModel` struct (after `width int`):

```go
	focused  bool // this panel currently holds keyboard focus
	selected int  // highlighted row index, clamped to [0, len-1]
```

Replace `setJobs` and add the selection methods:

```go
// setJobs replaces the panel's jobs, re-clamping the selection cursor.
func (m *jobsModel) setJobs(jobs []domain.Job) {
	m.jobs = jobs
	m.clampSelection()
}

// setFocused records whether this panel currently holds keyboard focus.
func (m *jobsModel) setFocused(f bool) { m.focused = f }

// clampSelection keeps selected within [0, len-1] (0 when the panel is empty).
func (m *jobsModel) clampSelection() {
	if m.selected >= len(m.jobs) {
		m.selected = len(m.jobs) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// selectUp / selectDown move the selection cursor and clamp it.
func (m *jobsModel) selectUp()   { m.selected--; m.clampSelection() }
func (m *jobsModel) selectDown() { m.selected++; m.clampSelection() }

// selectedJob returns the highlighted job, or false when the panel is empty.
func (m *jobsModel) selectedJob() (domain.Job, bool) {
	if len(m.jobs) == 0 {
		return domain.Job{}, false
	}
	m.clampSelection()
	return m.jobs[m.selected], true
}
```

- [ ] **Step 4: Add the shared `panelHeader` helper**

In `internal/tui/jobs.go`, add this helper (it is used by all three panels — Tasks 7 and 8 reuse it):

```go
// panelHeader renders a panel's section-rule header. A focused panel's header
// is recoloured to the theme accent so the focus is visible at a glance.
func panelHeader(label string, width int, th Theme, focused bool) string {
	label = strings.ToLower(label)
	line := label + " "
	if pad := width - len([]rune(line)); pad > 0 {
		line += strings.Repeat("─", pad)
	}
	style := th.SectionRule
	if focused {
		style = lipgloss.NewStyle().Foreground(th.Palette.Accent)
	}
	return style.Render(clipLine(line, width))
}
```

- [ ] **Step 5: Render the focus header and the selected row**

In `internal/tui/jobs.go`, replace the `View` method:

```go
// View renders the jobs panel. When focused, the header is accent-coloured
// and the selected row is marked with a saffron "▸".
func (m jobsModel) View() string {
	var b strings.Builder
	b.WriteString(panelHeader("jobs", m.width, m.theme, m.focused))
	b.WriteString("\n")
	if len(m.jobs) == 0 {
		b.WriteString(m.theme.Subtle.Render(wrapText(
			"no jobs yet · /install a tool or ask the agent to design", m.width)))
		return b.String()
	}
	accent := lipgloss.NewStyle().Foreground(m.theme.Palette.Accent)
	for i, j := range m.jobs {
		line := fmt.Sprintf("%-16s %s %s", j.Tool, shortID(string(j.ID)), jobTimeInfo(j))
		prefix := m.theme.statusMarker(j.Status) + " "
		rowStyle := m.theme.ToolTrace
		if m.focused && i == m.selected {
			prefix = accent.Render("▸") + " "
			rowStyle = accent
		}
		b.WriteString(prefix + rowStyle.Render(clipLine(line, m.width-2)))
		b.WriteString("\n")
		if elapsed, eta, ok := jobETA(j); ok {
			if bar := progressBar(elapsed, eta, m.width-2); bar != "" {
				b.WriteString("  " + m.theme.SectionRule.Render(bar))
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'Jobs' -v`
Expected: PASS — the new tests and the pre-existing jobs tests (`TestJobsPanelRendersJobs`, `TestJobsPanelEmpty`, `TestJobsSectionRule`).

Note: `TestJobsSectionRule` calls `sectionRule(...)` directly — that function is unchanged and still present, so the test still passes.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/jobs.go internal/tui/jobs_test.go
git commit -m "$(cat <<'EOF'
feat(tui): selection cursor and focus highlight for the jobs panel

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Designs panel selection cursor

**Files:**
- Modify: `internal/tui/designs.go`
- Test: `internal/tui/designs_test.go`

Depends on Task 6 only for the `panelHeader` helper. Run after Task 6, or in parallel if the executor merges the helper first.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/designs_test.go` add (ensure `"strings"` and `domain` are imported):

```go
func TestDesignsSelectionMovesAndClamps(t *testing.T) {
	m := newDesignsModel(NewTheme())
	m.setDesigns([]domain.Design{{ID: "d1"}, {ID: "d2"}, {ID: "d3"}})
	m.selectUp()
	if m.selected != 0 {
		t.Errorf("selectUp at top: selected = %d, want 0", m.selected)
	}
	m.selectDown()
	m.selectDown()
	m.selectDown()
	if m.selected != 2 {
		t.Errorf("selectDown past end: selected = %d, want 2", m.selected)
	}
}

func TestDesignsSelectedDesign(t *testing.T) {
	m := newDesignsModel(NewTheme())
	if _, ok := m.selectedDesign(); ok {
		t.Error("an empty panel has no selected design")
	}
	m.setDesigns([]domain.Design{{ID: "d1"}, {ID: "d2"}})
	m.selectDown()
	d, ok := m.selectedDesign()
	if !ok || d.ID != "d2" {
		t.Errorf("selectedDesign = %v, %v; want d2, true", d.ID, ok)
	}
}

func TestDesignsSetDesignsReclampsSelection(t *testing.T) {
	m := newDesignsModel(NewTheme())
	m.setDesigns([]domain.Design{{ID: "d1"}, {ID: "d2"}, {ID: "d3"}})
	m.selectDown()
	m.selectDown()
	m.setDesigns([]domain.Design{{ID: "d1"}})
	if m.selected != 0 {
		t.Errorf("after the list shrank, selected = %d, want 0", m.selected)
	}
}

func TestDesignsFocusedRowHighlight(t *testing.T) {
	m := newDesignsModel(NewTheme())
	m.setWidth(38)
	m.setDesigns([]domain.Design{{ID: "d1"}})
	if strings.Contains(m.View(), "▸") {
		t.Error("an unfocused panel must not show the selection marker")
	}
	m.setFocused(true)
	if !strings.Contains(m.View(), "▸") {
		t.Error("a focused panel should mark the selected row")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'DesignsSelection|DesignsSelectedDesign|DesignsSetDesignsReclamps|DesignsFocusedRow' -v`
Expected: FAIL — selection symbols undefined.

- [ ] **Step 3: Add selection state to `designsModel`**

In `internal/tui/designs.go`, add two fields to the `designsModel` struct (after `width int`):

```go
	focused  bool // this panel currently holds keyboard focus
	selected int  // highlighted row index, clamped to [0, len-1]
```

Replace `setDesigns` and add the selection methods:

```go
// setDesigns replaces the panel's designs, re-clamping the selection cursor.
func (m *designsModel) setDesigns(designs []domain.Design) {
	m.designs = designs
	m.clampSelection()
}

// setFocused records whether this panel currently holds keyboard focus.
func (m *designsModel) setFocused(f bool) { m.focused = f }

// clampSelection keeps selected within [0, len-1] (0 when the panel is empty).
func (m *designsModel) clampSelection() {
	if m.selected >= len(m.designs) {
		m.selected = len(m.designs) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// selectUp / selectDown move the selection cursor and clamp it.
func (m *designsModel) selectUp()   { m.selected--; m.clampSelection() }
func (m *designsModel) selectDown() { m.selected++; m.clampSelection() }

// selectedDesign returns the highlighted design, or false when empty.
func (m *designsModel) selectedDesign() (domain.Design, bool) {
	if len(m.designs) == 0 {
		return domain.Design{}, false
	}
	m.clampSelection()
	return m.designs[m.selected], true
}
```

- [ ] **Step 4: Render the focus header and the selected row**

In `internal/tui/designs.go`, in `View`, replace the header line:

```go
	b.WriteString(RenderSectionRule(m.theme,
		fmt.Sprintf("designs · %d", len(m.designs)), m.width, false))
```
with
```go
	b.WriteString(panelHeader(
		fmt.Sprintf("designs · %d", len(m.designs)), m.width, m.theme, m.focused))
```

Then change the row loop from `for _, d := range m.designs {` to `for i, d := range m.designs {` and, at the start of the loop body (before the `if isShortlisted(d)` branch), add the focused-selection branch:

```go
		if m.focused && i == m.selected {
			accent := lipgloss.NewStyle().Foreground(m.theme.Palette.Accent)
			line := fmt.Sprintf("%-11s %6s %6s %6s %3s",
				id, plddt, ipsae, iptm, lab)
			b.WriteString(accent.Render("▸ " + clipLine(line, m.width-2)))
			b.WriteString("\n")
			continue
		}
```

Place this branch immediately after the `id`, `plddt`, `ipsae`, `iptm`, `lab` variables are computed and before the existing `if isShortlisted(d) {` block, so a focused selected row always wins. (`lipgloss` is already imported in `designs.go`.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'Designs' -v`
Expected: PASS — new tests plus the pre-existing designs tests.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/designs.go internal/tui/designs_test.go
git commit -m "$(cat <<'EOF'
feat(tui): selection cursor and focus highlight for the designs panel

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Wet-lab panel selection cursor

**Files:**
- Modify: `internal/tui/lab.go`
- Test: `internal/tui/lab_test.go`

Depends on Task 6 only for the `panelHeader` helper.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/lab_test.go` add (ensure `"strings"` and `domain` are imported):

```go
func TestLabSelectionMovesAndClamps(t *testing.T) {
	m := newLabModel(NewTheme())
	m.setExperiments([]domain.Experiment{{ID: "e1"}, {ID: "e2"}, {ID: "e3"}})
	m.selectUp()
	if m.selected != 0 {
		t.Errorf("selectUp at top: selected = %d, want 0", m.selected)
	}
	m.selectDown()
	m.selectDown()
	m.selectDown()
	if m.selected != 2 {
		t.Errorf("selectDown past end: selected = %d, want 2", m.selected)
	}
}

func TestLabSelectedExperiment(t *testing.T) {
	m := newLabModel(NewTheme())
	if _, ok := m.selectedExperiment(); ok {
		t.Error("an empty panel has no selected experiment")
	}
	m.setExperiments([]domain.Experiment{{ID: "e1"}, {ID: "e2"}})
	m.selectDown()
	e, ok := m.selectedExperiment()
	if !ok || e.ID != "e2" {
		t.Errorf("selectedExperiment = %v, %v; want e2, true", e.ID, ok)
	}
}

func TestLabSetExperimentsReclampsSelection(t *testing.T) {
	m := newLabModel(NewTheme())
	m.setExperiments([]domain.Experiment{{ID: "e1"}, {ID: "e2"}, {ID: "e3"}})
	m.selectDown()
	m.selectDown()
	m.setExperiments([]domain.Experiment{{ID: "e1"}})
	if m.selected != 0 {
		t.Errorf("after the list shrank, selected = %d, want 0", m.selected)
	}
}

func TestLabFocusedRowHighlight(t *testing.T) {
	m := newLabModel(NewTheme())
	m.setWidth(38)
	m.setExperiments([]domain.Experiment{{ID: "e1"}})
	if strings.Contains(m.View(), "▸") {
		t.Error("an unfocused panel must not show the selection marker")
	}
	m.setFocused(true)
	if !strings.Contains(m.View(), "▸") {
		t.Error("a focused panel should mark the selected row")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'LabSelection|LabSelectedExperiment|LabSetExperimentsReclamps|LabFocusedRow' -v`
Expected: FAIL — selection symbols undefined.

- [ ] **Step 3: Add selection state to `labModel`**

In `internal/tui/lab.go`, add `"github.com/charmbracelet/lipgloss"` to the import block. Add two fields to the `labModel` struct (after `width int`):

```go
	focused  bool // this panel currently holds keyboard focus
	selected int  // highlighted row index, clamped to [0, len-1]
```

Replace `setExperiments` and add the selection methods:

```go
// setExperiments replaces the panel's experiments, re-clamping the cursor.
func (m *labModel) setExperiments(exps []domain.Experiment) {
	m.experiments = exps
	m.clampSelection()
}

// setFocused records whether this panel currently holds keyboard focus.
func (m *labModel) setFocused(f bool) { m.focused = f }

// clampSelection keeps selected within [0, len-1] (0 when the panel is empty).
func (m *labModel) clampSelection() {
	if m.selected >= len(m.experiments) {
		m.selected = len(m.experiments) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// selectUp / selectDown move the selection cursor and clamp it.
func (m *labModel) selectUp()   { m.selected--; m.clampSelection() }
func (m *labModel) selectDown() { m.selected++; m.clampSelection() }

// selectedExperiment returns the highlighted experiment, or false when empty.
func (m *labModel) selectedExperiment() (domain.Experiment, bool) {
	if len(m.experiments) == 0 {
		return domain.Experiment{}, false
	}
	m.clampSelection()
	return m.experiments[m.selected], true
}
```

- [ ] **Step 4: Render the focus header and the selected row**

In `internal/tui/lab.go`, replace the `View` method:

```go
// View renders the wet-lab panel. When focused, the header is accent-coloured
// and the selected row is marked with a saffron "▸".
func (m labModel) View() string {
	var b strings.Builder
	b.WriteString(panelHeader("wet-lab", m.width, m.theme, m.focused))
	b.WriteString("\n")
	if len(m.experiments) == 0 {
		b.WriteString(m.theme.Subtle.Render(wrapText(
			"no experiments yet · ask the agent to submit designs to Adaptyv", m.width)))
		return b.String()
	}
	accent := lipgloss.NewStyle().Foreground(m.theme.Palette.Accent)
	for i, e := range m.experiments {
		line := fmt.Sprintf("%s · day %d of ~%d",
			shortID(string(e.ID)), experimentDay(e.SubmittedAt), turnaroundDays)
		if m.focused && i == m.selected {
			b.WriteString(accent.Render("▸ " + clipLine(line, m.width-2)))
		} else {
			b.WriteString("  " + m.theme.AgentText.Render(clipLine(line, m.width-2)))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'Lab' -v`
Expected: PASS — new tests plus the pre-existing lab tests.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/lab.go internal/tui/lab_test.go
git commit -m "$(cat <<'EOF'
feat(tui): selection cursor and focus highlight for the wet-lab panel

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Panel focus ring, key routing, and dimmed input

Makes the side panels focusable on every terminal width. `Enter` on a focused panel is a no-op for now — Task 10 wires it to the detail view.

**Files:**
- Modify: `internal/tui/app.go` (`cycleFocus`, `syncPanelFocus`, focused-panel key routing, `panelSelectUp/Down`, `New`)
- Modify: `internal/tui/commandbar.go` (`active` field, `setActive`, dimmed `View`)
- Modify: `internal/tui/keybindings.go` (bindings table)
- Test: `internal/tui/app_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/tui/app_test.go` add:

```go
func TestTabCyclesPanelFocus(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	want := []panelFocus{focusJobs, focusDesigns, focusLab, focusChat}
	for i, w := range want {
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
		if m.focus != w {
			t.Errorf("Tab #%d: focus = %v, want %v", i+1, m.focus, w)
		}
	}
}

func TestFocusedPanelArrowsMoveSelection(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.jobs.setJobs([]domain.Job{{ID: "j1"}, {ID: "j2"}, {ID: "j3"}})
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus jobs
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.jobs.selected != 1 {
		t.Errorf("after Down, jobs.selected = %d, want 1", m.jobs.selected)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.jobs.selected != 0 {
		t.Errorf("after Up, jobs.selected = %d, want 0", m.jobs.selected)
	}
}

func TestFocusedPanelDimsInput(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus jobs
	if m.cmdbar.active {
		t.Error("the input should be inactive while a panel is focused")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // back to chat
	if !m.cmdbar.active {
		t.Error("Esc should return focus to the chat and reactivate the input")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TabCyclesPanelFocus|FocusedPanelArrows|FocusedPanelDimsInput' -v`
Expected: FAIL — `m.cmdbar.active` undefined; Tab does not cycle through all four states; arrows do not move the selection.

- [ ] **Step 3: Add the `active` flag to the command bar**

In `internal/tui/commandbar.go`, add a field to `commandBarModel` (after `focused bool`):

```go
	active   bool // the input has the keyboard (false while a panel is focused)
```

In `newCommandBarModel`, set `active: true` in the struct literal (alongside `focused: true`).

Add the setter:

```go
// setActive records whether the message input currently has the keyboard.
// While a side panel holds focus the input is inactive and renders dimmed.
func (m *commandBarModel) setActive(a bool) { m.active = a }
```

Replace `View`:

```go
func (m commandBarModel) View() string {
	box := m.inputBorderStyle().Render(m.ta.View())
	label := m.theme.Muted.Render(m.inputLabel())
	if !m.active {
		label = m.theme.Subtle.Render(m.inputLabel() + " · panel focus — esc to type")
	}
	return label + "\n" + box
}
```

- [ ] **Step 4: Replace `cycleFocus` and add the focus helpers**

In `internal/tui/app.go`, replace the entire `cycleFocus` method. The old `cycleFocus` was the only caller of `runningJobIDs`; after this replacement run `grep -rn runningJobIDs internal/tui/` — if the only remaining hit is its own `func` definition, delete that function; if a `_test.go` file also references it, that test asserts the old "running jobs are Tab stops" behaviour which this spec intentionally removes (spec §5.1) — delete that test too. Likewise, if any existing test asserts the old Tab ring (chat → running jobs → panels), update it to the new 4-stop ring or remove it.

```go
// cycleFocus advances the Tab focus ring: chat → jobs → designs → lab → chat.
func (m *Model) cycleFocus() {
	switch m.focus {
	case focusChat:
		m.focus = focusJobs
	case focusJobs:
		m.focus = focusDesigns
	case focusDesigns:
		m.focus = focusLab
	default:
		m.focus = focusChat
	}
	m.syncPanelFocus()
}

// syncPanelFocus pushes m.focus into the panels and the input bar so their
// rendering matches: the focused panel highlights; the input dims whenever a
// panel (not the chat) holds focus.
func (m *Model) syncPanelFocus() {
	m.jobs.setFocused(m.focus == focusJobs)
	m.designs.setFocused(m.focus == focusDesigns)
	m.lab.setFocused(m.focus == focusLab)
	m.cmdbar.setActive(m.focus == focusChat)
}

// panelSelectUp / panelSelectDown move the selection in the focused panel.
func (m *Model) panelSelectUp() {
	switch m.focus {
	case focusJobs:
		m.jobs.selectUp()
	case focusDesigns:
		m.designs.selectUp()
	case focusLab:
		m.lab.selectUp()
	}
}

func (m *Model) panelSelectDown() {
	switch m.focus {
	case focusJobs:
		m.jobs.selectDown()
	case focusDesigns:
		m.designs.selectDown()
	case focusLab:
		m.lab.selectDown()
	}
}
```

If `go build ./...` now reports `openDetail` declared and not used, that is expected — it is still referenced by `refreshJobLogs`; leave it. (Unused *methods* do not break the Go build; only unused imports/locals do.)

- [ ] **Step 5: Add focused-panel key routing to `handleKey`**

In `internal/tui/app.go`, in `handleKey`, immediately after the overlay `switch` block closes (the `}` after the `case overlayKeys:` block) and **before** the `if m.showSlashMenu {` block, insert:

```go
	// When a side panel holds focus, it owns the keyboard: arrows move the
	// row selection, Tab/Esc move focus, Enter opens the detail view (wired
	// in the detail-overlay task). The message input is inactive.
	if m.focus != focusChat {
		switch msg.Type {
		case tea.KeyUp:
			m.panelSelectUp()
			return m, nil
		case tea.KeyDown:
			m.panelSelectDown()
			return m, nil
		case tea.KeyEnter:
			return m, nil // detail view wired in Task 10
		case tea.KeyTab:
			m.cycleFocus()
			return m, nil
		case tea.KeyEsc:
			m.focus = focusChat
			m.syncPanelFocus()
			return m, nil
		case tea.KeyCtrlD:
			return m, tea.Quit
		case tea.KeyCtrlC:
			if m.running && m.turnCancel != nil {
				m.turnCancel()
				m.chat.appendError("cancelled")
			}
			return m, nil
		}
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '?' {
			m.overlay = overlayKeys
			return m, nil
		}
		return m, nil // swallow every other key — the input is inactive
	}
```

- [ ] **Step 6: Initialise focus state in `New`**

In `internal/tui/app.go`, in `New`, just before `return m` (the non-replay return at the end), add:

```go
	m.syncPanelFocus()
```

- [ ] **Step 7: Refresh the keybindings table**

In `internal/tui/keybindings.go`, in the `keybindings()` slice, replace these three rows:

```go
		{"Tab", "focus", "Cycle focus between panels"},
```
→
```go
		{"Tab", "focus", "Cycle focus: chat → jobs → designs → lab"},
```

```go
		{"↑/↓", "navigate", "Navigate within the focused panel"},
```
→
```go
		{"↑/↓", "navigate", "Move the selection in the focused panel"},
```

```go
		{"Enter", "send", "Send the message / activate"},
```
→
```go
		{"Enter", "send", "Send the message, or open the selected panel row"},
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TabCyclesPanelFocus|FocusedPanelArrows|FocusedPanelDimsInput' -v`
Expected: PASS.
Run: `go test ./internal/tui/`
Expected: PASS (no regressions).

- [ ] **Step 9: Commit**

```bash
git add internal/tui/app.go internal/tui/commandbar.go internal/tui/keybindings.go internal/tui/app_test.go
git commit -m "$(cat <<'EOF'
feat(tui): focusable side panels with a Tab ring and dimmed input

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Wire the detail overlay

Connects `Enter` on a focused panel to the full-screen detail view, handles its keys, and refreshes it live.

**Files:**
- Modify: `internal/tui/app.go` (`detailKind` field, `openDetail`, `refreshDetail`, `handleKey` `overlayDetail` case + focused-panel `Enter`, `refreshMsg` handler, `refreshJobLogs`)
- Test: `internal/tui/app_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/tui/app_test.go` add:

```go
func TestEnterOpensDetailOverlay(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.jobs.setJobs([]domain.Job{
		{ID: "j1", Tool: "design.bindcraft", Status: domain.JobRunning, Created: time.Now()},
	})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})   // focus jobs
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open detail
	if m.overlay != overlayDetail {
		t.Fatalf("Enter on a focused panel should open the detail overlay, got %v", m.overlay)
	}
	if !strings.Contains(m.View(), "design.bindcraft") {
		t.Error("the detail overlay should show the selected job")
	}
}

func TestDetailOverlayEscClosesKeepsFocus(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.jobs.setJobs([]domain.Job{{ID: "j1", Tool: "t", Status: domain.JobRunning, Created: time.Now()}})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != overlayNone {
		t.Error("Esc should close the detail overlay")
	}
	if m.focus != focusJobs {
		t.Error("Esc should keep the originating panel focus")
	}
}

func TestEnterOnEmptyPanelIsNoop(t *testing.T) {
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus jobs (empty)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayNone {
		t.Error("Enter on an empty panel must not open an overlay")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'EnterOpensDetail|DetailOverlayEsc|EnterOnEmptyPanel' -v`
Expected: FAIL — `Enter` on a focused panel is currently a no-op, so `overlay` stays `overlayNone`.

- [ ] **Step 3: Add the `detailKind` field**

In `internal/tui/app.go`, in the `Model` struct, next to the existing `detailID string` field (renamed from `jobLogID` in Task 4), add:

```go
	detailKind panelFocus // which panel the open detail view came from
```

- [ ] **Step 4: Replace `openDetail` with the panel-driven version**

In `internal/tui/app.go`, replace the whole `openDetail` method (the one renamed from `openJobLog` in Task 4) with:

```go
// openDetail builds the full-screen detail view for the focused panel's
// selected row and shows it. It is a no-op when the focused panel is empty or
// the chat is focused. Returns a tea.Cmd (always nil today) so it slots into
// the handleKey return contract.
func (m *Model) openDetail() tea.Cmd {
	var header, body string
	switch m.focus {
	case focusJobs:
		j, ok := m.jobs.selectedJob()
		if !ok {
			return nil
		}
		header, body = renderJobDetail(m.theme, j)
		m.detailID = string(j.ID)
	case focusDesigns:
		d, ok := m.designs.selectedDesign()
		if !ok {
			return nil
		}
		header, body = renderDesignDetail(m.theme, d)
		m.detailID = string(d.ID)
	case focusLab:
		e, ok := m.lab.selectedExperiment()
		if !ok {
			return nil
		}
		header, body = renderExperimentDetail(m.theme, e)
		m.detailID = string(e.ID)
	default:
		return nil
	}
	m.detailKind = m.focus
	m.detail.setSize(m.width, m.height)
	m.detail.setContent(header, body)
	m.overlay = overlayDetail
	return nil
}

// refreshDetail rebuilds the open detail overlay from current panel data so a
// running job's progress and log update live. It closes the overlay if the
// open item has disappeared.
func (m *Model) refreshDetail() {
	if m.overlay != overlayDetail {
		return
	}
	var header, body string
	found := false
	switch m.detailKind {
	case focusJobs:
		for _, j := range m.jobs.jobs {
			if string(j.ID) == m.detailID {
				header, body = renderJobDetail(m.theme, j)
				found = true
			}
		}
	case focusDesigns:
		for _, d := range m.designs.designs {
			if string(d.ID) == m.detailID {
				header, body = renderDesignDetail(m.theme, d)
				found = true
			}
		}
	case focusLab:
		for _, e := range m.lab.experiments {
			if string(e.ID) == m.detailID {
				header, body = renderExperimentDetail(m.theme, e)
				found = true
			}
		}
	}
	if !found {
		m.overlay = overlayNone
		return
	}
	m.detail.setContent(header, body)
}
```

- [ ] **Step 5: Wire `Enter` in the focused-panel block to `openDetail`**

In `internal/tui/app.go`, in `handleKey`, in the focused-panel block added by Task 9, replace:

```go
		case tea.KeyEnter:
			return m, nil // detail view wired in Task 10
```
with
```go
		case tea.KeyEnter:
			return m, m.openDetail()
```

- [ ] **Step 6: Replace the `overlayDetail` key handling**

In `internal/tui/app.go`, in `handleKey`, replace the whole `case overlayDetail:` block (renamed from `case overlayJobLog:` in Task 4) with:

```go
	case overlayDetail:
		switch msg.Type {
		case tea.KeyEsc:
			m.overlay = overlayNone // keep the originating panel focus
		case tea.KeyTab:
			m.overlay = overlayNone
			m.cycleFocus()
		case tea.KeyCtrlD:
			return m, tea.Quit
		case tea.KeyCtrlC:
			if m.running && m.turnCancel != nil {
				m.turnCancel()
				m.chat.appendError("cancelled")
			}
		default:
			m.detail = m.detail.update(msg)
		}
		return m, nil
```

- [ ] **Step 7: Replace `refreshJobLogs`'s overlay re-open with `refreshDetail`**

In `internal/tui/app.go`, in `refreshJobLogs`, delete the trailing overlay-reopen block:

```go
	if m.overlay == overlayDetail && m.detailID != "" {
		m.openDetail(m.detailID)
	}
```

(After Task 4's rename it reads as above; the original was `overlayJobLog` / `m.jobLogID` / `m.openJobLog`. Whatever its exact text, remove the `if m.overlay == ... { m.openDetail(...) }` tail — `refreshJobLogs` now only upserts the in-chat job-log blocks.)

Then, in `internal/tui/app.go`, in `Update`, in the `case refreshMsg:` handler, add `m.refreshDetail()` after `m.refreshJobLogs()`:

```go
	case refreshMsg:
		m.reloadPanels()
		m.refreshJobLogs()
		m.refreshDetail()
		return m, m.scheduleRefresh()
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'EnterOpensDetail|DetailOverlayEsc|EnterOnEmptyPanel' -v`
Expected: PASS.
Run: `go build ./... && go test ./...`
Expected: PASS — full build and suite green (`openDetail` now has no stale `(id string)` callers).

- [ ] **Step 9: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "$(cat <<'EOF'
feat(tui): full-screen detail view for jobs, designs, and experiments

Enter on a focused panel opens a scrollable detail overlay; it
refreshes live for running jobs and closes if the item disappears.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Final verification

- [ ] **Run the full suite and build**

Run: `go build ./... && go test ./...`
Expected: all packages PASS.

- [ ] **Manual smoke test** (`./bin/fova` after `make build`)

- Mouse wheel scrolls the chat; scrolling up and waiting >1 s no longer snaps back; the footer shows `↓ End for latest`.
- `Tab` cycles chat → jobs → designs → lab; the focused panel's header turns saffron and the input dims.
- `↑/↓` moves the selection; `Enter` opens a full-screen detail view; `Esc` closes it; `Tab` inside it moves to the next panel.
- `/clear` leaves the side panels exactly where they were.

---

## Spec Coverage Check

- Spec §4.1 enable mouse → Task 1. §4.2 route wheel → Task 1. §4.3 stop snap-to-bottom → Task 2. §4.4 footer hint → Task 2.
- Spec §5.1 focus ring → Task 9. §5.2 selection state → Tasks 6/7/8. §5.3 rendering → Tasks 6/7/8 (+ `panelHeader`). §5.4 key routing + dimmed input → Task 9.
- Spec §6.1 generalise the view → Task 4. §6.2 opening → Task 10. §6.3 body renderers → Task 5. §6.4 overlay keys → Task 10. §6.5 live refresh → Task 10.
- Spec §7 `/clear` fix → Task 3.
- Spec §10 testing → tests are embedded in every task; the manual smoke test covers §9 edge cases.
