# Modern TUI Design (v0.4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the Proteus TUI's visual language up to the standard of modern agent CLIs (Claude Code, Codex CLI, Gemini CLI, OpenCode), as specified in `docs/SPECS.md` §10.7, without changing agent-loop, tool, or persistence behaviour.

**Architecture:** Each visual component is a self-contained Bubble Tea sub-model in its own file under `internal/tui/`. A shared foundation (design-token palette + slash-command catalogue) lands first; six independent components are then built in parallel; finally `app.go` wires them together and removes dead code.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, Glamour. Tests are `go test`, offline and deterministic — consistent with the v0.1–v0.3 pattern.

---

## Execution model

Three phases:

- **Phase A — Foundation (sequential).** `theme.go` tokens + `commands.go` catalogue. Everything else depends on it. Done first, committed.
- **Phase B — Components (6 parallel agents).** Tasks B1–B6. Each owns a disjoint set of files; no two tasks touch the same file. `theme.go`, `commands.go`, and `app.go` are off-limits in Phase B.
- **Phase C — Integration (sequential).** `app.go` + `app_test.go`: wire the new components into `View`/`Update`/`layout`, remove superseded code.

### Hard rules for Phase B agents

The whole `internal/tui` package compiles as one unit and the test binary is one
unit. Six agents edit it concurrently. Therefore:

1. **Purely additive.** Keep every existing exported symbol and every symbol
   referenced by `app.go` (`commandBarModel.ta`, `commandBarModel.hints`,
   `chatModel.appendToolStart`, `statusBarModel.View`, `jobsModel.View`, etc.).
   Add new code; do not delete or rename. Phase C removes superseded code.
2. **Never leave the package non-compiling.** When doing TDD, write a *compiling
   stub* (correct signature, wrong/empty body) before the failing test, so the
   package always builds. A failing assertion is fine; a compile error is not —
   it blocks the other five agents.
3. **Scope your verification.** Build with `go build ./internal/tui/` and test
   with `go test ./internal/tui/ -run <YourPrefix>`. If a compile error names a
   file you do **not** own, another agent is mid-edit — wait briefly and re-run.
4. **Only touch your files.** Listed per task below.
5. **Follow the existing style:** package-level doc comments, `lipgloss` styles
   from the `Theme`, table-driven tests. Match `chat.go` / `jobs.go`.

---

## File Structure

| File | Phase | Owner | Responsibility |
|---|---|---|---|
| `internal/tui/theme.go` | A | foundation | Token `Palette`, `DefaultPalette`, glyph set, derived `Theme` styles |
| `internal/tui/commands.go` (new) | A | foundation | Slash-command catalogue (name + description) |
| `internal/tui/commandbar.go` | B1 | agent 1 | Bordered message input |
| `internal/tui/slashmenu.go` (new) | B2 | agent 2 | Slash-command autocomplete popup |
| `internal/tui/spinner.go` (new) | B3 | agent 3 | Animated thinking indicator |
| `internal/tui/chat.go` | B4 | agent 4 | Tree-connected tool traces + welcome entry |
| `internal/tui/statusbar.go` | B5 | agent 5 | Slim header + footer + context meter |
| `internal/tui/jobs.go`, `designs.go` | B6 | agent 6 | Panel polish: rules, empty states, progress bars |
| `internal/tui/app.go` | C | integrator | Wiring + dead-code removal |

---

## Phase A — Foundation

### Task A1: Design-token palette and glyph set

**Files:**
- Modify: `internal/tui/theme.go`
- Modify: `internal/tui/theme_test.go`

- [ ] **Step 1: Write failing tests** in `theme_test.go`: `DefaultPalette` has
  non-empty `Fg`, `FgMuted`, `FgSubtle`, `Accent`, `Border`, and the five status
  colours; `NewTheme()` still returns a `Theme` with the existing fields
  populated; `glyph(domain.JobSucceeded)` returns `"✓"`.
