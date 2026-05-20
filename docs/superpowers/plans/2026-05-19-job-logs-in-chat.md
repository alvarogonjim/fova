# Live Job Logs in the Chat — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface live job output in the TUI — a compact auto-updating log block per job in the chat, and a Tab-focusable full-screen log view — per `docs/superpowers/specs/2026-05-19-job-logs-in-chat-design.md`.

**Architecture:** A foundation change makes the `jobs.Manager` own one log file per job and threads an `io.Writer` through `jobs.Spec.Run` and `backends.Backend.Run`. The TUI then tails those files on its existing 1 s refresh tick to update an in-chat block, and shows a full-screen view on Tab.

**Tech Stack:** Go, Bubble Tea, Lip Gloss. Tests are `go test`, offline and deterministic.

---

## Execution model

- **Phase A — Foundation (Task A, one agent, sequential).** The cross-cutting `jobs` / `backends` / `domain` signature changes. Must land first and compile cleanly before Phase B — it touches the shared `tui` package via `setup.go`.
- **Phase B — TUI components (Tasks B and C, two parallel agents).** `chat.go` and a new `joblog.go`. Both are self-contained, take plain inputs, and do **not** depend on Task A's new APIs — they only need the package to compile (so they run after A).
- **Phase C — Integration (Task D, orchestrator).** Wires `app.go`.

### Hard rules for Phase B agents

1. **Only touch your task's files.** `internal/tui/app.go`, `cmd/proteus/`, and Phase A's files are off-limits.
2. **Keep the `tui` package compiling** — a compiling stub before a failing test; a compile error blocks the sibling agent.
3. **Offline, deterministic tests** — temp files, fixed inputs; no network.
4. **Follow existing patterns** — match `chat.go`, `jobs.go`, `modal.go`.
5. **Do NOT run git.** `gofmt -w` your files. The orchestrator commits.

---

## Task A: Foundation — per-job log files (one agent, do first)

**Files:** `internal/domain/types.go`, `internal/jobs/manager.go` + `manager_test.go`, `internal/backends/backend.go` (+ the local/modal backend code it calls), `internal/tools/design/design.go` + `design_test.go`, `internal/tools/fold/foldjob.go` + `foldjob_test.go`, `internal/tui/setup.go`, `cmd/proteus/main.go`.

**Read first:** all of the above; note every `Spec{Run: func(...)}` literal and every `Backend.Run` implementer/caller/stub.

- [ ] `domain/types.go`: add `LogFile string \`json:"log_file,omitempty"\`` to `Job`.
- [ ] `jobs/manager.go`:
  - Change `Spec.Run` to `func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error)`.
  - Add an unexported `logDir string` field and `func (m *Manager) SetLogDir(dir string)`. **Do not change `NewManager`'s signature** — that keeps every existing `NewManager` test call working.
  - In `run`: if `logDir != ""`, create `<logDir>/<jobID>.log` (mkdir the dir), set `job.LogFile` on the job before the first `mutate`, open the file, pass it as the `log` arg to `spec.Run`, and close it when the job ends. If `logDir == ""` pass `io.Discard`.
  - `manager_test.go`: update every `Spec{Run: ...}` closure to the 3-arg signature; add a test that with `SetLogDir(t.TempDir())` a job's `LogFile` is set and the file exists with the bytes the `Run` closure wrote to `log`.
