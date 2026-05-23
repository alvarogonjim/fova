# Editable tool-call review — implementation plan

**Spec:** `docs/superpowers/specs/2026-05-23-editable-review-design.md`
**Branch:** `feat/editable-review` (off `dev` at `8f5dfc5`)
**Approach:** Phase 1 (Foundation, sequential, in-session) → Phase 2 (parallel
Opus agents on file-disjoint streams) → Phase 3 (verify + merge to `dev`).

## Phase 1 — Foundation (sequential, in-session)

Touches the cross-cutting types and the only non-test caller of the loop's
`confirm` so the tree compiles end-to-end before parallel work begins.

1. **`internal/tools/tools.go`** — add the `Validator` sidecar interface:

   ```go
   type Validator interface {
       Validate(input json.RawMessage) error
   }
   ```

   No tool references it yet; compilation unchanged for all existing tools.

2. **`internal/agent/loop.go`** — widen `confirm` signature, update call site
   per spec §3.2. `NewLoop` / `NewLoopWithGuard` signatures change to match.

3. **`internal/agent/loop_test.go`** — update the six `NewLoop` /
   `NewLoopWithGuard` callers (lines 32, 62, 107, 154, 193, 231) to the new
   `confirm` shape: `func(prompt, name string, input json.RawMessage) (bool, json.RawMessage)`.

4. **`internal/agent/smoke_test.go`** — same shape update at line 50.

5. **`internal/tui/app.go`** — narrow shim to keep the build green:
   - widen `confirmCh chan bool` → `chan confirmReply` (new struct defined
     in this file or a new `confirm.go`);
   - update `confirmFn` to the new signature, returning
     `(r.accepted, r.input)`;
   - update the single send sites in `handleKey`'s `overlayConfirm` /
     `overlaySubmit` branches to send `confirmReply{accepted: true}` /
     `{accepted: false}` (no edit support yet — that lands in Phase 2A);
   - update the `agent.NewLoopWithGuard` call at line 780 — signatures
     match automatically since `confirmFn` is updated.

6. **`internal/tui/app_test.go`** — single read at line 148 updates from
   `v := <-m.confirmCh` (bool) to reading the struct's `accepted` field.

7. **Verify:** `go build ./...` clean; `go test ./internal/agent/...
   ./internal/tui/...` green.

After Phase 1 the editable surface is wired but no editor opens yet — the
modal is exactly today's binary gate, just over a richer channel. This is
the merge-safe foundation Phase 2's parallel streams build on.

## Phase 2 — Parallel implementation (two Opus agents on disjoint files)

Two file-disjoint streams. Each agent operates in its own worktree branched
from the Phase 1 commit.

### Stream A — TUI editable surface

**Files touched (exclusive):** `internal/tui/modal.go`,
`internal/tui/modal_test.go`, `internal/tui/app.go`,
`internal/tui/app_test.go`, plus a new `internal/tui/pendingfile.go`.

**Scope (spec §3.3 – §3.5):**

- `renderJSONModal(name, input, edited, theme, width, maxLines) string` in
  `modal.go` per spec §3.5; `modalModel.editable bool` flag.
- `confirmReply` struct co-located with `confirmCh` (already created in
  Phase 1 — extend if needed).
- `pendingInputDir func() string` and `openEditorFile func(path, initial string) tea.Cmd`
  hooks on `Model` for tests (spec §5).
- Pending-file helpers in `pendingfile.go`: path resolution, write seed
  content with `// fova:` header, strip-comments reader, `// ERROR:` retry
  header, cleanup, `.fova/.gitignore` hygiene (spec §3.4).
- Extend `editorFileDoneMsg` handler in `app.go` per spec §3.3: read +
  strip-comments + validate (via `tools.Validator` if the tool implements
  it) → re-render modal on success / re-open editor with `// ERROR:` on
  failure.
- Extend `handleKey`'s `overlayConfirm` branch with the `[e]` key per spec
  §3.3 keymap table.
- Extend `ConfirmRequestMsg` dispatch (`app.go:380–389`) per spec §3.5: keep
  `lab.submit_experiment` bespoke; route everything else through
  `renderJSONModal`.
- New tests per spec §5: `TestRenderJSONModal_*` (3), `TestConfirm*` (7).
  Update existing `TestConfirmCtrlCCancelsTurn` and any other affected
  tests to the new struct shape.

### Stream B — Tool opt-ins

**Files touched (exclusive):** `internal/tools/fold/boltz2.go`,
`internal/tools/fold/chai1.go`, plus their `_test.go` siblings if a test is
added.

**Scope (spec §3.1):**

- `(*boltz2Tool).Validate(input json.RawMessage) error` — unmarshal into
  `boltz2Request`, call `preflightBoltz2`.
- `(*chai1Tool).Validate(input json.RawMessage) error` — same shape against
  the chai1 preflight.
- One small unit test per tool: invalid input → `Validate` returns a
  non-nil error with the expected substring; valid input → `Validate`
  returns nil.

### Coordination

- Stream A may call `tool.(tools.Validator)` and expect to find `Validate`
  on `*boltzTool` / `*chai1Tool` after Stream B merges. While both streams
  are in flight, Stream A's edit-flow tests use a stub tool that
  implements `Validator` directly — no cross-stream dependency at test
  time.
- Both streams base off the same Phase 1 commit on `feat/editable-review`.
- After both finish, fast-forward or merge into `feat/editable-review`;
  resolve any conflict (only realistic candidate: `boltz2_test.go` /
  `chai1_test.go` if Stream A also touched them, which it should not).

## Phase 3 — Verification and merge

- `go build ./...`
- `go test ./...`
- `gofmt -l .` (must be empty)
- Manual sanity in the TUI on `fold.boltz2`: confirm modal opens, `[e]`
  hands off to editor on a temp workspace, save returns to modal with
  `(edited)` hint, `[y]` submits edited input. Documented in the merge
  commit; not blocking if the GB10 isn't reachable.
- Merge `feat/editable-review` → `dev` (per umbrella spec convention; user
  to confirm before push).

## Risks (mirrored from spec §7)

- `confirm` signature ripple: contained by doing all Phase 1 edits in one
  commit so the tree compiles end-to-end.
- Editor invocation under `tea.ExecProcess`: pattern is proven via
  `openEditorFileCmd`; the only new wrinkle is the overlay staying open
  behind the hand-off, which `editorFileDoneMsg` already supports.
- Pending-file leaks: `.fova/.gitignore` catches stray files; cleanup runs
  on every modal exit.
- `drainTurn` and flaky tests: the channel is still buffered-1 with
  exactly-one-reply-per-modal semantics; the reply shape changes but the
  rendezvous does not.
