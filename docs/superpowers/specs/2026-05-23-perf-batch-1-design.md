# fova — Performance Batch 1: Chat Cache + Concurrent Tools + SQLite WAL

**Spec date:** 2026-05-23
**Status:** Implementation-ready
**Author:** Alvaro (brainstormed with Claude Code)
**Scope:** `internal/tui/chat.go`, `internal/agent/loop.go`, `internal/tools/`, `internal/store/store.go`

## 1. Summary

The audit (see conversation 2026-05-23) surfaced nine optimization opportunities. This spec lands the top three together as the first perf batch:

1. **Chat render cache.** Stop re-running glamour over every prior chat entry on every streaming token; cache per-entry markdown output and only re-render the entry whose text changed.
2. **Concurrent batched tool calls.** When the LLM emits N tool calls in a single response, run the ones that opt in concurrently with `errgroup`, preserving result ordering and the existing confirmation modal contract.
3. **SQLite WAL mode.** Move the per-project DB to WAL with a `busy_timeout`, drop the `SetMaxOpenConns(1)` workaround, and let job writers stop serialising behind the foreground.

They are batched because they share validation surface (the streaming/agent loop) and ship as one PR sized to a single review pass.

## 2. Current behaviour and pain points

### 2.1 Chat re-renders every entry on every token
`internal/tui/chat.go:323` — `refresh()` is called from every `appendAgentDelta` (line 105). `refresh()` calls `renderEntries()` (line 287), which loops every entry; the `entryAgent` branch (line 294) runs `c.renderer.Render(e.text)` (glamour) on every entry every time. After 50 entries of conversation, every streamed token re-pays the full markdown cost of all 50. The streaming feel becomes laggy proportional to conversation length, especially on Opus turns with long tool traces above.

### 2.2 Batched tool calls run strictly serially
`internal/agent/loop.go:163-166`:
```go
for _, tc := range resp.ToolCalls {
    display := l.executeTool(ctx, tc)
    l.session.AddToolResult(tc.ID, display)
}
```
When the model emits e.g. `[knowledge.uniprot, knowledge.s2, knowledge.pdb]` in one turn — three independent network reads — they run end-to-end. Each adds 200-800ms wall-clock to the turn. Most knowledge / search tools are pure reads with no shared mutable state and could run concurrently.

### 2.3 SQLite serialises every writer
`internal/store/store.go:36` opens with `_pragma=foreign_keys(1)` only; no WAL. Line 51 sets `SetMaxOpenConns(1)` because background job goroutines were producing `SQLITE_BUSY` against the foreground writer. With WAL + `busy_timeout`, readers don't block writers and a 5s timeout absorbs short contention windows. Today every job-status update queues behind the previous one, and the TUI's status read blocks job writers.

## 3. Goals / non-goals

**Goals**
- Make streaming feel constant-time in conversation length: glamour runs only on the entry currently being appended to.
- Cut wall-clock for batches of read-only tool calls roughly by N (where N is the batch size) while keeping the confirmation modal, biosecurity guard, and per-tool ordering of results unchanged.
- Eliminate `SQLITE_BUSY` under concurrent job writes without changing schema or domain code.
- Ship the three changes as one PR with one round of review.

