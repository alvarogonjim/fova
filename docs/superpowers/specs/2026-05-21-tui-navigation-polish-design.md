# fova — TUI Navigation Polish

**Spec date:** 2026-05-21
**Status:** Implementation-ready
**Author:** Alvaro (brainstormed with Claude Code)
**Scope:** `internal/tui`, `cmd/fova`

## 1. Summary

Three usability gaps in the fova TUI:

1. **The chat history cannot be browsed.** The mouse wheel does nothing, and
   even keyboard scrolling is undone within a second because the chat snaps
   back to the bottom on every refresh.
2. **The side panels are display-only.** On wide terminals the jobs / designs /
   wet-lab panels cannot be focused, a row cannot be selected, and there is no
   way to open an item's full detail / status.
3. **`/clear` makes the side panels appear to vanish.**

This spec fixes all three. It is the first of two specs splitting four
requested features; the **first-run onboarding wizard** is the second spec and
is out of scope here.

## 2. Current behaviour and root causes

### 2.1 Chat scrolling
- `cmd/fova/main.go:166` starts the program with
  `tea.NewProgram(app, tea.WithAltScreen())` — no mouse option, so wheel events
  never reach the model.
- `chatModel.refresh()` (`internal/tui/chat.go:323`) calls
  `c.viewport.GotoBottom()` **unconditionally**. `refreshJobLogs()` runs every
  second off `refreshMsg` and calls `upsertJobLog → refresh()`, so any `PgUp`
  the user does is reverted within ≤1 s.
- Keys `PgUp` / `PgDn` / `Home` / `End` are wired (`app.go:540-551`) and do
  work — momentarily.

### 2.2 Side panels
- On terminals ≥100 cols, `View()` (`app.go:1082-1097`) always renders
  jobs + designs + lab on the right and **ignores `m.focus`** — focus is
  invisible and inert.
- `panelFocus` / `cycleFocus()` only change which *single* pane is shown on
  terminals <100 cols.
- The full-screen view (`jobLogView`, `joblog.go`) exists, but `cycleFocus()`
  only ever opens it for **running** jobs (`runningJobIDs()`), and there is no
  detail view at all for designs or experiments.

### 2.3 /clear
- `runSlashCommand` "clear" (`app.go:719-723`) rebuilds the chat with
  `newChatModel(m.theme, m.chatWidth(), m.chatHeight())`. `chatWidth()`
  (`app.go:1141`) returns the **full** terminal width — it does not subtract
  the 38-col panel column — and `/clear` never calls `m.layout()`. The chat
  viewport becomes full width, `JoinHorizontal(chat, "  ", right)` overflows
  the terminal, and the panels are drawn past the right edge → they look empty
  / gone.

## 3. Goals / non-goals

**Goals**
- The mouse wheel scrolls the chat; a scrolled-up position is preserved.
- Every side panel can be focused, navigated with ↑/↓, and any row opened into
  a full-screen detail view — on every terminal width.
- Detail views for jobs (any status), designs, and experiments.
- `/clear` leaves the panels exactly where they were.

**Non-goals** (explicitly confirmed during brainstorming)
- Mouse click / drag selection of panel rows — keyboard only.
- Mouse wheel over the panels — the wheel scrolls the chat only.
- The first-run onboarding wizard — separate spec.
- Any change to the store schema, the agent loop, or job execution.

## 4. Feature 1 — Chat scrolling

### 4.1 Enable the mouse
`cmd/fova/main.go` and `cmd/fova/replay.go`: add `tea.WithMouseCellMotion()`
to the `tea.NewProgram` options.

Tradeoff accepted: mouse capture disables the terminal's native click-drag text
selection; users copy with the terminal's modifier-drag (commonly Shift+drag).

### 4.2 Route the wheel to the chat
`Model.Update` gains a `tea.MouseMsg` case. The wheel scrolls the chat **only**
(panels stay keyboard-driven), and the event is not a bus message, so the
handler returns no command:

```go
case tea.MouseMsg:
    m.chat.handleMouse(msg)
    return m, nil
```

`chatModel.handleMouse` forwards the event to the inner `viewport`, whose
built-in `MouseWheelEnabled` handling scrolls it. Non-wheel mouse events
(clicks, motion) are ignored by the viewport — no extra filtering needed:

