# v0.4 Design & Fold Tool Wrappers — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the v0.4 antibody and enzyme compute tools — `design.rfantibody`, `design.chai2`, `design.rfdiffusion2`, `design.ligandmpnn`, `fold.boltz2`, `fold.chai1` — plus the `design-antibody` / `design-enzyme` skills, wired into the registry.

**Architecture:** The `design.*` tools reuse the existing generic `designTool` in `internal/tools/design/design.go` — each is only a ~20-line constructor. The `fold.*` tools (`boltz2`/`chai1`) are Modal-backed, so they need a small job-based fold tool mirroring `designTool` (the existing `fold.esmfold` is a synchronous HTTP tool and is *not* the pattern to copy for these). Skills are markdown. `cmd/proteus/main.go` registers everything.

**Tech Stack:** Go. Tests are `go test`, offline and deterministic, using stub backends — consistent with `internal/tools/design/design_test.go`.

**Scope note:** `internal/domain/types.go` already defines `AppAntibody`, `AppEnzyme`, `OriginRFAntibody`, `OriginChai2`, `OriginRFDiff2MPNN`; `internal/backends/local/tools.toml` already has the install recipes. Neither needs changes.

---

## Execution model

- **Phase A — Components (3 parallel agents).** Tasks A, B, C below. Each owns a disjoint set of files; no two touch the same file. `cmd/proteus/main.go` and `main_test.go` are off-limits in Phase A.
- **Phase B — Integration (sequential).** Task D: register the six tools in `cmd/proteus/main.go`, run the full suite.

### Hard rules for Phase A agents

Three agents edit the repo concurrently. Therefore:

1. **Only touch your task's files** (listed per task). Do not edit `cmd/proteus/`, `domain/types.go`, `tools.toml`, or another task's files.
2. **Never leave a package non-compiling.** Most of your files are new; when you add a test, make sure the package still builds (write a compiling stub before a failing test if needed). A failing assertion is fine; a compile error blocks the other agents.
3. **Offline, deterministic tests only** — use a stub backend like `stubBackend` in `internal/tools/design/design_test.go`. Never hit the network.
4. **Follow existing patterns exactly.** Match the surrounding code's style, doc comments, and error idioms.
5. **Do NOT run git.** Leave your files in the working tree; the orchestrator commits. Run `gofmt -w` on your files.

---

## File Structure

| File | Task | Responsibility |
|---|---|---|
| `internal/tools/design/rfantibody.go` (new) | A | `design.rfantibody` constructor |
| `internal/tools/design/chai2.go` (new) | A | `design.chai2` constructor |
| `internal/tools/design/rfdiffusion2.go` (new) | A | `design.rfdiffusion2` constructor |
| `internal/tools/design/ligandmpnn.go` (new) | A | `design.ligandmpnn` constructor |
| `internal/tools/design/design_test.go` (modify) | A | tests for the four new tools |
| `internal/tools/fold/foldjob.go` (new) | B | shared job-based fold tool |
| `internal/tools/fold/boltz2.go` (new) | B | `fold.boltz2` constructor |
| `internal/tools/fold/chai1.go` (new) | B | `fold.chai1` constructor |
| `internal/tools/fold/foldjob_test.go` (new) | B | tests for the fold tools |
| `internal/skills/builtin/design-antibody.md` (new) | C | antibody design skill |
| `internal/skills/builtin/design-enzyme.md` (new) | C | enzyme design skill |
| `cmd/proteus/main.go` (modify) | D | register the six tools |
| `cmd/proteus/main_test.go` (modify if needed) | D | keep the registry test passing |

---

## Task A: design.* tool constructors

**Owns (only these):** `internal/tools/design/rfantibody.go`, `chai2.go`, `rfdiffusion2.go`, `ligandmpnn.go` (new), `internal/tools/design/design_test.go` (modify).

**Read first:** `internal/tools/design/design.go` (the generic `designTool` — already does Execute/persist/schema/confirmation), `internal/tools/design/bindcraft.go` and `rfdiffusion.go` (the constructor pattern), `internal/tools/design/design_test.go`, and `internal/domain/types.go` lines 25-33 and 124-132 (the `Application` / `DesignOrigin` constants — all already exist).

Each tool is a constructor file exactly like `bindcraft.go`. Use the existing constants:

| File | Constructor | name | origin | application |
|---|---|---|---|---|
| `rfantibody.go` | `NewRFAntibodyTool` | `design.rfantibody` | `domain.OriginRFAntibody` | `domain.AppAntibody` |
| `chai2.go` | `NewChai2Tool` | `design.chai2` | `domain.OriginChai2` | `domain.AppAntibody` |
| `rfdiffusion2.go` | `NewRFdiffusion2Tool` | `design.rfdiffusion2` | `domain.OriginRFDiff2MPNN` | `domain.AppEnzyme` |
| `ligandmpnn.go` | `NewLigandMPNNTool` | `design.ligandmpnn` | `domain.OriginRFDiff2MPNN` | `domain.AppEnzyme` |