- [ ] **Step 2: Run** `go test ./internal/tui/ -run TestPalette -v` — expect FAIL.
- [ ] **Step 3: Implement** in `theme.go`, per SPECS §10.5:
  - Add the `Palette` struct and `DefaultPalette` var exactly as in SPECS §10.5.
  - Keep the existing `Theme` struct and `NewTheme()`; additionally derive new
    styles from the palette as fields on `Theme` (e.g. `SectionRule`, `Footer`,
    `InputBorder`, `InputBorderActive`, `Hint`). Keep all current fields.
  - Add a `glyph(domain.JobStatus) string` helper and the glyph table from
    SPECS §10.7.8 (queued `·`, running `⟳`, succeeded `✓`, failed `✗`,
    cancelled `⊘`). This is the single source of truth — `jobs.go` will use it.
- [ ] **Step 4: Run** `go test ./internal/tui/ -run 'TestPalette|TestTheme|TestGlyph' -v` — expect PASS.
- [ ] **Step 5: Run** `go build ./... && go vet ./internal/tui/` — expect clean.
- [ ] **Step 6: Commit** `feat(tui): add v0.4 design-token palette and glyph set`.

### Task A2: Slash-command catalogue

**Files:**
- Create: `internal/tui/commands.go`
- Create: `internal/tui/commands_test.go`

- [ ] **Step 1: Write failing test** in `commands_test.go`: `slashCommands` is
  non-empty; every entry has a non-empty `Name` and `Description`; it includes
  `model`, `provider`, `clear`, `help`, `quit`; `matchCommands("mo")` returns
  the `model` entry; `matchCommands("")` returns all.
- [ ] **Step 2: Run** `go test ./internal/tui/ -run TestSlashCommands -v` — expect FAIL.
- [ ] **Step 3: Implement** `commands.go`:
  ```go
  // slashCmd is one entry in the slash-command catalogue.
  type slashCmd struct{ Name, Description string }

  // slashCommands is the single source of truth for slash-command metadata,
  // consumed by the autocomplete popup, the footer hint, and /help.
  var slashCommands = []slashCmd{
      {"model", "switch the active model"},
      {"provider", "switch the LLM provider"},
      {"clear", "compact the conversation context"},
      {"help", "show keybindings and commands"},
      {"quit", "save and exit"},
  }

  // matchCommands returns the catalogue entries whose name has prefix as a
  // case-insensitive prefix. An empty prefix returns the whole catalogue.
  func matchCommands(prefix string) []slashCmd { ... }
  ```
- [ ] **Step 4: Run** `go test ./internal/tui/ -run TestSlashCommands -v` — expect PASS.
- [ ] **Step 5: Commit** `feat(tui): add slash-command catalogue`.

---

## Phase B — Components (parallel)

All six tasks start from the Phase A commit. Obey the **Hard rules** above.

### Task B1: Bordered message input

**Files (only these):** `internal/tui/commandbar.go`, `internal/tui/commandbar_test.go`

Per SPECS §10.7.2. Additive — keep `slashCommandHints`, `parseSlashCommand`,
`newCommandBarModel`, `value`, `reset`, `setWidth`, and the `ta` field.

- [ ] Add a `focused` and `running` bool to `commandBarModel` with setters.
- [ ] Add `func (m commandBarModel) View() string`: render `m.ta.View()` inside
  a `lipgloss.RoundedBorder()` titled `message`. Border colour: `Accent` when
  `focused && !running`, `FgSubtle` when `running`, else `Border` (use the
  `Theme` styles from Task A1). Set the textarea `Prompt` to `"› "` and
  `Placeholder` to `"Type a message, or / for commands"` in `newCommandBarModel`.
- [ ] Tests: `View()` output contains the rounded-border runes and the label
  `message`; border style differs between focused-idle and running states
  (compare rendered output is non-equal).
- [ ] TDD: compiling stub → failing test → implement → green. Verify with
  `go build ./internal/tui/` and `go test ./internal/tui/ -run TestCommandBar`.
- [ ] Commit `feat(tui): bordered message input`.

### Task B2: Slash-command autocomplete popup

**Files (only these):** `internal/tui/slashmenu.go` (new), `internal/tui/slashmenu_test.go` (new)

Per SPECS §10.7.3. Reads `slashCommands` / `matchCommands` from `commands.go`
(Task A2) — read-only.