```go
func (c *chatModel) handleMouse(msg tea.MouseMsg) {
    c.viewport, _ = c.viewport.Update(msg)
}
```

### 4.3 Stop the snap-to-bottom
`chatModel.refresh()` only auto-scrolls when the reader is already at the
bottom:

```go
func (c *chatModel) refresh() {
    follow := c.viewport.AtBottom()
    c.viewport.SetContent(c.renderEntries())
    if follow {
        c.viewport.GotoBottom()
    }
}
```

New content follows the reader only when they are already at the bottom; a
scrolled-up reader stays put across the 1-second refresh tick and across
streaming agent deltas.

`appendUser` is the one exception — sending a message always jumps to the
latest, so the user sees their own message land:

```go
func (c *chatModel) appendUser(text string) {
    c.entries = append(c.entries, chatEntry{kind: entryUser, text: text})
    c.refresh()
    c.viewport.GotoBottom()
}
```

A small `atBottom() bool` accessor is added for the footer hint (§4.4).

### 4.4 "Scrolled up" footer hint
While the chat is not at the bottom, the footer shows a hint so a reader
browsing history during a streaming turn knows there is new content below.

- `statusBarModel` gains `chatScrolledUp bool`.
- `footerView()` appends `  ·  ↓ End for latest` when it is true.
- `Model.View()` sets `m.status.chatScrolledUp = !m.chat.atBottom()` before
  composing the footer.

This is the only new affordance beyond the literal bug fixes; it is cheap and
prevents a "did the agent stop replying?" confusion.

## 5. Feature 2 — Panel focus & navigation

### 5.1 Focus ring
`cycleFocus()` is simplified to a **4-stop ring on all widths**:
chat → jobs → designs → lab → chat. The running-job-in-the-ring behaviour is
removed — it is replaced by focusing the jobs panel and selecting a row.

### 5.2 Selection state
`jobsModel`, `designsModel`, and `labModel` each gain:

- `focused bool` — set by the `Model` from `m.focus` before each render.
- `selected int` — the highlighted row index, clamped to `[0, len-1]`, `0`
  when the panel is empty.
- `selectUp()` / `selectDown()` — move and clamp `selected`.
- `selectedJob() (domain.Job, bool)` (and `selectedDesign`,
  `selectedExperiment`) — return the selected item, `false` when empty.

`setJobs` / `setDesigns` / `setExperiments` re-clamp `selected` so a shrinking
list never strands the cursor out of range.

### 5.3 Rendering
Each panel's `View()`:

- The header (`sectionRule` / `RenderSectionRule`) renders in the theme accent
  colour when `focused`, dim otherwise — this is the focus indicator.
- The `selected` row renders highlighted (accent foreground + a `▸ ` marker)
  **only when `focused`**. An unfocused panel renders flat, exactly as today.

### 5.4 Key routing
In `handleKey`, when `m.overlay == overlayNone` and `m.focus != focusChat`,
the focused panel owns the keyboard:

| Key | Action |
|---|---|
| ↑ / ↓ | move the panel selection |
| Enter | open the detail view for the selected row |
| Tab | advance focus to the next panel |
| Esc | return focus to chat |
| ? | open the `/keys` overlay |
| Ctrl+C / Ctrl+D | cancel turn / quit (unchanged) |
| anything else | ignored — not typed into the input |

When `m.focus == focusChat` everything is unchanged: the input is live,
`PgUp` / `PgDn` / `Home` / `End` and the wheel scroll the chat, `Tab` moves
focus to the jobs panel.

The message input renders **dimmed** while a panel is focused, signalling it is
inactive. `commandBarModel` gains `setActive(bool)`, toggling a dim style on
its rendered view; the `Model` calls it on every focus transition. Input text
is preserved while a panel is focused and restored (live) when focus returns
to chat.

Empty panels: ↑/↓/Enter are no-ops; the header still shows the focus highlight.

## 6. Feature 3 — The detail view

### 6.1 Generalise the view
`joblog.go` is renamed `detail.go`; `jobLogView` becomes `detailView` (it is
already generic — a styled header line above a `viewport` body). `joblog_test.go`
becomes `detail_test.go`. The `overlayJobLog` constant becomes `overlayDetail`;
`newJobLogView` becomes `newDetailView`; `m.jobLog` becomes `m.detail`. The
`m.jobLogID` field and `openJobLog` are replaced by §6.2.

