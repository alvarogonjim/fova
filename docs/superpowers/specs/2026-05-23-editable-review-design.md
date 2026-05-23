# Editable tool-call review — design

**Date:** 2026-05-23
**Branch:** `feat/editable-review` (off `dev` at `8f5dfc5`)
**Umbrella:** `docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md` §4
**Status:** Approved by the user (brainstorm pass); ready for implementation
planning via `superpowers:writing-plans`.

## Goal

Turn the agent loop's binary confirmation gate (`internal/agent/loop.go:186`)
into an editable accept / **edit** / cancel surface, so that every tool which
sets `RequiresConfirmation` true gets the same review semantics that
`/plan` + `DesignPlan.MethodConfig` already gives design tools. After this
spec lands, "the agent proposes, the user supervises" — fova's core pitch — is
real for predictors too, not just half-true.

The change is additive. Tools that don't want a richer review surface keep
working exactly as before; tools that want pre-execution validation on edited
input opt into a small optional interface.

---

## 1. Why — current-state gap

The umbrella spec §4 carved this out as the cross-cutting sub-project. The
short version:

- **Design tools** flow through `/plan` (`DesignPlan.MethodConfig`); the user
  edits the proposed config in the workspace, `/plan approve` re-runs
  preflight, the agent picks up the approved plan. Full accept / **edit** /
  cancel. The BoltzGen spec (`docs/superpowers/specs/2026-05-21-boltzgen-tool-design.md`)
  established this pattern.
- **Predictors** (`fold.boltz2`, `fold.chai1`) and other agent tools go
  through the **tool confirmation gate** (`internal/agent/loop.go:186`):

  ```go
  if tool.RequiresConfirmation(input) {
      l.bus <- ConfirmContextMsg{Tool: tc.Name, Input: input}
      if !l.confirm("Run " + tc.Name + "? " + string(input)) {
          ...decline...
      }
  }
  ```

  The TUI side (`internal/tui/app.go:380–389, 463–478`) renders a `y/n`
  `modalModel` and writes the result onto `m.confirmCh chan bool`. There is no
  edit path: the user either runs the exact spec the agent proposed, or
  declines and waits for the model to propose again.

The umbrella spec calls this out explicitly: until an editable confirmation
surface lands, predictors ship with "the enriched binary gate and 'edit' means
decline-with-correction". This spec is what makes that promise concrete.

---

## 2. Scope

### In scope

- A widened confirm contract on `internal/agent/loop.go` that carries
  edited input bytes back to the loop.
- A TUI-owned accept ↔ edit ↔ re-validate state machine that opens the user's
  `$EDITOR` on a workspace pending-input file (reusing
  `openEditorFileCmd` / `editorFileDoneMsg` from `internal/tui/editor.go`).
