# Kickoff — Editable tool-call review

You are a fresh Claude session driving **the editable tool-call review surface** for fova — the umbrella's 7th cross-cutting sub-project. Read this brief first.

## Why

fova's pitch is *"the agent proposes, the user supervises (accept / **edit** / cancel)"* — that loop is called *the core of fova*. Today it only half-exists:

- **Design tools** integrate through `/plan` (`DesignPlan.MethodConfig`), which supports editing the proposed config in the workspace + re-running preflight on approve. Good — full accept/edit/cancel.
- **Predictors** (`fold.boltz2`, `fold.chai1`) and other agent tools go through the **tool confirmation gate** (`internal/agent/loop.go:186`): a **binary** prompt `"Run X? <input>"` — accept or decline. **No edit path.** The umbrella §4 explicitly carved this out as a cross-cutting sub-project.

Your job: design and ship the editable confirmation surface. Once it lands, every tool that sets `RequiresConfirmation` true gets accept / edit / cancel by default.

## Project context (orient yourself in this order)

1. `MEMORY.md` (auto-loaded) — the broader effort + branch conventions.
2. `docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md` §4 — the existing review-mechanism description, the design-tool vs predictor split, and the explicit "editable review = its own spec" note.
3. `internal/agent/loop.go` — the agent loop and the existing `RequiresConfirmation` / `confirm` flow (around line 186). `ConfirmRequestMsg` and `ConfirmContextMsg` are how the loop talks to the TUI.
4. `internal/tui/app.go` — the TUI side: how `ConfirmRequestMsg`/`ConfirmContextMsg` are handled, where the confirm prompt is rendered (search the file), and the existing `confirmCh chan bool` plumbing.
5. The bespoke predictor specs (`docs/superpowers/specs/2026-05-21-fold-boltz2-design.md`, `2026-05-21-fold-chai1-design.md`) — these explain why the binary gate is insufficient and what an editable surface should buy.

## Branch + worktree

- **Worktree:** `/home/gonjim/Projects/proteus-edreview`
- **Branch:** `feat/editable-review` (off `dev` at `8f5dfc5`)

## Your task — scope to negotiate with the user

This is the **most design-heavy** of the parallel tracks — UX-shaped. The brainstorm step is *mandatory* and substantial; do not skip to implementation. The genuine open questions:

- **Editor surface.** In-place inline edit in the TUI confirmation modal? A workspace JSON file the user opens in their own editor (like BoltzGen does for its spec YAML)? Both?
- **Schema awareness.** Should the editor be schema-aware (read `tool.InputSchema()` and offer field-by-field edit / validation) or just a raw JSON editor (parse on accept)?
- **Validation on accept.** Re-run the tool's `RequiresConfirmation`-side preflight (if any) on the edited input before submitting?
- **Cancel semantics.** Decline-with-correction vs hard cancel — already covered by `decline`; what does "cancel" add?
- **Tool interface impact.** Does this need a new method on `tools.Tool` (e.g. `Validate(input) error`) or does it fit on top of `InputSchema()`? Touching `tools.Tool` ripples through every tool — a careful trade-off.

## Cross-session friction

You are running concurrently with two other Claude sessions: `feat/knowledge-pdb-search` (zero overlap) and `feat/rfdiffusion2` (which touches `internal/tools/design/`, `/plan`, `internal/domain/types.go` — **no overlap with `internal/agent/`, `internal/tui/`, or `internal/tools/tools.go`** unless you change the `tools.Tool` interface).

**Watch-outs:**

- If you add a method to the `tools.Tool` interface, **every tool implementation** must add it (40+ tools across `internal/tools/{design,fold,score,jobs,knowledge,lab,viz,plan}`) — that's a huge merge surface with the other sessions. Strongly prefer keeping `tools.Tool` unchanged and building the editable surface around `InputSchema()` + an optional sidecar interface tools can satisfy if they want validation hooks. Discuss this fork explicitly with the user before committing.
- `internal/tui/app.go` is touched by `feat/rfdiffusion2`'s `/plan` work (small, localised to plan-rendering — different surface from yours). Stay out of the `MethodConfig` render dispatch.
- The test-hygiene pattern matters here: any test that triggers the confirm gate or `startTurn` must use `drainTurn(t, m)` (in `internal/tui/app_test.go`) before returning — there are three previously-diagnosed flaky-test fixes of that class in `dev`'s history.

## Pattern to follow

This track is **brainstorm-heavy** and the implementation surface is moderate (not 4-stream-worthy unless it grows large). Recommended cycle:

1. **Brainstorm** (`superpowers:brainstorming`) — thoroughly. Multiple clarifying questions (one at a time). Resolve the open questions above with the user. Propose 2-3 architectural approaches with trade-offs. Strong preference for an additive design that does NOT change the `tools.Tool` interface.
2. **Spec** → `docs/superpowers/specs/2026-05-23-editable-review-design.md`. Cover: agent-loop changes, TUI editor surface, message-flow, validation policy, fallback when a tool has no `RequiresConfirmation`. Get user approval explicitly.
3. **Plan** (`superpowers:writing-plans`) — likely a Foundation (any `tools.Tool`-adjacent changes) + small streams (agent-side, TUI-side, optionally a per-tool migration). Save to `docs/superpowers/plans/2026-05-23-editable-review.md`.
4. **Implement** — use parallel Opus agents if and only if the work cleanly decomposes file-disjointly; otherwise inline. The agent/tui split is a natural seam.
5. Verify build + full suite + gofmt; ensure all confirm-flow tests still pass.
6. **Merge to `dev`** — outward, confirm with user.

## Stop conditions — involve the user

- Each brainstorm clarifying question (one at a time).
- Spec approval — this one matters; UX choices are sticky.
- Plan approval.
- Any decision to touch the `tools.Tool` interface (high-impact).
- Dev merge.

## Honest assessment

This is the most open-ended of the three tracks. Resist the urge to dive into implementation before the design is settled. The brainstorming skill's hard gate applies: **no implementation before approved design**.