- [ ] `backends/backend.go`: change `Backend.Run` to `Run(ctx, tool string, input []byte, log io.Writer) ([]byte, error)`. Update `localBackend.Run` (tee the subprocess's combined output to `log`) and `modalBackend.Run` (write the returned payload to `log`). Update any backends tests / `Client` plumbing as needed.
- [ ] `design.go` and `foldjob.go`: the `Spec{Run: ...}` closure takes the new `log io.Writer` param and forwards it to `t.backend.Run(ctx, t.name, input, log)`. Update the `stubBackend.Run` in `design_test.go` and `foldjob_test.go` to the new signature (they can ignore `log` or write `input` to it).
- [ ] `setup.go`: the three closures (`install`, `uninstall`, `modal deploy`) take the new `log io.Writer` and write subprocess output to it. Remove the manual `os.Create(logPath)` handling and the `setupLogPath` helper; drop "log: <path>" from the start messages (the in-chat block replaces it).
- [ ] `cmd/proteus/main.go`: after `mgr := jobmgr.NewManager(st, nil)`, call `mgr.SetLogDir(filepath.Join(proteusHome(), "logs"))`.
- [ ] Verify: `gofmt -l` empty, `go vet ./...` clean, `go build ./...` clean, `go test ./...` all pass. Commit.

## Task B: in-chat job-log block (parallel, after A)

**Owns (only these):** `internal/tui/chat.go`, `internal/tui/chat_test.go`.

**Read first:** `internal/tui/chat.go` (the `entryKind` enum, `chatEntry`, `renderEntries`, the `⏺`/`⎿` tool-trace rendering), `internal/tui/theme.go`, design doc §4.4.

- [ ] Add `entryJobLog` to the `entryKind` enum. Extend `chatEntry` with the fields a job-log block needs (job id, tool name, status, started time, `tail []string`) — keep all existing fields and the `append*` signatures.
- [ ] Add `func (c *chatModel) upsertJobLog(id, tool string, status domain.JobStatus, started *time.Time, tail []string)` — finds the existing `entryJobLog` for `id` and updates it in place, or appends a new one. Then `refresh()`.
- [ ] In `renderEntries`, render `entryJobLog`: a header line `<glyph> <tool> · <id> · <elapsed>` (reuse the `glyph`/`statusColor` helpers and the elapsed style) and the dim `tail` lines indented under a `⎿` connector, like the tool traces.
- [ ] Tests (`chat_test.go`, prefix `TestChatJobLog`): `upsertJobLog` once then again with the same id updates one block (entry count unchanged); `renderEntries` shows the tool name and a tail line.
- [ ] Verify: `go build ./internal/tui/`, `go test ./internal/tui/ -run TestChatJobLog`, `gofmt -w`.

## Task C: full-screen log view (parallel, after A)

**Owns (only these):** `internal/tui/joblog.go`, `internal/tui/joblog_test.go` (new).

**Read first:** `internal/tui/chat.go` (the `bubbles/viewport` usage in `chatModel`), `internal/tui/modal.go` (the overlay `view` pattern), `internal/tui/theme.go`, design doc §4.5.

- [ ] `readLog(path string) string` — read a whole log file; "" (no error) when the file is missing.
- [ ] `tailLines(path string, n int) []string` — the last `n` lines of a log file; empty when missing.
- [ ] `jobLogView` model wrapping a `bubbles/viewport`: `newJobLogView(th Theme)`, `setSize(w, h int)`, `setContent(header, body string)`, `update(tea.KeyMsg)` routing `↑/↓/PgUp/PgDn` to the viewport, `View() string` (header line + the viewport).
- [ ] Tests (`joblog_test.go`, prefix `TestJobLog`): `readLog`/`tailLines` over a temp file (and a missing path); `jobLogView.setContent` + `View()` contains the header and body.
- [ ] Verify: `go build ./internal/tui/`, `go test ./internal/tui/ -run TestJobLog`, `gofmt -w`.

---

## Task D: Integration (orchestrator, after A/B/C)

**Files:** `internal/tui/app.go`, `internal/tui/app_test.go`.

- [ ] Add a `jobLogView` field and a unified focus model. The `panelFocus` cycle becomes: chat → one stop per running job → jobs → designs → lab → chat.
- [ ] On `refreshMsg`: for every job from `ListJobs`, `chat.upsertJobLog(...)` with `tailLines(job.LogFile, 6)`.
- [ ] Add `overlayJobLog`. When Tab focus lands on a running job, open the overlay with that job's `jobLogView` populated from `readLog(job.LogFile)` (refreshed each tick while open). `handleKey` routes scroll keys to the view and `Esc` returns to chat. `View` renders the overlay.
- [ ] Update `app_test.go` for the new focus cycle; add a test that a `refreshMsg` with a job whose `LogFile` exists produces an `entryJobLog` block.
- [ ] Verify: `gofmt -l` empty, `go vet ./...` clean, `go build ./...` clean, `go test ./...` all pass. Commit.

---

## Self-Review checklist

- [ ] Every design-doc §4 component maps to a task.
- [ ] `NewManager`'s signature is unchanged (only `SetLogDir` is added) — no test-call ripple.
- [ ] No Phase B task edits `app.go`, `cmd/proteus/`, or a Phase A file.
- [ ] Tasks B and C touch different files.
- [ ] `go test ./...` and `go vet ./...` clean after Task D.
