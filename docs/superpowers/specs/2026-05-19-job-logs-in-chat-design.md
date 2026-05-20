# Proteus — Live job logs in the chat — Design

**Date:** 2026-05-19
**Status:** Approved, ready for planning
**Source of truth:** this document; `docs/SPECS.md` §10 (TUI), §13 (jobs).

## 1. Goal

When a job runs (`/install`, `/modal deploy`, or an agent-submitted compute job
such as `design.bindcraft`), surface its live output in the TUI: a compact,
auto-updating log block in the chat, and a Tab-focusable full-screen view of the
complete log. Today a job's subprocess output goes only to a file the TUI never
reads — the user sees a one-line "started job …" and a status glyph.

## 2. Scope

**In scope:** every job kind (`JobSetup`, `JobCompute`, `JobLab`). A compact
in-chat log block per job; a full-screen scrollable log view; a unified Tab
focus cycle.

**Out of scope:** no change to what jobs *do*; no new job kinds; no
bus-streaming protocol (see decision 1); the wet-lab webhook surface is
unchanged.

## 3. Key decisions

1. **File-tailing on the existing 1 s tick, not bus-streaming.** The TUI already
   fires a `refreshMsg` every second (`scheduleRefresh`) to reload the
   jobs/designs panels. The job-log blocks update on that same tick by reading
   the tail of each running job's log file. This avoids a chatty job flooding
   the 256-slot agent bus and needs no new message type. ~1 s latency is fine
   for a progress view.
2. **The `jobs.Manager` owns one log file per job**, at `<logDir>/<jobID>.log`.
   `domain.Job` gains a `LogFile` field recording the path. Setup commands stop
   creating their own log files — the Manager does it uniformly for every job.
3. **`jobs.Spec.Run` and `backends.Backend.Run` each gain an `io.Writer`.** The
   Manager passes `Run` a writer onto the job's log file; a job writes its
   subprocess output there. Compute jobs reach the same writer because
   `Backend.Run` now forwards it (the local backend streams its subprocess's
   combined output; the Modal backend writes the result it receives).
4. **The full-screen log view is an overlay**, like the picker / submit modal —
   not an inline-expanding chat entry (a long log would fight the chat
   viewport).
5. **Tab is a unified focus cycle**: chat → each running job's full-screen log →
   the side panels → chat; `Esc` always returns to chat.

## 4. Components

### 4.1 `internal/domain/types.go`

`Job` gains `LogFile string` — the path to the job's log file.

### 4.2 `internal/jobs/manager.go`

- `Spec.Run` becomes
  `func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error)`.
- `NewManager` gains a `logDir string` argument.
- When a job starts, the Manager creates `<logDir>/<jobID>.log`, sets
  `job.LogFile`, opens it, passes it as `log` to `spec.Run`, and closes it when
  the job ends.

### 4.3 `internal/backends/`

`Backend.Run` becomes
`Run(ctx, tool string, input []byte, log io.Writer) ([]byte, error)`.
The local backend tees its subprocess's combined output to `log`; the Modal
backend writes the returned payload to `log`. The two callers — the design tool
(`design.go`) and the fold tool (`foldjob.go`) — forward the `log` they receive
from `Spec.Run`.

### 4.4 `internal/tui/chat.go` — the in-chat block

A new `entryJobLog` entry kind. The entry holds a job ID, tool name, status, and
the last ~6 log lines. `chatModel.upsertJobLog(id, tool, status, started, tail)`
creates the block the first time a job is seen and updates it in place
thereafter. It renders like the tool traces: a header
`⟳ install bindcraft · j_8f2a · 1m20s` and the dim, `⎿`-indented tail.

### 4.5 `internal/tui/joblog.go` (new) — the full-screen view

A `jobLogView` model wrapping a `bubbles/viewport`: a header (tool, status,
elapsed), the complete log body, `↑/↓/PgUp/PgDn` scrolling. Plus helpers
`readLog(path)` and `tailLines(path, n)` for reading a job's log file.

### 4.6 `internal/tui/setup.go`

The three setup-job closures (`install`, `uninstall`, `modal deploy`) write
their subprocess output to the `log` writer the Manager provides; the manual
`os.Create` / `setupLogPath` log-file handling is removed.

### 4.7 `internal/tui/app.go` — wiring

- On each `refreshMsg`, for every job in `ListJobs`, read the tail of
  `job.LogFile` and call `chat.upsertJobLog`.
- Tab steps the unified focus cycle; when focus lands on a running job, the
  `overlayJobLog` overlay renders that job's `jobLogView`.
- `handleKey` routes scroll keys to `jobLogView` while the overlay is open;
  `Esc` returns to chat.

## 5. Data flow

`Spec.Run` writes a line → the Manager's per-job log file. Each second the TUI's
`refreshMsg` reads the tail of every running job's `LogFile` → `upsertJobLog`
updates that job's in-chat block. Tab opens the full-screen `jobLogView`, which
reads the complete file (and re-reads it on the tick while focused).

## 6. Error handling

- A missing or unreadable log file → the block shows "(no output yet)"; never a
  crash.
- A job with no `LogFile` (e.g. an instant job) → no block.
- The Manager failing to create the log file → the job still runs; `LogFile` is
  empty and no block appears (logged, not fatal).

## 7. Testing

Offline and deterministic:
- `jobs`: a `Spec.Run` that writes known lines to `log`; assert the file is
  created and `Job.LogFile` is set.
- `backends`: the local backend tees subprocess output to the writer.
- `chat.go`: `upsertJobLog` creates then updates one block; `renderEntries`
  shows the header and tail.
- `joblog.go`: `readLog` / `tailLines` over a temp file; `jobLogView` renders
  the header and content.
- `app.go`: a `refreshMsg` with a job whose `LogFile` exists updates the chat;
  Tab cycles focus onto the job and opens the overlay.

## 8. Acceptance criteria

1. Running `/install <tool>` shows a live, auto-updating log block in the chat.
2. An agent-submitted compute job likewise shows a live block.
3. Tab cycles focus through running jobs; focusing one opens a full-screen,
   scrollable view of its complete log; `Esc` returns to chat.
4. The Manager writes one `<jobID>.log` per job and records it on `Job.LogFile`.
5. `go test ./...` passes and `go vet ./...` is clean.

## 9. Execution approach

Invoke `writing-plans`, then implement with Opus subagents: a foundation task
(the `jobs` / `backends` / `domain` signature changes — cross-cutting, done
first), then two parallel tasks for the TUI components (`chat.go` block,
`joblog.go` view), then an integration pass wiring `app.go`.