- [ ] Implement `slashMenuModel`: fields for the filtered entries and a cursor.
  - `newSlashMenu() *slashMenuModel`
  - `setFilter(prefix string)` — refilter via `matchCommands`, clamp the cursor.
  - `next()`, `prev()` — move the cursor (no wrap, clamp at ends).
  - `selected() (slashCmd, bool)` — current entry; false when the list is empty.
  - `visible() bool` — true when there is at least one entry to show.
  - `view(th Theme, width int) string` — a list, one row per command:
    `/name  — description`, the cursor row styled with `Theme.PickerSel`,
    descriptions in `FgMuted`.
- [ ] Tests: `setFilter("mo")` leaves one entry (`model`); `setFilter("")` shows
  all; `next()`/`prev()` clamp; `view` contains `/model` and its description.
- [ ] TDD with compiling stubs; verify `go test ./internal/tui/ -run TestSlashMenu`.
- [ ] Commit `feat(tui): slash-command autocomplete popup`.

### Task B3: Thinking indicator

**Files (only these):** `internal/tui/spinner.go` (new), `internal/tui/spinner_test.go` (new)

Per SPECS §10.7.4.

- [ ] Implement `thinkingModel`:
  - Braille frames `[]rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")`.
  - `start(verb string, t time.Time)`, `stop()`, `tick()` (advance the frame).
  - `active() bool`.
  - `view() string` → `"<frame> <verb>… (<elapsed>s · esc to interrupt)"`,
    empty string when not active. Elapsed is whole seconds from the start time
    (inject the start time so the test is deterministic).
  - A `verbForTool(tool string) string` helper mapping tool-name substrings to
    `Designing` / `Folding` / `Scoring` / `Searching`, default `Thinking`.
- [ ] Provide a `spinnerTickMsg` type + `spinnerTick() tea.Cmd` (~80 ms
  `tea.Tick`) so `app.go` can drive the animation in Phase C.
- [ ] Tests: `view()` empty before `start`; after `start("Designing", fixedTime)`
  contains `Designing…` and `esc to interrupt`; `tick()` advances the frame;
  `verbForTool("rfdiffusion.generate")` → `"Designing"`; elapsed computed from
  an injected "now".
- [ ] TDD with compiling stubs; verify `go test ./internal/tui/ -run TestThinking`.
- [ ] Commit `feat(tui): animated thinking indicator`.

### Task B4: Tree-connected tool traces + welcome entry

**Files (only these):** `internal/tui/chat.go`, `internal/tui/chat_test.go`

Per SPECS §10.7.5 and §10.7.7. Additive — keep `newChatModel`, `resize`, every
`append*` method signature, `View`, and `renderEntries`.

- [ ] Add `entryWelcome` to the `entryKind` enum and a `chatModel.appendWelcome(text string)`.
- [ ] In `renderEntries`, render `entryWelcome` in `FgMuted` (no markdown pass).
- [ ] Rework tool rendering (SPECS §10.7.5): a tool entry renders as a header
  `⏺ <name>(<args>)` in `Fg`, then dim `FgMuted` result lines indented under a
  `⎿` connector; truncate result output to 6 lines with a `… +N lines` footer
  in `FgSubtle`. Running header uses `⏺` in the `Running` token; done uses `⏺`
  (`Succeeded`) or `✗` (`Failed`); append ` (<duration>)` when known.
  - Extend `chatEntry` with the fields you need (e.g. `args string`,
    `dur time.Duration`); keep the existing fields. Adjust `appendToolStart` /
    `appendToolDone` bodies but **keep their signatures** (`app.go` calls them).
- [ ] Tests: `appendWelcome` then `renderEntries` contains the welcome text;
  a done tool entry renders the `⏺`/`⎿` connectors; output over 6 lines is
  truncated with `… +`; an errored tool renders `✗`.
- [ ] TDD with compiling stubs; verify `go test ./internal/tui/ -run TestChat`.
- [ ] Commit `feat(tui): tree-connected tool traces and welcome entry`.

### Task B5: Status footer and context meter

**Files (only these):** `internal/tui/statusbar.go`, `internal/tui/statusbar_test.go`

Per SPECS §10.7.6. Additive — keep `newStatusBarModel`, the existing `View()`
(it becomes the header), and the existing fields.

- [ ] Add `project string`, `ctxPercent int` fields with setters.
- [ ] Add `headerView() string` → ` proteus · <project> ` in `Accent` (the
  existing `View()` may delegate to this).