The `Model` records what is open so the overlay can refresh live:

```go
detail     detailView
detailKind panelFocus // focusJobs|focusDesigns|focusLab — origin panel
detailID   string     // id of the open item, for live refresh
```

### 6.2 Opening
`Enter` on a focused panel calls `openDetail()`:

1. Read the selected item from the focused panel (`selectedJob()` etc.); if the
   panel is empty, do nothing.
2. Build `(header, body)` with the matching renderer (§6.3).
3. `m.detail.setSize(m.width, m.height)` then `m.detail.setContent(header, body)`.
4. Record `detailKind` / `detailID`, set `m.overlay = overlayDetail`.

### 6.3 Detail-body renderers (new, in `detail.go`)
Each renderer returns a plain-string `(header, body)` — no markdown,
newline-faithful, consistent with the existing `joblog` body.

**`renderJobDetail(domain.Job) (header, body string)`**
- header: `<glyph> <tool> · <id> · <status>`
- metadata: status, kind, backend, cost, created / started / finished; a
  progress bar + `%` + ETA (`progressBar`, `jobETA`) for a running job;
  produced-design count and ids
- a red `error` block when `Status == failed` and `Error != ""`
- a `log` section: full contents of `LogFile` via `readLog` (`(no output yet)`
  when empty)

**`renderDesignDetail(domain.Design) (header, body string)`**
- header: `<id> · <origin> · <application>`, plus `★ shortlisted` when
  `isShortlisted(d)`
- metadata: created, structure file (`StructureFile`), tags
- `scores` section: every key in `Scores` (pLDDT, ipSAE, ipTM, …)
- `sequence` section: the amino-acid sequence, length-labelled, wrapped in
  10-character groups
- `provenance` section: one line per `ToolCallRef` — tool, version, time,
  short input hash
- a `lab` line: a one-line result summary, or `not submitted` when
  `LabResults` is empty
- `notes` when `Notes != ""`

**`renderExperimentDetail(domain.Experiment) (header, body string)`**
- header: `<target name> · <assay type> · <status>`
- metadata: backend, external id, submitted at, cost, design count
- a `results` table: one row per `ExperimentResult` — design id, Kd (+units),
  binding strength, R²; `—` for absent (`nil`-pointer) values; a
  `no results yet` note before any results arrive

### 6.4 In the overlay
`overlayDetail` key handling, in `handleKey`:

- ↑ / ↓ / PgUp / PgDn / Home / End → scroll the detail viewport
- Esc → close the overlay, keep the originating panel focus (so the user can
  ↑/↓ to the next row and open it)
- Tab → close the overlay and advance focus to the next panel
- Ctrl+C / Ctrl+D — unchanged

### 6.5 Live refresh
On the 1-second `refreshMsg`, after `reloadPanels()`, if `overlayDetail` is
open the `Model` re-resolves `detailID` against the freshly loaded panel data
and rebuilds the detail body — so a running job's progress and log update live.
This generalises today's `refreshJobLogs` re-open of `overlayJobLog`. If the
item is no longer present, the overlay closes.

The existing in-chat job-log blocks (`entryJobLog`, the *job-logs-in-chat*
feature) are unaffected and remain.

## 7. Feature 4 — /clear panel fix

`runSlashCommand` "clear":

```go
case "clear":
    m.chat = newChatModel(m.theme, m.chatWidth(), m.chatHeight())
    m.session = agent.NewSession(m.systemPrompt)
    m.beginPersistedSession()
    m.focus = focusChat // reset focus to the chat
    m.layout()          // re-size the chat for the panel column
    return m, nil
```

`m.layout()` recomputes `chatW = m.width - panelW - 2` and resizes the chat
viewport, so `JoinHorizontal` no longer overflows and the panels stay visible.
Panel selection indices are untouched — the jobs / designs survive `/clear`;
only the conversation is cleared.

## 8. Interaction model

`m.focus` ∈ {`focusChat`, `focusJobs`, `focusDesigns`, `focusLab`};
`m.overlay` ∈ {`overlayNone`, `overlayConfirm`, `overlaySubmit`,
`overlayPicker`, `overlayDetail`, `overlayKeys`}.

- **`overlayNone`, `focus == focusChat`** — input live; wheel + PgUp/PgDn/
  Home/End scroll chat; `Tab` → `focusJobs`.