- A generic pretty-printed JSON modal renderer that serves every tool with
  `RequiresConfirmation` true; existing bespoke renderers (today only
  `lab.submit_experiment`'s `submitModal`) keep their dispatch unchanged.
- An optional `tools.Validator` sidecar interface for tools that want their
  edited input revalidated before `Execute` runs. Two opt-ins in this spec:
  `fold.boltz2` and `fold.chai1`. `lab.submit_experiment` may opt in if the
  change is small; otherwise unchanged.
- A workspace-relative pending-input file convention with cleanup and
  `.gitignore` hygiene.
- Tests covering: accept-as-proposed, edit-then-accept, edit-then-validate-
  fail-then-fix, edit-then-decline, no-`Validator` fallback, bespoke-renderer
  dispatch unchanged, and a golden test for the generic JSON renderer.

### Out of scope

- **Changes to `tools.Tool`.** Adding a method ripples through 40+ tool
  implementations across `internal/tools/{design,fold,score,jobs,knowledge,lab,viz,plan}`
  and is a large merge surface against the concurrent `feat/rfdiffusion2`
  session. The umbrella spec's watch-out is followed: the editable surface
  builds on `InputSchema()` + the optional sidecar interface.
- **Schema-aware form editors.** No field-by-field form UI inside the modal.
  The editor surface is raw JSON in `$EDITOR`; the modal renders a pretty-
  printed read-only preview. Field validation lives in the tool's optional
  `Validator.Validate`, not in a generic form-builder.
- **Migration of `lab.submit_experiment` away from `submitModal`.** The
  bespoke overlay stays; the generic renderer is the default fallback for
  every other tool.
- **Per-tool grounding-skill changes.** Out of scope here; tracked under each
  tool's own spec.
- **Replay mode UX.** Replay is read-only and never enters `confirmFn`; this
  spec is invisible to it.

---

## 3. Architecture

### 3.1 Tool-side: optional `Validator` sidecar

Add to `internal/tools/tools.go`:

```go
// Validator is implemented by tools that want their input revalidated after
// a user edit on the editable confirmation gate, before Execute runs.
// Tools that don't implement it accept any JSON the user produces;
// Execute is still the last line of defense.
type Validator interface {
    Validate(input json.RawMessage) error
}
```

This is an opt-in sidecar — no change to the `Tool` interface, no ripple
through the 40+ existing tool implementations.

Two opt-ins land with this spec:

- `fold.boltz2`: a 3-line `Validate` that unmarshals into `boltz2Request`
  and calls the existing `preflightBoltz2`. Wins the rich-error retry loop on
  edit.
- `fold.chai1`: same pattern against its existing preflight.

`lab.submit_experiment` may opt in if the change is small (it already
validates submit requests inside `Execute`); otherwise it stays as-is and
accepts user edits raw.

The other 35+ tools change nothing.

### 3.2 Agent-loop contract change

Single localised edit to `internal/agent/loop.go`. The `confirm` callback
widens by one argument and one return value:

```go
// before
confirm func(prompt string) bool

// after
confirm func(prompt, name string, input json.RawMessage) (accepted bool, finalInput json.RawMessage)
```

`finalInput` is the bytes the loop should submit. An empty `finalInput`
(`len(finalInput) == 0`) means "user accepted without editing"; a non-empty
value means "user edited, submit these bytes instead".

`NewLoop` and `NewLoopWithGuard` signatures change correspondingly. The only
producer of `confirm` in the project is the TUI's `confirmFn` (`app.go:787`)
plus test helpers.

The call site (`loop.go:186–192`) becomes:

```go
if tool.RequiresConfirmation(input) {
    l.bus <- ConfirmContextMsg{Tool: tc.Name, Input: input}
    accepted, edited := l.confirm("Run "+tc.Name+"?", tc.Name, input)
    if !accepted {
        l.bus <- ToolDoneMsg{Name: tc.Name, Display: "declined by user"}
        return "error: user declined to run " + tc.Name
    }
    if len(edited) > 0 {
        input = edited
    }
}
```

`guard.Inspect(tc.Name, input)` and `registry.Execute(ctx, tc.Name, input)`
already read the local `input` variable, so they pick up the edited bytes for
free — no further loop changes.

The bus message shapes (`ConfirmContextMsg`, `ConfirmRequestMsg`) are
unchanged. The prompt string in `ConfirmRequestMsg` is no longer the primary
review surface but stays for backward compat and tests.

### 3.3 TUI state machine

The TUI owns the entire accept ↔ edit ↔ re-validate dance. The agent loop
sees only the final result.

`confirmCh` widens from `chan bool` to a small reply struct:

```go
type confirmReply struct {
    accepted bool
    input    json.RawMessage // empty = unchanged
}

// Model.confirmCh becomes chan confirmReply.
```

`confirmFn` (`app.go:787`) becomes:

```go
func (m *Model) confirmFn(prompt, name string, input json.RawMessage) (bool, json.RawMessage) {
    m.bus <- agent.ConfirmRequestMsg{Prompt: prompt}
    r := <-m.confirmCh
    return r.accepted, r.input
}
```

The `overlayConfirm` keymap (`app.go:463–478`) gains one case:

| Key             | Action                                                                          |
|-----------------|---------------------------------------------------------------------------------|
| `y` / `Y`       | `confirmCh <- {accepted: true, input: m.pendingEdited}` (nil ⇒ accept original) |
| `n` / `N`       | `confirmCh <- {accepted: false}`, clear pending state                           |
| `e` / `E`       | Open `$EDITOR` on the pending file (see §3.4); modal stays open, no reply yet   |
| `esc`           | `confirmCh <- {accepted: false}` (existing behaviour)                           |
| `ctrl+c`        | `confirmCh <- {accepted: false}` + cancel turn (existing behaviour)             |

New `Model` state, narrowly scoped to the confirmation overlay:

```go
pendingInputPath  string          // workspace path to the pending JSON file; "" = no edit in flight
pendingEdited     json.RawMessage // validated edited bytes; submitted on [y]; nil = accept original
pendingValidator  tools.Validator // nil for tools that don't opt in
```

On `[e]`: write the proposed JSON (pretty-printed, with a `//` header — see
§3.4) to `pendingInputPath`, fire `openEditorFileCmd(pendingInputPath, "")`,
return without sending on `confirmCh`. The existing `editorFileDoneMsg`
handler (`app.go:436–448`) gets a new branch:

```go
case editorFileDoneMsg:
    if m.overlay == overlayConfirm && m.pendingInputPath != "" {
        return m.handleConfirmEditDone(msg)
    }
    // ...existing asset-file branch unchanged...
```

`handleConfirmEditDone` reads the file, strips `// …` comment lines, attempts
`json.Unmarshal` to confirm well-formedness, then — if `pendingValidator` is
non-nil — calls `Validate(bytes)`. On success: `pendingEdited = bytes`,
re-render the modal with an "(edited)" hint, wait for the user's `[y]` /
`[n]`. On failure: rewrite the pending file with the same body but with
`// ERROR: <message>` prepended to the comment block, reopen the editor.
One retry layer per edit cycle — the user can still `[n]` or `esc` to bail
out of the modal at any point.

### 3.4 Workspace file lifecycle

The pending-input file lives in the project workspace so the user's editor
opens a real path (sibling files navigable, LSP / JSON schema highlighting
active, no `/tmp` indirection).

- **Path:** `<workspace>/.fova/pending/<tool>-<short-uuid>.json`, where
  `<workspace>` is `workspaceFromHome(m.fovaHome)` (the existing helper, see
  `app.go:1431`) and `<short-uuid>` is an 8-hex-char prefix of a UUIDv4.
  The uuid prefix prevents two concurrent confirm overlays from clobbering
  each other (the TUI is single-modal today, but the path stays
  collision-safe under future replay / async confirm work).

- **Seed content:**

  ```
  // fova: edit the JSON below, save and quit. Comments (lines starting
  //       with //) are stripped before validation and submission.
  // Tool: fold.boltz2
  {
    "entities": [
      ...pretty-printed proposed input...
    ]
  }
  ```

  Pretty-printed with `json.Indent` (2-space). The header is rewritten on
  each retry; only the body bytes are preserved across retries.

- **Validation-failure retry:** on `Validator` error, the file is rewritten
  in place with the same body plus a `// ERROR: <message>` line at the top
  of the comment block, and the editor reopens. No new modal state — the
  user is still inside the same `overlayConfirm` cycle.

- **Cleanup:** the pending file is removed on accept (`[y]`), decline (`[n]`,
  `esc`, `ctrl+c`), and turn-cancel. Cleanup is the responsibility of the
  TUI handler that sends on `confirmCh`. A best-effort `os.Remove` is enough;
  failure to delete is logged but not surfaced (the workspace gitignore
  catches abandoned files).

- **gitignore hygiene:** the TUI ensures `<workspace>/.fova/.gitignore`
  exists with the line `pending/` when the first pending file is created.
  Idempotent: if the file already exists and contains the line, no-op; if it
  exists without the line, append; if it doesn't exist, create with that one
  line. The `.fova/` directory may already exist for other reasons; this
  spec only owns `.fova/pending/`.

### 3.5 Generic JSON modal renderer

New helper in `internal/tui/modal.go`:

```go
// renderJSONModal renders a tool-call confirmation as pretty-printed JSON
// inside a saffron-bordered ModalBox. The body is capped at maxLines rows;
// when truncated, the tail reads "… [e] to edit · scroll with PgUp/PgDn".
// edited is true when m.pendingEdited is non-nil — the header shows
// "(edited)" so the user knows what they're about to submit.
func renderJSONModal(name string, input json.RawMessage, edited bool, th Theme, width, maxLines int) string
```

The header is `"Run " + name + "?" + (edited ? " (edited)" : "")`. The action
row delegates to `RenderKeyRow` (`modal.go:39`) with four entries:

```
[y] accept  [e] edit  [n] decline  [esc] cancel turn
```

`lab.submit_experiment` keeps its bespoke `submitModal` path verbatim — the
existing `pendingTool` switch in `app.go:381–387` is the dispatch hook, with
one new generic-fallback branch:

```go
case agent.ConfirmRequestMsg:
    switch m.pendingTool {
    case "lab.submit_experiment":
        m.submit = buildSubmitModal(m.pendingInput, m.webhookURL)
        m.overlay = overlaySubmit
    default:
        m.modal = modalModel{
            prompt:   renderJSONModal(m.pendingTool, m.pendingInput, false, m.theme, m.width, 15),
            editable: true,
        }
        m.overlay = overlayConfirm
    }
```

`modalModel` gains an `editable bool` flag so the existing y/n-only modal
(used by future non-tool prompts, if any) keeps its three-key layout; an
editable modal swaps in the four-key layout above. Today every modal opened
through `ConfirmRequestMsg` is editable; the flag exists for forward
flexibility, not to support a current non-editable consumer.

---

## 4. Message and data flow

End-to-end happy path for "agent proposes → user edits → user accepts":

1. Agent loop reaches `if tool.RequiresConfirmation(input)`. Sends
   `ConfirmContextMsg{Tool, Input}` then calls
   `l.confirm("Run X?", "X", input)`.
2. `confirmFn` (TUI side) sends `ConfirmRequestMsg{Prompt}` on the bus, then
   blocks on `<-m.confirmCh`.
3. TUI `Update` receives `ConfirmContextMsg` (stashes `pendingTool` /
   `pendingInput`), then `ConfirmRequestMsg`. Dispatch picks the generic
   editable modal (or `submitModal` for `lab.submit_experiment`); modal
   renders pretty-printed JSON; user sees `[y] [e] [n] [esc]`.
4. User presses `[e]`. TUI writes
   `<workspace>/.fova/pending/X-a1b2c3d4.json`, fires
   `openEditorFileCmd(path, "")` via the existing helper, modal stays
   visible. `confirmCh` has not been written.
5. `$EDITOR` runs (TTY handed off via `tea.ExecProcess`). User edits, saves,
   quits.
6. `editorFileDoneMsg` arrives. New `overlayConfirm` branch reads the file,
   strips `//` comments, calls `json.Unmarshal` then
   `pendingValidator.Validate` if present. On success: `pendingEdited =
   bytes`; modal re-renders with `"(edited)"` hint and the new JSON;
   `confirmCh` still not written.
7. User presses `[y]`. TUI removes the pending file, writes
   `confirmReply{accepted: true, input: pendingEdited}` on `confirmCh`,
   closes the overlay.
8. `confirmFn` returns `(true, edited)`; agent loop sets `input = edited`,
   continues to `guard.Inspect` and `registry.Execute` with the new bytes.

Validation-failure variant (between steps 6 and 7): on `Validate` error,
TUI rewrites the file in place with `// ERROR: <message>` prepended, reopens
the editor; the second `editorFileDoneMsg` re-runs the validation. The loop
in §3.3 retries indefinitely per edit-and-save cycle — the user remains in
control via `[n]` / `esc`.

Decline variant: on `[n]` / `esc` / `ctrl+c` at any point after the modal
opens (including after one or more edit cycles), TUI removes the pending
file if present, writes `confirmReply{accepted: false}` on `confirmCh`,
closes the overlay. The agent loop returns `"error: user declined to run X"`
to the model.

Turn-cancel variant: `ctrl+c` additionally invokes `m.turnCancel()`,
matching the existing keymap behaviour.

---

## 5. Test plan

The test-hygiene pattern from the umbrella spec (`drainTurn(t, m)`) is
preserved. The only mechanical change is that test code that today writes
`m.confirmCh <- true` writes `m.confirmCh <- confirmReply{accepted: true}`
instead — same call sites, same drain semantics.

New tests:

**`internal/tui/modal_test.go`:**

1. `TestRenderJSONModal_short`: small input renders untruncated; key row
   shows `[y] [e] [n] [esc]`; no `(edited)` hint.
2. `TestRenderJSONModal_truncated`: input over `maxLines` rows shows the
   `"… [e] to edit · scroll with PgUp/PgDn"` tail.
3. `TestRenderJSONModal_edited`: `edited=true` adds the `(edited)` hint to
   the header.

**`internal/tui/app_test.go`:**

4. `TestConfirmAcceptUnchanged`: confirm modal opens, `y` submits, registry
   sees original bytes.
5. `TestConfirmEditAccept`: `e` opens fake editor that writes valid JSON,
   modal re-renders with `(edited)`, `y` submits edited bytes; pending file
   removed.
6. `TestConfirmEditValidateFailRetry`: first edit writes invalid JSON,
   `Validator` returns an error, pending file is rewritten with `// ERROR:`
   header, second edit writes valid JSON, modal re-renders, user accepts.
7. `TestConfirmEditDecline`: user edits then declines; pending file removed;
   registry never called.
8. `TestConfirmNoValidatorFallback`: tool without `Validator` accepts any
   well-formed JSON (used with a minimal stub tool).
9. `TestConfirmBespokeDispatchUnchanged`: `lab.submit_experiment` opens the
   existing `submitModal`, not the generic JSON renderer; `y` still works.
10. `TestConfirmCtrlCCancelsTurn`: `ctrl+c` in the modal both declines and
    invokes `turnCancel` (existing behaviour preserved).

**Hooks for tests:** the pending-file directory is resolved via a
`pendingInputDir func() string` hook on `Model` so tests point at
`t.TempDir()`; the editor invocation is swappable via an
`openEditorFile func(path, initial string) tea.Cmd` field, defaulting to
`openEditorFileCmd`, replaced in tests by a fake that writes a fixture and
immediately posts `editorFileDoneMsg`.

**Agent-side tests** (`internal/agent/loop_test.go`): existing confirm-flow
tests get their fake `confirm` updated to the new signature; one new test
covers the "edited bytes are submitted to registry" path with a stub
registry and a stub `confirm` that returns `(true, edited)`.

---

## 6. Migration and rollout

This is an additive change. Order of edits:

1. **`internal/tools/tools.go`** — add the `Validator` interface. No tool
   change required yet; nothing references it.
2. **`internal/agent/loop.go`** — widen `confirm` signature, update the call
   site, update `NewLoop` / `NewLoopWithGuard`. Compilation will fail at
   `internal/tui/app.go:780–790` (the only non-test caller) and in any test
   that constructs a `Loop` directly — fix together.
3. **`internal/tui/app.go`** — update `confirmFn`, widen `confirmCh`, add
   the `[e]` key branch, the `editorFileDoneMsg` extension, the pending-
   file helpers, the generic-renderer dispatch.
4. **`internal/tui/modal.go`** — add `renderJSONModal` and `modalModel`'s
   `editable` flag.
5. **`internal/tui/app_test.go`** — bulk-update existing
   `confirmCh <- true` / `false` writes to the new struct shape; add the new
   tests.
6. **`internal/tools/fold/boltz2.go`** + **`chai1.go`** — implement
   `Validate` against the existing preflight.
7. **Verify**: `go build ./...`, `go test ./...`, `gofmt -l` clean. Manual
   sanity in the TUI on at least one tool (boltz2) to confirm the editor
   handoff feels right.
8. **Merge to `dev`** — per the umbrella spec's convention.

Backward compatibility: tools without `Validator` see no behaviour change.
Tools that don't set `RequiresConfirmation` true never enter the gate.
`lab.submit_experiment` keeps its bespoke surface. Replay mode is unaffected.

---

## 7. Risks

- **`confirm` signature ripple.** The widened signature touches every caller
  of `NewLoop` / `NewLoopWithGuard`. There is only one production caller
  (`app.go:780`) and a small number of test helpers; the change is mechanical
  but must catch all of them or the build breaks. Mitigation: do steps 2–5
  of §6 in a single commit so the tree compiles end-to-end.

- **Editor invocation under `tea.ExecProcess`.** Bubble Tea's TTY hand-off
  is already used by `openEditorFileCmd` for asset-file edits and by
  `openEditorCmd` for the chat composer — the pattern is proven. The new
  twist is that the confirmation modal remains visible *behind* the editor
  hand-off; on return, the overlay re-renders with the edited content. This
  is exactly what `editorFileDoneMsg` already supports; the new branch just
  steers it.

- **Pending-file leaks.** A crash between writing the pending file and
  cleanup leaves it on disk. Mitigation: `.fova/.gitignore` with `pending/`
  ensures no accidental commit; the file is small JSON and the user's own
  workspace; a future `/doctor` check could surface stale entries but is
  not in scope here.

- **`Validator` opt-in inconsistency.** Tools that don't implement
  `Validator` skip pre-execution validation; their edits surface failures
  only when `Execute` runs (matching today's behaviour). Acceptable for a
  first cut — the opt-in is explicit and discoverable, and the umbrella
  spec's per-tool integration cycle is the place to add `Validator` where
  it pays off.

- **`drainTurn` and the broader test surface.** Three previously-fixed flaky
  tests in `dev`'s history relate to the confirm gate timing
  (`dcb2dfa`'s neighbour commits). The widened reply struct is
  source-compatible from the drain's perspective: the channel is still
  buffered-1, the test still sends exactly one reply per modal. Tests get
  re-run end-to-end as part of the verification gate before merge.

---

## 8. Deliverables

- This spec (`docs/superpowers/specs/2026-05-23-editable-review-design.md`).
- An implementation plan under
  `docs/superpowers/plans/2026-05-23-editable-review.md` produced via
  `superpowers:writing-plans`.
- The agent-loop contract change, the TUI state machine, the generic JSON
  renderer, the `Validator` sidecar, the two `Validator` opt-ins
  (`fold.boltz2`, `fold.chai1`), the workspace file lifecycle plumbing,
  the gitignore hygiene, and the test suite enumerated in §5.
- A merge to `dev` per the umbrella convention.