- [ ] Add `footerView() string` → `FgMuted` line:
  `<hint>   <model> · $<cost> · <NN>% context`, where `<hint>` is built from the
  first few `slashCommands` names (read `commands.go`, read-only). The
  `<NN>% context` segment turns `Warning` when `ctxPercent > 80`.
- [ ] Tests: `footerView()` contains the model, the dollar cost, `% context`;
  at `ctxPercent = 90` the warning style is applied; `headerView()` contains the
  project name.
- [ ] TDD with compiling stubs; verify `go test ./internal/tui/ -run TestStatus`.
- [ ] Commit `feat(tui): status footer with context meter`.

### Task B6: Panel polish

**Files (only these):** `internal/tui/jobs.go`, `internal/tui/jobs_test.go`, `internal/tui/designs.go`, `internal/tui/designs_test.go`

Per SPECS §10.7.8. Additive — keep `newJobsModel`, `newDesignsModel`, `setJobs`,
`setDesigns`, `setWidth`, and the `View()` signatures.

- [ ] Add a `sectionRule(label string, width int, th Theme) string` helper
  (lowercase label + `─` run in `FgSubtle`) and use it for both panel headers.
- [ ] Replace the empty-state strings with the actionable ones from SPECS §10.7.8.
- [ ] Add a `progressBar(elapsed, eta time.Duration, width int) string` helper
  (`▓`/`░`); render it under a running job line when an ETA is known.
- [ ] Use the shared `glyph()` helper from Task A1 instead of the local
  `jobGlyph` (keep `jobGlyph` as a thin wrapper if it is referenced elsewhere).
- [ ] Tests: a running job with an ETA renders `▓` and `░`; the empty states
  render the new actionable text; the section rule renders the label and `─`.
- [ ] TDD with compiling stubs; verify `go test ./internal/tui/ -run 'TestJobs|TestDesigns'`.
- [ ] Commit `feat(tui): panel polish — rules, empty states, progress bars`.

---

## Phase C — Integration

### Task C1: Wire components into the root model

**Files:** `internal/tui/app.go`, `internal/tui/app_test.go`

- [ ] Add `thinking thinkingModel` and `slashMenu *slashMenuModel` to `Model`;
  construct them in `New`.
- [ ] `View`: render the slim header (`status.headerView()`); the chat / panels
  body (panels use `sectionRule`); the thinking indicator line when
  `m.running`; the bordered input (`m.cmdbar.View()`); the slash menu popup
  above the input when it is visible; and `status.footerView()` at the bottom.
  Remove the `m.theme.CommandBar.Render(m.cmdbar.hints)` line.
- [ ] `Update`: on every keystroke into the command bar, if the line starts with
  `/`, call `m.slashMenu.setFilter(...)` and show it; handle `↑/↓/Tab/Esc` for
  the menu; drive `spinnerTick` while `m.running`; set `cmdbar` focused/running
  state; clear `thinking` on `TurnDoneMsg` / `TurnErrorMsg`; start it in
  `startTurn`; update the verb on `ToolStartMsg` via `verbForTool`.
- [ ] On `Init` / `New`, call `chat.appendWelcome(...)` with the §10.7.7 text.
- [ ] Recompute `chatHeight()` for the new fixed rows (header 1, thinking 1,
  bordered input 3, footer 1).
- [ ] Remove now-dead code: `slashCommandHints` and its use; fold the old
  combined status bar into header+footer.
- [ ] Update `app_test.go` for the new `View` composition; keep the
  `runSlashCommand` routing tests.
- [ ] **Verify:** `gofmt -l internal/tui` empty, `go vet ./...` clean,
  `go build ./...` clean, `go test ./...` all pass.
- [ ] Commit `feat(tui): integrate v0.4 modern design`.

---

## Self-Review checklist (run before Phase C commit)

- [ ] Every SPECS §10.7 subsection (10.7.1–10.7.8) maps to a task above.
- [ ] No Phase B task touches `theme.go`, `commands.go`, or `app.go`.
- [ ] No two Phase B tasks share a file.
- [ ] `tui.New` / `tui.Deps` signature unchanged — `cmd/proteus` is untouched.
- [ ] `go test ./...` and `go vet ./...` clean.