- **`overlayNone`, `focus == panel`** — input dimmed; ↑/↓ select; `Enter` opens
  `overlayDetail`; `Tab` → next panel; `Esc` → `focusChat`.
- **`overlayDetail`** — scroll keys move the detail body; `Esc` closes (keeps
  panel focus); `Tab` closes and advances panel focus.

Existing overlays (`overlayConfirm`, `overlaySubmit`, `overlayPicker`,
`overlayKeys`) are unchanged and continue to take priority in `handleKey`.

## 9. Edge cases & error handling

- **Terminal <100 cols** — `View()` still shows one pane at a time; `Tab`
  cycles which pane is visible; the detail overlay is full-screen regardless.
  Structurally unchanged.
- **Empty panel focused** — navigation and `Enter` are no-ops; the header
  still shows the focus highlight.
- **Open item disappears from the store** — the overlay closes on the next
  refresh (§6.5).
- **Mouse-disabled terminals / SSH** — wheel events simply never arrive;
  keyboard scrolling still works.
- **Missing / empty job log** — `readLog` already returns `""`; the detail
  body shows `(no output yet)`.

## 10. Testing

`internal/tui/chat_test.go`
- `refresh()` preserves the scroll offset when the viewport is not at the
  bottom, and `GotoBottom`s when it is.
- `appendUser` always jumps to the bottom, even when previously scrolled up.
- `handleMouse` with a wheel-up event scrolls the chat up.

`internal/tui/jobs_test.go` / `designs_test.go` / `lab_test.go`
- `selectUp` / `selectDown` move and clamp `selected` at both ends.
- `selectedJob` / `selectedDesign` / `selectedExperiment` return the right
  item, and `false` for an empty panel.
- the selected row renders highlighted only when `focused`.
- `setJobs` (etc.) re-clamps `selected` when the list shrinks.

`internal/tui/detail_test.go`
- `renderJobDetail` produces the metadata block + log for running, succeeded,
  and failed (error block) jobs.
- `renderDesignDetail` renders scores, the wrapped sequence, and provenance;
  handles a design with no lab results.
- `renderExperimentDetail` renders the results table and the `no results yet`
  case.

`internal/tui/app_test.go`
- `Tab` cycles `focus` chat → jobs → designs → lab → chat on a wide terminal.
- with a panel focused, ↑/↓ move that panel's selection and `Enter` sets
  `overlayDetail`; `Esc` closes it and keeps panel focus.
- a `tea.MouseMsg` wheel event scrolls the chat.
- after `/clear`, the chat width equals `width - panelW - 2` (the panels are
  not pushed off-screen) and `focus == focusChat`.

## 11. Files

**Modified**
- `cmd/fova/main.go` — add `tea.WithMouseCellMotion()`.
- `cmd/fova/replay.go` — add `tea.WithMouseCellMotion()`.
- `internal/tui/app.go` — `tea.MouseMsg` case; `overlayDetail` rename;
  `cycleFocus` simplification; focused-panel key routing; `openDetail`;
  detail live-refresh on `refreshMsg`; focus-aware `View()`; `/clear` fix;
  `cmdbar.setActive` calls.
- `internal/tui/chat.go` — `refresh()` at-bottom check; `atBottom()`;
  `appendUser` jump; `handleMouse`.
- `internal/tui/jobs.go` — `focused` / `selected` state, selection methods,
  highlight rendering.
- `internal/tui/designs.go` — same.
- `internal/tui/lab.go` — same.
- `internal/tui/statusbar.go` — `chatScrolledUp` field + footer hint.
- `internal/tui/commandbar.go` — `setActive(bool)` + dim style.
- `internal/tui/keybindings.go` — refresh the bindings table (Tab ring, ↑/↓
  selection, Enter opens detail).

**Renamed**
- `internal/tui/joblog.go` → `internal/tui/detail.go` — `jobLogView` →
  `detailView`, `newJobLogView` → `newDetailView`; add the three
  `render*Detail` body builders.
- `internal/tui/joblog_test.go` → `internal/tui/detail_test.go`.

## 12. Out of scope

- The first-run onboarding wizard (separate spec).
- Mouse click / drag row selection; mouse wheel over panels.
- Resizable or reorderable panels.
- Persisting the focused panel / scroll position across restarts.
