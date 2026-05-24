# fova — Performance Batch 2: Specs Cache + DB Indexes + Streaming Coalescer

**Spec date:** 2026-05-23
**Status:** Implementation-ready
**Author:** Alvaro (brainstormed with Claude Code)
**Scope:** `internal/tools/registry.go`, `internal/store/schema.sql`, `internal/tui/chat.go`, `internal/tui/app.go`

## 1. Summary

The audit (conversation 2026-05-23) surfaced nine perf opportunities. Batch 1 landed the top three (chat render cache, concurrent batched tool calls, SQLite WAL). This batch lands the next three:

1. **Cache `Registry.Specs()`** (audit #5) — stop rebuilding + sorting ~40 tool specs on every agent loop iteration.
2. **Right indexes for `ListJobs` / `ListExperiments`** (audit #7) — the current `idx_jobs_status(status, created)` doesn't match the actual workload (`WHERE project_id ORDER BY created`); `experiments` has no index at all.
3. **Streaming-tick coalescer at 30 FPS** (audit #3) — `appendAgentDelta` runs `refresh()` per token; coalesce into a `tea.Tick(33ms)` so we cap viewport copies at ~30/s while a turn streams.

They are batched because each is small, independent, low-risk, and shares the same review/verification surface (one PR).

## 2. Current behaviour and pain points

### 2.1 `Registry.Specs()` rebuilds every agent loop iteration

`internal/tools/registry.go:84-99` — `Specs()` allocates a fresh `[]llm.ToolSpec`, iterates the `tools` map, builds ~40 `ToolSpec` structs, then runs a hand-rolled insertion sort. It is called from `Loop.Run` at the top of every iteration (`internal/agent/loop.go:107` in the `req.Tools = l.registry.Specs()` line) — every LLM round-trip pays this cost.

The registry is populated once in `cmd/fova/main.go` (35+ `Register` calls) before the agent loop starts; it is essentially immutable after that. There is no reason to rebuild.

### 2.2 Missing DB indexes for the actual list workloads

`internal/store/schema.sql:67` declares:

```sql
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status, created);
```

But `ListJobs` (`internal/store/jobs.go:44`) runs:

```sql
SELECT … FROM jobs WHERE project_id=? ORDER BY created DESC, rowid DESC
```

SQLite can't use `idx_jobs_status` to satisfy `WHERE project_id` — wrong leading column. It falls back to a full scan + sort. With v0.2's single-project model that's fine; as job history grows past a few hundred rows the scan becomes the dominant cost of the jobs pane.

`experiments` has **no index at all**. `ListExperiments` (`internal/store/experiments.go:58`) runs `WHERE project_id=? ORDER BY submitted DESC, rowid DESC` — also a full scan + sort.

### 2.3 Streaming refreshes per token

`internal/tui/chat.go:124-133` — `appendAgentDelta` appends to the last agent entry's `.text`, clears its `rendered` cache (good, from batch 1), and calls `c.refresh()`. `refresh()` runs `renderEntries()` and copies the result into `viewport.SetContent` plus `viewport.GotoBottom()`.

Batch 1 made `renderEntries()` itself O(1) per token (cache hits on every prior entry, one glamour render on the streaming one). What remains is the per-token viewport copy, the per-token lipgloss style application on the rendered slice, and Bubble Tea's per-token redraw to the terminal. At 100 tok/sec on a fast local model that's 100 redraws/sec — wasteful and visually no smoother than 30.

## 3. Goals / non-goals

**Goals**
- Eliminate the per-iteration allocation + sort in `Specs()` so it returns a stable cached slice after first call.
- Make `ListJobs` and `ListExperiments` index-driven (covering scan, no sort).
- Cap streaming-turn redraws at ~30 FPS regardless of token rate, without dropping any tokens visible at end-of-turn.

**Non-goals**
- Rebuilding the tools registry as immutable-by-construction. We keep the existing `Register` API; we just invalidate the cache when it's called.
- Migrating away from SQLite or changing the schema for the existing tables.
- A general per-component tick scheduler. The coalescer is scoped to the streaming-agent-text path; tool-traces, job-logs, and other UI updates stay synchronous.
- Lazy markdown rendering, viewport diffing, or any other large UI refactor.

## 4. Design — #5 Cache `Registry.Specs()`

### 4.1 Lazy cache, invalidated on Register

`Registry` gains a private cache field:

```go
type Registry struct {
    tools map[string]Tool
    specs []llm.ToolSpec // built lazily; cleared on Register
}
```

`Register` invalidates the cache:

```go
func (r *Registry) Register(t Tool) {
    r.tools[t.Name()] = t
    r.specs = nil
}
```

`Specs` builds-on-miss using `sort.Slice` (replacing the hand-rolled insertion sort — same result, idiomatic, easier to read):

```go
func (r *Registry) Specs() []llm.ToolSpec {
    if r.specs == nil {
        specs := make([]llm.ToolSpec, 0, len(r.tools))
        for _, t := range r.tools {
            specs = append(specs, llm.ToolSpec{
                Name:        t.Name(),
                Description: t.Description(),
                InputSchema: t.InputSchema(),
            })
        }
        sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
        r.specs = specs
    }
    return r.specs
}
```

### 4.2 Concurrency contract

`Registry` is constructed and populated in `cmd/fova/main.go` on the main goroutine before the agent loop is spawned. After that, `Specs()` is called from the agent goroutine via `Loop.Run`, and `Register` is never called concurrently. A new doc comment on `Registry` makes this explicit:

> Registry is not safe for concurrent Register/Specs calls. In fova, all Register calls happen on the main goroutine before the agent loop starts; afterwards Specs is read-only from the agent goroutine.

We deliberately do not add a mutex — the contract is "build at startup, read at runtime", and a mutex would suggest otherwise. Tests that need to register tools dynamically (only `internal/tools/registry_test.go` and a handful of agent tests) do so before exercising `Specs()`, so the contract holds there too.

### 4.3 Tests

- `TestSpecsCachedAcrossCalls`: register two tools, call `Specs()` twice, assert the returned slice headers are identical (`&specs1[0] == &specs2[0]` — same backing array).
- `TestSpecsInvalidatedOnRegister`: call `Specs()`, register a new tool, call `Specs()` again, assert (a) the second result has one more entry, (b) the slice header is different (cache was rebuilt).
- The existing `Specs()` correctness test (whatever asserts the sorted order today) keeps working — `sort.Slice` produces identical output to the hand-rolled insertion sort.

## 5. Design — #7 Right indexes for `ListJobs` / `ListExperiments`

### 5.1 Schema changes

Append to `internal/store/schema.sql`:

```sql
CREATE INDEX IF NOT EXISTS idx_jobs_project ON jobs(project_id, created);
CREATE INDEX IF NOT EXISTS idx_experiments_project ON experiments(project_id, submitted);
```

Keep `idx_jobs_status` as-is — it is unused by the current query mix but harmless, and `MarkRunningJobsInterrupted` (which filters on `status`) could benefit if it ever needs a covering scan. Dropping it would require a migration step for no gain.

### 5.2 Migration

`schema.sql` is run via `db.Exec(schemaSQL)` on every `Open` (`internal/store/store.go:43`), and `CREATE INDEX IF NOT EXISTS` is idempotent. Existing per-project DBs pick up the new indexes on the next launch — no migration code, no user-visible step.

### 5.3 Tests

Two `EXPLAIN QUERY PLAN` assertions, mirroring the SQLite plan-format convention:

```go
func TestListJobsUsesProjectIndex(t *testing.T) {
    st := openTestStore(t)
    var plan string
    rows, err := st.db.Query(
        `EXPLAIN QUERY PLAN SELECT id FROM jobs WHERE project_id=? ORDER BY created DESC`,
        string(DefaultProjectID),
    )
    // …iterate rows.Scan(&id, &parent, &notused, &detail)…
    // assert detail mentions "idx_jobs_project"
}
```

Same shape for `experiments` → `idx_experiments_project`. The plan-format check is "string contains the index name" which is stable across SQLite versions.

A second-tier test (optional, can be added later) would be a "list-many-rows benchmark" comparing pre/post index timing — out of scope for this batch.

## 6. Design — #3 Streaming-tick coalescer at 30 FPS

### 6.1 Buffer in `chatModel`

Add two fields to `chatModel`:

```go
type chatModel struct {
    // ... existing fields ...
    pendingDelta string // accumulated tokens since the last flush
    pendingDirty bool   // true when pendingDelta has unflushed content
}
```

### 6.2 `appendAgentDelta` becomes non-rendering

```go
func (c *chatModel) appendAgentDelta(delta string) {
    c.pendingDelta += delta
    c.pendingDirty = true
}
```

It no longer calls `refresh()`. It also no longer mutates `c.entries`. Those happen at flush time.

### 6.3 New `flushPendingDelta`

```go
// flushPendingDelta drains pendingDelta into the last agent entry (creating
// one if needed), invalidates that entry's cache, and refreshes the viewport.
// Called at most ~30/s during streaming via streamFlushMsg, and once on
// TurnDoneMsg/TurnErrorMsg as a guaranteed final flush.
func (c *chatModel) flushPendingDelta() {
    if !c.pendingDirty {
        return
    }
    delta := c.pendingDelta
    c.pendingDelta = ""
    c.pendingDirty = false

    if n := len(c.entries); n > 0 && c.entries[n-1].kind == entryAgent {
        c.entries[n-1].text += delta
        c.entries[n-1].rendered = ""
    } else {
        c.entries = append(c.entries, chatEntry{kind: entryAgent, text: delta})
    }
    c.refresh()
}
```

### 6.4 Tick lifecycle in `Model`

A new message and command:

```go
type streamFlushMsg struct{}

const streamFlushInterval = 33 * time.Millisecond // ~30 FPS

func scheduleStreamFlush() tea.Cmd {
    return tea.Tick(streamFlushInterval, func(time.Time) tea.Msg { return streamFlushMsg{} })
}
```

`Model` gains a `streamFlushScheduled bool` field. Lifecycle:

- **`TextDeltaMsg` handler** in `Update` calls `m.chat.appendAgentDelta(msg.Delta)`. If `!m.streamFlushScheduled`, it sets the flag and returns `scheduleStreamFlush()` as the command.
- **`streamFlushMsg` handler** calls `m.chat.flushPendingDelta()`. If a turn is still in progress (tracked by an existing `m.streaming bool` flag — see §6.5), it returns `scheduleStreamFlush()` to chain the next tick. Otherwise it clears `m.streamFlushScheduled` and returns no command.
- **`TurnDoneMsg` / `TurnErrorMsg` handlers** call `m.chat.flushPendingDelta()` (guaranteed final flush) and clear `m.streamFlushScheduled`. The next streamFlushMsg that arrives after this is a no-op (pendingDirty is false; no chain).

### 6.5 Streaming-state tracking

`Model` already has a `m.running bool` that tracks "turn in progress" — set when the user submits a turn, cleared in the `TurnDoneMsg` / `TurnErrorMsg` handlers (`internal/tui/app.go:446, 453`). The spinner's `spinnerTickMsg` handler (line 381) keys off it. The streaming coalescer reuses this exact flag:

- `streamFlushMsg` handler chains the next tick only when `m.running` is true.
- `TurnDoneMsg` / `TurnErrorMsg` already clear `m.running`; the final flush in their handlers naturally lets the chain terminate.

No new "is a turn in progress" state is added.

### 6.6 Tool-trace path stays synchronous

`appendToolStart`, `appendToolDone`, `upsertJobLog`, `appendUser`, `appendError`, `appendSlashOutput`, `appendRaw`, `appendAgentDeltaBlock` — **none change**. They continue to call `refresh()` immediately. Only the per-token `appendAgentDelta` stream is coalesced. This keeps:

- The `⏺ tool` → `⏺ tool (123ms)` transition feeling instant.
- Job-log block updates from background ticker (already 1s cadence) snappy.
- User input echo immediate (no perceptible lag between typing Enter and seeing the message).

### 6.7 Edge cases

- **First delta of a turn** — pendingDelta accumulates; `streamFlushScheduled` flips from false to true; one tick is scheduled. The first flush happens 33ms later (with all deltas received in that window).
- **Token arrives between flush and TurnDoneMsg** — `flushPendingDelta` on TurnDoneMsg drains it. Final state is exact.
- **TurnDoneMsg with no streamed text** (rare: pure tool-call turn) — `pendingDirty` is false; `flushPendingDelta` is a no-op; correct.
- **Mid-stream resize** — `resize` already invalidates the entire cache; the next streamFlushMsg re-renders the streaming entry with the new wrap width. No special handling.
- **Cancellation** — `TurnErrorMsg` path forces a final flush, so any tokens emitted just before the cancel are visible.

### 6.8 Tests

- **`TestChatStreamingCoalesces30FPS`** — use a counting `mdRenderer` fake. Call `appendAgentDelta` 50 times (no ticks). Assert `Render` was called 0 times (still buffered) and `c.entries` either has no agent entry or the agent entry's text is the empty/initial value. Then deliver one `streamFlushMsg` equivalent (call `flushPendingDelta`). Assert one `Render` happened. (The test exercises `chatModel` directly without the tick — simpler than driving Bubble Tea time.)
- **`TestChatFlushOnTurnDone`** — accumulate deltas, then call `flushPendingDelta` (simulating TurnDoneMsg path). Assert final text contains all accumulated deltas.
- **`TestModelStreamFlushTickIsRescheduled`** (in `app_test.go`) — send `TextDeltaMsg`, capture the returned `tea.Cmd`, assert it is non-nil (a tick is scheduled). Send another `TextDeltaMsg` immediately; assert NO new tick is scheduled (the flag prevents pile-ups). Send `streamFlushMsg`; assert a new tick is scheduled (chain). Send `TurnDoneMsg`; assert no new tick.
- **`TestModelStreamFlushStopsAtTurnEnd`** — drive through a complete fake turn; assert that after `TurnDoneMsg`, a final `streamFlushMsg` results in no further tick being scheduled.

## 7. Order of work

#5 and #7 are mechanical; #3 is the largest. They share no code surface, so the order is mostly arbitrary. Recommended:

1. **#5 cache Specs()** — single file, single test file, lowest risk. Lands first as a confidence-builder.
2. **#7 DB indexes** — schema.sql + two test functions in `store_test.go`. Independent of #5.
3. **#3 streaming coalescer** — chat.go + app.go + tests. The largest change.

All independent — could land in parallel as worktree-isolated agents off the same base. Merge order is irrelevant since they touch disjoint files.

## 8. Backwards compatibility & risk

- **Specs cache**: invisible behavior change after correctness verified (sort order identical). The "no mutex" choice is documented and matches existing usage.
- **DB indexes**: additive, idempotent migration; older DB files upgrade transparently. Index size is small (one row pointer per job/experiment, 24 bytes × few hundred rows).
- **Streaming coalescer**: visible behavior change — streamed text appears in ~33ms chunks instead of per-token. With token rates of 50-100/s this means 2-3 tokens per chunk, imperceptible. With slower token rates (e.g. 5/s on cold models), each token still triggers its own tick, so the streaming feel is unchanged. Worst-case correctness bug is a stuck buffer — caught by the TurnDone flush invariant tests.

## 9. Out of scope (deliberately deferred)

- #6 parallel LLM streaming
- #8 idle wake-up coalescing (sibling to #3 — pause the 1s job-poll tick when no jobs are running)
- #9 viewport diffing

#8 could pair naturally with #3 in a follow-up batch — both are about reducing wasted ticks.
