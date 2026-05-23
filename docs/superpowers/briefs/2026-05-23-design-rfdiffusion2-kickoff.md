# Kickoff — `design.rfdiffusion2`

You are a fresh Claude session driving **the last umbrella tool — `design.rfdiffusion2`** for the fova project. Read this brief first.

## Why

`design.rfdiffusion2` closes the 6-tool umbrella. **It is also currently a dead-end**: registered in `cmd/fova/main.go` and accepted by `plan.create` as `MethodRFdiffusion2`, but with **no adapter** — `RunDesign` returns *"no local adapter on this backend yet"* at execution. Same defect class as `design.chai2` had before retirement (umbrella tier-3). Unlike chai2, rfdiffusion2 *is* installable (recipe + Containerfile + weights exist) — it just isn't wired.

## Project context (orient yourself in this order)

1. `MEMORY.md` (auto-loaded) — the tool-integration effort, branches, conventions.
2. `docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md` — the umbrella decomposition and per-tool template.
3. `docs/superpowers/specs/2026-05-22-design-rfantibody-design.md` + `docs/superpowers/plans/2026-05-22-design-rfantibody.md` — **your closest template**. RFantibody is also x86-only and uses a multi-stage pipeline driver — follow that pattern.
4. `internal/tools/design/rfantibody.go`, `internal/backends/local/adapter_rfantibody.go`, `internal/domain/rfantibody.go` — the rfantibody bespoke-tool, adapter, and domain code committed on `dev`. **Mirror this structure exactly.**
5. `internal/backends/local/containerfiles/rfdiffusion2.Containerfile` — the existing Containerfile (read the comments; it explicitly fails the build on aarch64 — PyRosetta has no py3.12 aarch64 wheel).
6. `internal/backends/local/tools.toml` `[tools.rfdiffusion2]` — the recipe + the pipeline entrypoint.

## Branch + worktree

- **Worktree:** `/home/gonjim/Projects/proteus-rfdiff2`
- **Branch:** `feat/rfdiffusion2` (off `dev` at `8f5dfc5`)

## Your task — scope

Full integration of `design.rfdiffusion2`:
- bespoke `rfdiffusion2Tool` replacing the shared `designTool` wrapper;
- typed schema covering RFdiffusion2's `pipeline.py` (Hydra-config) surface;
- new local adapter (mirrors the rfantibody 3-stage driver pattern);
- `/plan` method-config (`MethodConfig.RFdiffusion2 *RFdiffusion2Params` in `internal/domain/types.go`);
- preflight validation;
- score ingestion (RFdiffusion2 writes metrics CSVs — `IdealizedResidueRMSD`, `motif_ideality_diff`);
- grounding skill at `internal/assets/embed/skills/rfdiffusion2-design.md`.

### Important upstream facts

- RFdiffusion2's `pipeline.py` runs the **full pipeline**: backbone diffusion → idealization → **inline LigandMPNN sequence design** → **inline Chai-1 fold** → metrics. Decide with the user whether v1 runs the full pipeline (Hydra default) or a backbone-only stop-step.
- It is **enzyme active-site scaffolding** focused (atomic motif scaffolding) — the schema centres on the catalytic motif input + `inference.guidepost_xyz_as_design_bb` etc.
- **x86-only.** GPU end-to-end validation needs an x86 GPU box; the GB10 cannot run it. CI layers (schema, adapter logic, preflight, /plan wiring, score parsing) are all platform-independent.

## Cross-session friction

You are running concurrently with two other Claude sessions: `feat/knowledge-pdb-search` (zero overlap with you) and `feat/editable-review` (light overlap on `internal/tui/` only — no design-tool touch).

After **you** finish and merge `feat/rfdiffusion2` into `dev`, a **fourth track** ("schema-expansion trio" — `design.rfdiffusion` / `proteinmpnn` / `bindcraft`) will branch off the post-your-merge `dev` and add similar `MethodConfig.<Tool>` fields alongside `RFdiffusion2`. Keep your changes **additive** in `internal/domain/types.go` (`MethodConfig`), `internal/tools/plan/plan.go` (the method-config dispatch), and `internal/tools/plan/render.go` (the render dispatch) so the trio rebases cleanly.

The `internal/assets/assets_test.go` skill-count assertion: each session that adds a built-in skill bumps it by one. Yours adds `rfdiffusion2-design.md` → bump 12 → 13 in your integration commit. If a sibling session lands first, rebase: bump from whatever count `dev` shows to that + 1.

## Pattern to follow (matches rfantibody exactly)

1. **Brainstorm** (`superpowers:brainstorming`) — research the RFdiffusion2 upstream (`https://github.com/RosettaCommons/RFdiffusion2`); 2-3 scoping questions (pipeline scope: full vs backbone-only; feature scope; agent-input model for the catalytic motif).
2. **Spec** → `docs/superpowers/specs/2026-05-23-design-rfdiffusion2-design.md`. Mirror the rfantibody spec sections.
3. **Plan** (`superpowers:writing-plans`) — Foundation + 4 parallel streams (tool / adapter / `/plan` / skill). Save to `docs/superpowers/plans/2026-05-23-design-rfdiffusion2.md`.
4. **Foundation** (you, the coordinator): 1 commit for the domain — `RFdiffusion2Params` + `MethodConfig.RFdiffusion2` + `RFdiffusion2Params.Validate()` in `internal/domain/`. *No chai2-style retirement is needed — chai2 is already gone.*
5. **Dispatch 4 parallel Opus agents** — one each for tool / adapter / `/plan` / skill. Each in its own worktree off `feat/rfdiffusion2`. Use the rfantibody plan's agent prompts as the template.
6. **Integrate** — merge the four streams; bump the asset skill-count; build + full suite + gofmt.
7. **Merge to `dev`** when verified (offer to the user; the dev merge is outward-ish — confirm).

## Test hygiene (from prior debugging)

If any test in `internal/tui/` triggers `/plan approve` on an RFdiffusion2 plan, it will spawn an agent-loop goroutine via `startTurn`. **Use the existing `drainTurn(t, m)` helper in `app_test.go`** before the test returns, or the test will flake (3 such flakes have been diagnosed + fixed in this codebase; same pattern applies here).

## Stop conditions — involve the user

- Spec approval (after brainstorm).
- Pipeline-scope decision (full pipeline vs backbone-only).
- Plan approval.
- Dispatch confirmation (the Agent tool: only when explicitly authorised — the brief already authorises it for this work).
- Dev merge (outward).