- [ ] Write the four constructor files. Each takes `(mgr *jobs.Manager, backend backends.Backend, st *store.Store)` and returns `*designTool`, mirroring `NewBindCraftTool`. Give each a one-line `description` matching its method (e.g. "Design de novo antibodies (VHH / scFv) against a target with RFantibody (runs as an async job).").
- [ ] In `design_test.go`: extend `TestDesignToolsImplementToolInterface` to also assert `var _ tools.Tool = NewRFAntibodyTool(...)` etc. for all four. Add one test `TestAntibodyEnzymeToolMetadata` that constructs each new tool and asserts its `Name()` and that a persisted design carries the right `Origin`/`Application` (reuse the `stubBackend` + `waitJob` helpers and the `{"designs":[...]}` output shape from the existing tests).
- [ ] Verify: `go build ./internal/tools/design/` and `go test ./internal/tools/design/ -v`. `gofmt -w` your five files.

## Task B: fold.boltz2 and fold.chai1

**Owns (only these):** `internal/tools/fold/foldjob.go`, `boltz2.go`, `chai1.go`, `foldjob_test.go` (all new).

**Read first:** `internal/tools/design/design.go` (the job-based pattern to mirror — submit a `jobs.Spec`, run `backend.Run`, return a `JobID`), `internal/tools/fold/esmfold.go` (the `tools.Tool` interface shape and metrics idiom — but note esmfold is synchronous HTTP; boltz2/chai1 are *not* like it), `internal/tools/design/design_test.go` (the `stubBackend` test pattern), SPECS §7.2.2.

`fold.boltz2` and `fold.chai1` are Modal-backed structure predictors. Build a shared job-based fold tool:

- [ ] `foldjob.go`: a `foldJobTool` struct with `name`, `description`, `mgr *jobs.Manager`, `backend backends.Backend`. Implement the `tools.Tool` interface: `InputSchema` accepts a `sequences` object (chain-id → amino-acid string) and an optional `save_as` path; `RequiresConfirmation` returns false; `EstimatedCostUSD`/`EstimatedDuration` return small non-zero values; `Execute` submits a `jobs.Spec{Kind: domain.JobCompute, Tool: name, Backend: backend.Name(), Input: input, Run: ...}` whose `Run` closure calls `backend.Run(ctx, name, input)` and returns its output (no design persistence — these are predictors, not generators), then returns `tools.Result{JobID, Display, Provenance}`.
- [ ] `boltz2.go`: `NewBoltz2(mgr, backend) *foldJobTool` → name `fold.boltz2`. `chai1.go`: `NewChai1(mgr, backend) *foldJobTool` → name `fold.chai1`.
- [ ] `foldjob_test.go`: with a stub backend, assert each tool implements `tools.Tool`, `Execute` returns a non-empty `JobID`, and the submitted job reaches `JobSucceeded`. Use the `stubBackend`/`waitJob`-style helpers (define local copies — do not import the `design` test package).
- [ ] Verify: `go build ./internal/tools/fold/` and `go test ./internal/tools/fold/ -v`. `gofmt -w` your four files.

## Task C: design-antibody and design-enzyme skills

**Owns (only these):** `internal/skills/builtin/design-antibody.md`, `internal/skills/builtin/design-enzyme.md` (new).

**Read first:** an existing built-in skill — `internal/skills/builtin/design-binder.md` — for the exact frontmatter / heading format, and `internal/skills/loader.go` (or whatever loads `builtin/`) to confirm the format. SPECS §8.2 contains the drafted content: the antibody skill is the SPECS block around "Primary method: RFantibody" / "Fallback: Chai-2" / "Wet-lab notes"; the enzyme skill is the block around "Primary method: RFdiffusion2 + LigandMPNN" / "Theozyme requirement".

- [ ] Write `design-antibody.md` following `design-binder.md`'s format, with the SPECS §8.2 antibody content (when to use, primary method `design.rfantibody`, fallback `design.chai2`, required inputs, standard parameters, wet-lab notes).
- [ ] Write `design-enzyme.md` likewise, with the SPECS §8.2 enzyme content (primary method `design.rfdiffusion2` + `design.ligandmpnn`, theozyme requirement, standard parameters, validation with `fold.chai1`).
- [ ] Verify: `go test ./internal/skills/ -v` still passes (the loader picks up the new files). `gofmt` does not apply to markdown.

---

## Task D: Integration (orchestrator, after A/B/C)

**Files:** `cmd/proteus/main.go`, `cmd/proteus/main_test.go` (if it asserts a tool count/set).

- [ ] In `buildRegistry`, after the existing `designtools.New*Tool` registrations add `NewRFAntibodyTool`, `NewChai2Tool`, `NewRFdiffusion2Tool`, `NewLigandMPNNTool` (all `(mgr, backend, st)`), and after `fold.NewESMFold` add `fold.NewBoltz2(mgr, backend)` and `fold.NewChai1(mgr, backend)`.
- [ ] Update `main_test.go` only if it asserts the exact tool set/count.
- [ ] Verify: `gofmt -l` empty, `go vet ./...` clean, `go build ./...` clean, `go test ./...` all pass.
- [ ] Commit.

---

## Self-Review checklist

- [ ] Every v0.4 design/fold tool in SPECS §7.2.3 / §7.2.2 maps to a task.
- [ ] No Phase A task touches `cmd/proteus/`, `domain/types.go`, or `tools.toml`.
- [ ] No two Phase A tasks share a file.
- [ ] Constructor names, tool names, and `domain` constants are consistent between Task A and Task D.
- [ ] `go test ./...` and `go vet ./...` clean after Task D.