**Non-goals**
- Coalescing streaming-token ticks (audit item #3). Out of scope for batch 1; revisit if the cache alone doesn't smooth the UI on long turns.
- Caching tool specs (`Registry.Specs()` allocation, audit item #5). Cheap to do later as a one-line addition.
- Parallelising LLM streaming itself (audit item #6).
- Migrating an existing DB. WAL is per-connection and applied at `Open` time — existing DBs upgrade transparently the next time fova starts.
- Adding "max concurrency" / a worker pool. With batches up to ~10 calls and per-tool network limits already in each tool, an unbounded `errgroup` per turn is fine.

## 4. Design — #1 Chat render cache

### 4.1 Cache field on `chatEntry`
Add a single field to `chatEntry` (`internal/tui/chat.go:40`):

```go
type chatEntry struct {
    // ... existing fields ...
    rendered string // cached output of the per-kind renderer for this entry
}
```

`rendered` is the **fully styled string** the entry contributes to the viewport, including the trailing `\n\n` separator handled by `renderEntries`. Empty string means "needs render".

### 4.2 Invalidation rule
The cache is invalidated whenever the text the entry renders from changes — and **only** then. That gives us one invalidator per append site, and every append site is already a method on `chatModel`:

| Method                | Invalidation                                             |
|-----------------------|----------------------------------------------------------|
| `appendUser`          | new entry; `rendered=""`                                 |
| `appendAgentDelta`    | clear `rendered` on the agent entry being appended to (or create new) |
| `appendAgentDeltaBlock` / `appendSlashOutput` / `appendRaw` / `appendError` | new entry; `rendered=""` |
| `appendToolStart`     | new entry; `rendered=""`                                 |
| `appendToolDone`      | clear `rendered` on the entry transitioning to done      |
| `upsertJobLog`        | clear `rendered` on the matched entry (status/tail change) |
| `resize`              | clear `rendered` on **every** entry — the glamour wrap width changes |
| `(/clear)`            | new entries slice; cache is per-entry so naturally gone |

The theme change path (`Theme`) is reachable through `resize`'s caller in `tui/app.go` — the `cmd/onboarding` re-run resizes on overlay close, and theme changes today force a full repaint. We piggy-back on `resize`'s invalidation.

### 4.3 `renderEntries` becomes cache-aware
```go
func (c *chatModel) renderEntries() string {
    var b strings.Builder
    for i := range c.entries {
        e := &c.entries[i]
        if e.rendered == "" {
            e.rendered = c.renderEntry(e) + "\n\n"
        }
        b.WriteString(e.rendered)
    }
    return b.String()
}
```

The existing per-kind rendering (lines 290-318) moves into a small `renderEntry(e *chatEntry) string` helper that returns the styled block **without** the trailing `\n\n`. `renderEntries` adds the separator and concatenates.

Crucially, `renderEntry` is called with `*chatEntry` (pointer, not copy) so the cache field write lands on the slice element.

### 4.4 Streaming entry stays warm
`appendAgentDelta` extends the in-progress entry's text and clears its `rendered`. The next `refresh()` re-renders only that one entry; every entry above it stays cached. Glamour cost per token becomes O(1) in conversation length.

A subtle case: `entryTool` running-vs-done — `appendToolStart` creates the entry (cache empty), and `appendToolDone` clears the cache on the same entry. The glyph/duration changes are caught.

### 4.5 Test coverage
- `TestChatCacheReusesRenderForUnchangedEntries`: append two agent deltas, count `renderer.Render` calls (via a counting renderer fake or a `c.renderer = ...` test seam). After the second append, only the second entry's render counter should advance.
- `TestChatCacheInvalidatedOnResize`: after `resize`, every entry's `rendered` is empty before the next `renderEntries`.
- `TestChatCacheInvalidatedOnToolDone`: a running tool entry's cache clears when `appendToolDone` is called for it.
- `TestChatCacheInvalidatedOnUpsertJobLog`: an existing job-log block's cache clears when its tail/status updates.

The counting-renderer seam: introduce a small `mdRenderer` interface wrapping `Render(string) (string, error)`, defaulted to `*glamour.TermRenderer`, replaceable in tests. The existing `renderer` field changes type but the embed is otherwise mechanical.

## 5. Design — #2 Concurrent batched tool calls

### 5.1 Opt-in via Go's optional-interface idiom
Tools are not modified in bulk. We add a separate interface:

```go
// Concurrent, optionally implemented by a Tool, marks it as safe to run in
// parallel with other concurrent-safe tools in the same batched tool-call
// response. Tools without this method are treated as non-concurrent.
type Concurrent interface {
    Concurrent() bool
}
```

A tool opts in by adding `func (*X) Concurrent() bool { return true }`. The agent loop type-asserts:

```go
func isConcurrent(t Tool) bool {
    c, ok := t.(Concurrent)
    return ok && c.Concurrent()
}
```

### 5.2 Tools that opt in (batch 1)
Pure read-only network / filesystem inspectors, no shared mutable state, no UI side-effects beyond their own `ToolStartMsg`/`ToolDoneMsg`:

- `fs.read`, `fs.read_structure`
- `knowledge.uniprot`, `knowledge.pdb`, `knowledge.biorxiv`, `knowledge.europepmc`, `knowledge.crossref`, `knowledge.openalex`, `knowledge.s2`, `knowledge.interpro`, `knowledge.web_search`, `knowledge.web_fetch`, `knowledge.local_pdfs`
- `knowledge.corpus_search`, `knowledge.corpus_grep`, `knowledge.corpus_read`, `knowledge.corpus_map` (read-only corpus queries; `corpus_add`, `corpus_add_from_search`, `corpus_remove`, `corpus_reduce` do **not** opt in — they mutate the corpus index)
- `score.metrics`, `score.filter`, `score.ipsae`
- `viz.ascii_structure`, `viz.contact_map`, `viz.metric_plot` (pure-Go renderers, no external process)
- `jobs.list`, `jobs.status`, `jobs.result` (read-only DB queries — they are SQLite reads, which under WAL no longer block writers)
- `lab.targets_search`, `lab.cost_estimate`, `lab.experiment_status`, `lab.results` (read-only Adaptyv API queries)
- `knowledge.blast`, `knowledge.paperclip` (network reads; no local mutation)

Explicitly **not** opting in (run serially even if batched with concurrent tools):
- `fs.write`, `fs.edit`, `fs.bash` — local mutation / arbitrary process execution.
- `plan.create` — writes the plan file.
- All design / fold job-launchers (`design.proteinmpnn`, `design.rfdiffusion`, `design.bindcraft`, `fold.esmfold`, `fold.boltz2`, etc.) — they enqueue jobs, write to the DB, and the existing job queue already handles concurrency.
- `lab.submit_experiment`, `design.*` — require confirmation modals (see §5.4).
- `jobs.cancel` — mutates job state.
- `viz.pymol_render` — spawns PyMOL externally.

Exact opt-in list is finalised in implementation as a checklist; the criteria above are the gate.

### 5.3 Execution plan in `loop.go`
The serial loop at `loop.go:163-166` is replaced with a two-phase partition:

```go
// 1. Partition the batch.
var serial, concurrent []llm.ToolCall
for _, tc := range resp.ToolCalls {
    t, ok := l.registry.Get(tc.Name)
    switch {
    case !ok:
        serial = append(serial, tc) // produces the "unknown tool" error message
    case isConcurrent(t) && !t.RequiresConfirmation(rawInput(tc)):
        concurrent = append(concurrent, tc)
    default:
        serial = append(serial, tc)
    }
}

// 2. Run concurrent calls in parallel.
results := make([]string, len(resp.ToolCalls)) // indexed in original order
indexOf := map[string]int{}                    // tc.ID -> original index
for i, tc := range resp.ToolCalls { indexOf[tc.ID] = i }

if len(concurrent) > 0 {
    g, gctx := errgroup.WithContext(ctx)
    for _, tc := range concurrent {
        tc := tc
        g.Go(func() error {
            display := l.executeTool(gctx, tc)
            results[indexOf[tc.ID]] = display
            return nil
        })
    }
    _ = g.Wait() // executeTool already swallows errors into display text
}

// 3. Run serial calls in original order, skipping slots already filled.
for i, tc := range resp.ToolCalls {
    if results[i] != "" || isConcurrentSlot(i, ...) { continue }
    results[i] = l.executeTool(ctx, tc)
}

// 4. Record results in original order — this is the ordering the model sees.
for i, tc := range resp.ToolCalls {
    l.session.AddToolResult(tc.ID, results[i])
}
```

Key invariants:

- **Result order shown to the model is identical to the order the model emitted the calls in** — we index by `tc.ID` and write back in original order. The model never sees out-of-order tool results.
- **`AddToolResult` is called only from the foreground goroutine** after all goroutines join. The `Session` is not made thread-safe; concurrency is contained inside `executeTool`'s registry/network work.
- **`executeTool` itself is goroutine-safe today.** It only reads `l.registry`, writes to `l.bus` (a channel), and calls `tool.Execute`. Tools that opt in must not mutate shared state.
- **The biosecurity guard** is consulted inside `executeTool` per call as today — concurrent calls each pass through `l.guard.Inspect` independently. Guard implementations are read-only (`internal/safety`) so this is safe.
- **`l.bus` already accepts concurrent sends** (it is a `chan<- tea.Msg` with a sender goroutine that fans into Bubble Tea's program). The chat trace contract changes below.

`errgroup.WithContext`: we use the `gctx`-cancellation behaviour so that if the user cancels the turn (Esc), every in-flight concurrent tool sees ctx cancellation. Today's serial loop already honours `ctx.Err()` between calls.

### 5.4 Confirmation modal stays single-threaded
`RequiresConfirmation` tools (`design.*`, `lab.submit_experiment`) **never** run concurrently. The partition in §5.3 forces them into the serial bucket, so the confirmation modal still blocks the agent loop exactly as today. The wider rule: a tool either opts in **and** never requires confirmation, or it stays serial. We enforce this with a guard test (§5.7) that fails the build if any tool returns `Concurrent()=true` and `RequiresConfirmation(...)=true`.

### 5.5 ToolStart / ToolDone carry the tool-call ID
The chat trace's `appendToolDone` (`chat.go:127-149`) currently matches "the most recent unfinished tool entry by name". Under concurrency two tools can be running simultaneously, so name-only matching is ambiguous.

Add an `ID string` field to `ToolStartMsg` and `ToolDoneMsg`:

```go
type ToolStartMsg struct { ID, Name string; Input json.RawMessage }
type ToolDoneMsg  struct { ID, Name string; Display string; Err error }
```

`executeTool` sets `ID: tc.ID` on both messages. `chat.go` adds a `toolCallID string` field to `chatEntry` and matches in `appendToolDone` by ID first, falling back to name-only for in-flight test code that doesn't supply an ID.

This is a strictly additive API change — callers that pass `ID: ""` still work and behave as today.

### 5.6 Goroutine-safety audit of the opt-in tools
Each tool in §5.2 is verified once during implementation:

- No package-level mutable state written during `Execute`.
- HTTP clients are fine concurrently (Go's `http.Client` is goroutine-safe; the per-tool clients are zero-value or constructed per-call).
- The DB read tools (`jobs.list`, `jobs.status`, `jobs.result`, `lab.experiment_status`, `lab.results`) hit SQLite — which under WAL (§6) allows concurrent reads with the writer. Without WAL these were serialised but harmless; with WAL they parallelise.
- `knowledge.corpus_search` / `corpus_grep` / `corpus_read` / `corpus_map` read from the corpus index files. They must not mutate any cache; verified that they do not.

If a tool is found to fail the audit, it stays out of the opt-in list. The opt-in list is conservative by design.

### 5.7 Test coverage
- `TestLoopRunsConcurrentToolsInParallel`: register two fake tools opting into `Concurrent`, each sleeping 100ms. Assert turn finishes in <150ms (parallel) not ~200ms (serial). Use a small slack to avoid CI flake.
- `TestLoopPreservesOrderingOfToolResults`: register fake tools with deterministic ordered IDs returning different strings; assert `Session` records them in original order regardless of completion order.
- `TestLoopSerializesNonConcurrentTools`: mix one concurrent + one non-concurrent tool; assert the non-concurrent one observes the concurrent one completed before it starts (or vice versa; what matters is they don't overlap).
- `TestLoopConfirmationStaysSerial`: a `RequiresConfirmation` tool emitted alongside a concurrent tool runs in the serial bucket; the confirm modal still blocks.
- `TestLoopCancellationStopsInFlightConcurrentTools`: cancel `ctx` mid-batch; both running tools see ctx cancellation.
- `TestToolNoConcurrentAndConfirmationOverlap`: iterate every registered tool in the production registry; fail if `Concurrent()=true && RequiresConfirmation(stub)=true`.
- `TestChatAppendToolDoneMatchesByID`: two `ToolStartMsg`s with different IDs, then a `ToolDoneMsg` for the second; assert only the second entry transitions to done.

## 6. Design — #4 SQLite WAL mode

### 6.1 DSN change
`internal/store/store.go:36`:

```go
const sqliteDSN = "file:%s?" +
    "_pragma=foreign_keys(1)&" +
    "_pragma=journal_mode(WAL)&" +
    "_pragma=busy_timeout(5000)&" +
    "_pragma=synchronous(NORMAL)"

db, err := sql.Open("sqlite", fmt.Sprintf(sqliteDSN, dbPath))
```

- `journal_mode=WAL`: writers don't block readers; readers don't block writers.
- `busy_timeout=5000`: a write that hits a 5s contention window retries internally rather than failing with `SQLITE_BUSY`.
- `synchronous=NORMAL`: under WAL this is the standard durability trade — a power-loss within the last commit can be lost; nothing in fova requires fsync-level durability per write (job state is reconstructable from logs).

### 6.2 Drop `SetMaxOpenConns(1)`
`store.go:51` becomes `db.SetMaxOpenConns(8)` (or simply removed — Go's default is unlimited, but a bound is defensive). With WAL the original `SQLITE_BUSY` motivation is gone, and concurrent job goroutines can each hold a connection.

### 6.3 Migration
WAL is per-database, applied by the first connection that sets the pragma. Existing per-project DBs upgrade transparently on the next `Open`. No schema migration, no user-visible step, no compatibility shim.

WAL produces sidecar files (`*-wal` and `*-shm`) alongside the DB. `.gitignore` already excludes the project data folder. Existing backup tooling (none yet) would need to know about the sidecar files if it ever ships; documented in §8.

### 6.4 Test coverage
- `TestStoreWALEnabled`: after `Open`, `SELECT journal_mode FROM pragma_journal_mode()` returns `wal`.
- `TestStoreBusyTimeoutSet`: `pragma_busy_timeout` returns 5000.
- `TestStoreConcurrentWrites`: launch 4 goroutines each inserting 50 job rows; all 200 succeed without `SQLITE_BUSY`. (This test would have failed under the old `MaxOpenConns=1` only if we artificially provoked contention; under WAL it's straightforwardly green.)
- `TestStoreReaderDoesNotBlockWriter`: a read transaction holds a row; a concurrent writer completes within `busy_timeout`. Verifies WAL semantics rather than DSN strings.

## 7. Order of work

The three items share no code surface, but #2 needs the chat-trace ID change (§5.5), and #1 touches `chat.go` heavily. Implement in this order to minimise rebase:

1. **#4 SQLite WAL** — smallest, no test cross-dependencies. DSN string + drop the connection-pool clamp. Land first; verifies the test seam.
2. **#1 Chat render cache** — touches `chat.go` only. The `mdRenderer` interface seam (§4.5) is introduced here.
3. **#2 Concurrent batched tools** — the `Concurrent` interface, the loop partition, the `ID` field on `ToolStartMsg`/`ToolDoneMsg`, the opt-in additions to the ~25 tools listed in §5.2, and the chat-ID-matching path in `appendToolDone`. Largest change; lands last.

Each lands as its own commit on `feat/perf-batch-1`, branched from `dev`.

## 8. Backwards compatibility & risk

- **Chat cache**: invisible to users when correct, and the test coverage (§4.5) prevents stale-cache regressions. Worst-case correctness bug is a stale entry — easy to diagnose and revert.
- **Concurrent tools**: opt-in defaults to off; any tool not explicitly opted in behaves identically to today. The `Concurrent`/`RequiresConfirmation` invariant is build-time-enforced (§5.7). The risk surface is the audited opt-in list; conservative-by-default.
- **WAL**: existing DBs upgrade transparently. Sidecar files (`-wal`, `-shm`) appear next to the DB file. The only externally observable change is faster, non-blocking reads.
- **Streaming UX**: cache-driven streaming should feel noticeably smoother on long turns; if it doesn't, audit item #3 (coalescing tick) is the follow-up.

## 9. Out of scope (deliberately deferred)

These were in the audit but are not in this batch:

- #3 streaming-tick coalescing
- #5 cached `Registry.Specs()`
- #6 parallel LLM streaming
- #7 PRAGMA-driven query plans
- #8 background-task wake-up coalescing
- #9 viewport diffing

Each can ship in a later batch without conflicting with this one.
