# BoltzGen Tool — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fully develop the BoltzGen design tool — a runnable container, an
agent that authors and validates real BoltzGen specification YAMLs, the full
run-config parameter surface, a plan-integrated review/edit/approve flow, and
ranked-and-scored design ingestion.

**Architecture:** BoltzGen becomes a bespoke `tools.Tool` (not the shared
`designTool`). The agent writes the spec YAML to a workspace file, validates
it with a new `design.boltzgen_check` tool (`boltzgen check` in the
container), and the typed run params become CLI flags. The spec + params fold
into the `DesignPlan` for `/plan` review and `/plan approve`. Results come
from BoltzGen's metrics CSVs.

**Tech Stack:** Go 1.26, the fova local container backend (podman/docker),
BoltzGen 0.3.2 (Python, in-container), NGC PyTorch base image.

**Design doc:** `docs/superpowers/specs/2026-05-21-boltzgen-tool-design.md`

---

## Shared contracts (all tasks depend on these — do not diverge)

### `BoltzGenParams` — the run-config parameter struct

Defined in `internal/tools/design/boltzgen.go`, consumed by the adapter and
the plan integration:

```go
// BoltzGenParams is the agent-facing BoltzGen run configuration. Every field
// maps to a `boltzgen run` CLI flag; fova owns the infra flags separately.
type BoltzGenParams struct {
	Protocol                string   `json:"protocol"`                   // enum, default "protein-anything"
	NumDesigns              int      `json:"num_designs"`                // --num_designs
	Budget                  int      `json:"budget"`                     // --budget
	DiffusionBatchSize      int      `json:"diffusion_batch_size,omitempty"`
	Steps                   []string `json:"steps,omitempty"`            // --steps
	Alpha                   *float64 `json:"alpha,omitempty"`            // --alpha
	FilterBiased            *bool    `json:"filter_biased,omitempty"`    // --filter_biased
	AdditionalFilters       []string `json:"additional_filters,omitempty"`
	RefoldingRMSDThreshold  *float64 `json:"refolding_rmsd_threshold,omitempty"`
	InverseFoldNumSequences int      `json:"inverse_fold_num_sequences,omitempty"`
	InverseFoldAvoid        string   `json:"inverse_fold_avoid,omitempty"`
	StepScale               *float64 `json:"step_scale,omitempty"`
	NoiseScale              *float64 `json:"noise_scale,omitempty"`
	Reuse                   bool     `json:"reuse,omitempty"`
}
```

Pointers (`*float64`, `*bool`) distinguish "unset" (omit the flag, use
BoltzGen's default) from a real zero value.

### Valid enum values

- `Protocol`: `protein-anything` (default), `peptide-anything`,
  `protein-small_molecule`, `antibody-anything`, `nanobody-anything`,
  `protein-redesign`.
- `Steps` entries: `design`, `inverse_folding`, `design_folding`, `folding`,
  `affinity`, `analysis`, `filtering`.

### `design.boltzgen` tool input

`boltzGenInput` = `BoltzGenParams` plus `SpecPath string json:"spec_path"`
(required; workspace-relative path to the spec YAML).

### `design.boltzgen_check` tool

Input: `{spec_path string}`. Output JSON:
`{valid bool, errors []string, visualization_path string}`.

### `DesignPlan` additions (`internal/domain/types.go`)

```go
// MethodConfig carries method-specific run configuration on a DesignPlan.
// Populated only for methods that need it (currently BoltzGen).
type MethodConfig struct {
	SpecPath  string          `json:"spec_path,omitempty"`  // workspace-relative spec YAML
	BoltzGen  *BoltzGenParams `json:"boltzgen,omitempty"`   // run params
}
```
Add field `MethodConfig *MethodConfig` to `DesignPlan`. `BoltzGenParams` is
referenced from `internal/domain` — to avoid an import cycle, define
`BoltzGenParams` in `internal/domain` and have `internal/tools/design`
alias/use it. (Decision: `BoltzGenParams` lives in `internal/domain`.)

---

## File structure

| File | Responsibility | Task |
|---|---|---|
| `internal/backends/local/containerfiles/boltzgen.Containerfile` | Fix NumPy-2 conflict | T1 |
| `internal/backends/local/tools.toml` | boltzgen smoke test → `boltzgen check` | T1 |
| `internal/skills/builtin/boltzgen-spec.md` | Spec-authoring skill (new) | T2 |
| `internal/domain/types.go` | `BoltzGenParams`, `MethodConfig`, `DesignPlan.MethodConfig` | T3 |
| `internal/tools/design/boltzgen.go` | Bespoke `design.boltzgen` tool (replaces wrapper) | T3 |
| `internal/backends/local/adapter_boltzgen.go` | Consume agent spec, params→CLI, results ingestion | T4, T6 |
| `internal/tools/design/boltzgen_check.go` | `design.boltzgen_check` tool (new) | T5 |
| `internal/backends/local/adapter_boltzgen_check.go` | `boltzgen check` runner (new) | T5 |
| `cmd/fova/main.go` | Register `design.boltzgen_check`; construct bespoke tool | T3, T5 |
| `internal/tools/plan/plan.go` | `plan.create` spec inputs + `boltzgen check` gate | T7 |
| `internal/tools/plan/render.go` | `/plan` BoltzGen section | T7 |
| `internal/tui/plan.go`, `internal/tui/app.go` | `/plan` render + `/plan approve` re-check | T7 |

---

## Task 1: Container fix (Phase 1)

**Files:**
- Modify: `internal/backends/local/containerfiles/boltzgen.Containerfile`
- Modify: `internal/backends/local/tools.toml` (the `[tools.boltzgen]` `smoke_test`)

**Context:** The NGC `pytorch:25.04` base bundles `tensorboard` compiled
against NumPy 1.x; BoltzGen pins `numpy==2.0.2`; PyTorch-Lightning's default
`TensorBoardLogger` imports `tensorboard` → `np.string_` crash at pipeline
step 1 (job log `j_759fab5d`).

- [ ] **Step 1: Apply the primary fix — disable the TensorBoard logger.**
  BoltzGen runs inference, not training. After `pip install boltzgen`, append
  a build step that verifies `boltzgen run --help` exposes the `--config`
  override, then rely on the adapter (Task 4) passing
  `--config <step> trainer.logger=false` for each step. If the BoltzGen
  config schema does not expose `trainer.logger`, fall to Step 2.
- [ ] **Step 2: Fallback — repair the env.** If Step 1 cannot disable
  TensorBoard, add `RUN PIP_CONSTRAINT= pip install --no-cache-dir -U tensorboard`
  after the boltzgen install so a NumPy-2-compatible `tensorboard` replaces
  the NGC one. Add a build-time check: `python -c "import tensorboard, numpy"`.
- [ ] **Step 3: Update the smoke test.** In `tools.toml`, change
  `[tools.boltzgen]` `smoke_test` from `boltzgen --help` to
  `boltzgen check /opt/_base/boltzgen_example.yaml` (a tiny vanilla-protein
  spec staged into the image — add a `COPY` for it, content from the design
  doc §2). Validates install + spec parsing with no GPU/weights.
- [ ] **Step 4: Fix the stale comment.** The Containerfile header still says
  weights mount from `~/.proteus/models/boltzgen/` — change to
  `~/fova/.fova/models/boltzgen/` (matches `ModelsRoot`).
- [ ] **Step 5: Commit.** `git commit -m "fix(boltzgen): container runs on
  the NGC base — resolve the NumPy 2.0 / tensorboard conflict"`.
- [ ] **Step 6: User validation.** This phase cannot be unit-tested. The user
  runs `/install boltzgen` on the GB10 and confirms the smoke test passes.
  If the build or smoke test fails, iterate on the fallback ladder
  (design doc §3).

**This task is fully parallel-safe** — it touches only the Containerfile and
the boltzgen recipe.

---

## Task 2: `boltzgen-spec` skill (Phase 2)

**Files:**
- Create: `internal/skills/builtin/boltzgen-spec.md`

**Context:** This skill is the agent's grounding for authoring BoltzGen spec
YAMLs. Skills in `internal/skills/builtin/` are auto-discovered by
`internal/skills/loader.go` — no loader change needed (see `design-binder.md`
as the existing pattern).

- [ ] **Step 1: Write the skill.** Sections, all content drawn from the
  design doc §2 and the BoltzGen `example/README.md`:
  - **When to use** — authoring or editing a `design.boltzgen` spec.
  - **The spec schema** — `entities` (protein/ligand/file), sequence notation
    (`80..140`, fixed AAs, `15..20AAAA18`, `C` for fixed Cys), `binding_types`
    (string and range forms), `structure_groups`, `design` regions,
    `secondary_structure`, `residue_constraints`, `cyclic`, ligands
    (CCD/SMILES), `constraints` (`bond`, `total_len`).
  - **Protocols** — the 6 protocols and when each applies (the design doc §2
    table).
  - **Critical caveats** — residue indices are 1-based `label_asym_id` (NOT
    author numbering); file paths in the spec are relative to the spec file.
  - **Workflow** — write the YAML with `fs.write` → validate with
    `design.boltzgen_check` → fix any errors → reference it from `plan.create`.
  - **Examples** — embed 3 upstream specs verbatim: the vanilla-protein
    binder, `vanilla_peptide_with_target_binding_site/beetletert.yaml`, and a
    binding-site example. (Copy them from the BoltzGen repo `example/` dir.)
- [ ] **Step 2: Verify discovery.** Run `go test ./internal/skills/...`;
  confirm the loader picks up the new skill (add a test asserting
  `boltzgen-spec` is in the loaded set if the loader test enumerates skills).
- [ ] **Step 3: Commit.** `git commit -m "feat(skills): boltzgen-spec skill
  for authoring BoltzGen design specifications"`.

**This task is fully parallel-safe** — one new file.

---

## Task 3: Bespoke `design.boltzgen` tool + domain types (Phase 2)

**Files:**
- Modify: `internal/domain/types.go` (add `BoltzGenParams`, `MethodConfig`,
  `DesignPlan.MethodConfig`)
- Rewrite: `internal/tools/design/boltzgen.go` (replace the `designTool`
  wrapper with a bespoke `boltzGenTool`)
- Modify: `cmd/fova/main.go` (construct the bespoke tool — the
  `registry.Register(designtools.NewBoltzGenTool(...))` line stays, the
  constructor signature is unchanged)
- Test: `internal/tools/design/boltzgen_test.go`

- [ ] **Step 1:** Add `BoltzGenParams` (shared-contracts section), `MethodConfig`,
  and `DesignPlan.MethodConfig *MethodConfig` to `internal/domain/types.go`.
- [ ] **Step 2: Write failing tests** in `boltzgen_test.go`: `NewBoltzGenTool`
  returns a tool named `design.boltzgen`; `InputSchema()` advertises
  `spec_path` (required) + every `BoltzGenParams` field; `RequiresConfirmation`
  is true; `Execute` with a missing `spec_path` errors clearly; `Execute` with
  a valid input submits a job and returns its `JobID`.
- [ ] **Step 3: Implement `boltzGenTool`.** A bespoke `tools.Tool`: `Name`,
  `Description` (mentions the spec must be authored first; point at the
  `boltzgen-spec` skill), `InputSchema` (rich — `spec_path` + the params, with
  `enum` for `protocol` and `steps`), `RequiresConfirmation` → true,
  `EstimatedCostUSD`/`EstimatedDuration`, and `Execute`: unmarshal
  `boltzGenInput`, resolve `spec_path` against the workspace, submit a
  `jobs.Spec{Tool: "design.boltzgen"}` whose `Run` calls
  `backend.Run(ctx, "design.boltzgen", resolvedInput, log, progress)` then
  persists designs (Task 6 fills the persistence). Keep the
  `NewBoltzGenTool(workspaceRoot, mgr, backend, st)` signature so
  `cmd/fova/main.go` is unchanged.
- [ ] **Step 4:** Run `go test ./internal/tools/design/... ./internal/domain/...`
  → PASS. Run `go build ./...` → clean.
- [ ] **Step 5: Commit.** `git commit -m "feat(boltzgen): bespoke
  design.boltzgen tool with the full run-config schema"`.

**Dependency:** Tasks 4, 6, 7 depend on `BoltzGenParams`/`boltzGenInput` from
this task. This is the Round-1 foundation.

---

## Task 4: Adapter rework — consume agent spec, params → CLI (Phase 2)

**Files:**
- Rewrite: `internal/backends/local/adapter_boltzgen.go`
- Test: `internal/backends/local/adapter_boltzgen_test.go`

**Context:** The adapter currently generates a hardcoded YAML
(`buildBoltzGenSpec`). It must instead consume the agent's spec file.

- [ ] **Step 1: Delete `buildBoltzGenSpec` + `normalizeBoltzGenHotspots`.**
  The adapter no longer generates YAML.
- [ ] **Step 2: Update `boltzGenRequest`** to match `boltzGenInput`
  (`spec_path` + the `BoltzGenParams` fields).
- [ ] **Step 3: Spec staging.** `Invoke` reads the spec YAML, parses its
  `entities[].file.path` references, and copies the spec + every referenced
  structure file into `env.WorkDir` preserving the relative layout BoltzGen
  expects (paths in the spec are relative to the spec). Keep the existing
  weights-cache `os.MkdirAll` from the `fix/validation-issues` work.
- [ ] **Step 4: Params → CLI flags.** A table-driven `boltzGenArgs(params)
  []string` mapping each set field of `BoltzGenParams` to its flag (design
  doc §2 table). Unset pointer/zero fields are omitted. fova fixes
  `--output /work/out`, devices, cache. If Task 1 chose the config route, also
  emit `--config <step> trainer.logger=false` for each step.
- [ ] **Step 5: Write tests** for `boltzGenArgs` (table test: each param →
  expected flag; unset → omitted) and spec staging (a spec referencing a
  `.cif` → both files land in WorkDir).
- [ ] **Step 6:** `go test ./internal/backends/local/...` → PASS.
- [ ] **Step 7: Commit.** `git commit -m "feat(boltzgen): adapter consumes
  the agent-authored spec and maps run params to CLI flags"`.

**Dependency:** Task 3 (`BoltzGenParams`). Same file as Task 6 — do Tasks 4
and 6 in one agent, sequentially.

---

## Task 5: `design.boltzgen_check` tool (Phase 2)

**Files:**
- Create: `internal/tools/design/boltzgen_check.go`
- Create: `internal/backends/local/adapter_boltzgen_check.go`
- Modify: `cmd/fova/main.go` (register the new tool)
- Test: `internal/tools/design/boltzgen_check_test.go`,
  `internal/backends/local/adapter_boltzgen_check_test.go`

- [ ] **Step 1: Write failing tests** — `design.boltzgen_check` tool named
  correctly, `InputSchema` advertises `spec_path`, `RequiresConfirmation`
  false (cheap, no GPU), `Execute` submits/runs and returns
  `{valid, errors, visualization_path}`. Adapter test: a stub container
  runtime; `boltzgen check` invocation builds the right argv.
- [ ] **Step 2: Implement the check adapter** (`adapter_boltzgen_check.go`):
  an adapter registered for `design.boltzgen_check`, `Recipe() "boltzgen"`,
  `Invoke` stages the spec + referenced files, runs
  `boltzgen check /work/in.yaml` in the container, parses stdout into
  `{valid, errors, visualization_path}` (the viz mmCIF BoltzGen writes).
- [ ] **Step 3: Implement the tool** (`boltzgen_check.go`): cheap tool,
  `RequiresConfirmation` false, `Execute` runs synchronously or as a fast job,
  returns the structured result.
- [ ] **Step 4: Register** in `cmd/fova/main.go` next to `design.boltzgen`.
- [ ] **Step 5:** `go test ./internal/tools/design/... ./internal/backends/local/...`
  → PASS; `go build ./...` clean.
- [ ] **Step 6: Commit.** `git commit -m "feat(boltzgen): design.boltzgen_check
  tool validates a spec via boltzgen check"`.

**Dependency:** Round 2 — needs Task 3 merged (shares `cmd/fova/main.go` and
the adapter registry conventions).

---

## Task 6: Results ingestion (Phase 4)

**Files:**
- Modify: `internal/backends/local/adapter_boltzgen.go` (same file as Task 4)
- Test: `internal/backends/local/adapter_boltzgen_test.go`

- [ ] **Step 1:** After the container run, the adapter reads
  `final_ranked_designs/final_<budget>_designs/*.cif` (structures) and
  parses `final_ranked_designs/final_designs_metrics_<budget>.csv`.
- [ ] **Step 2: Metrics parser.** A `parseBoltzGenMetrics(csvPath) (map[designID]map[string]float64, error)`
  — header row → column names; each row → one design's scores. Unknown
  columns are carried through as raw score keys (not dropped).
- [ ] **Step 3:** Emit the conventional `{"designs":[{sequence, structure_file,
  scores}, ...]}` JSON the design layer persists (`backendOutput` in
  `design.go`). One design per CSV row, structure_file = its CIF.
- [ ] **Step 4:** Copy `results_overview.pdf` to
  `<workspace>/designs/boltzgen-<run>/results_overview.pdf` and include its
  path in the result `Display`.
- [ ] **Step 5: Write tests** — `parseBoltzGenMetrics` against a fixture CSV
  (commit a small `testdata/boltzgen_metrics.csv`); the CIF+CSV → `designs[]`
  assembly.
- [ ] **Step 6:** `go test ./internal/backends/local/...` → PASS.
- [ ] **Step 7: Commit.** `git commit -m "feat(boltzgen): ingest ranked
  designs and metrics from the BoltzGen output"`.

**Dependency:** Task 4 (same file). One agent does Task 4 then Task 6.

---

## Task 7: Plan integration (Phase 3)

**Files:**
- Modify: `internal/tools/plan/plan.go` (`plan.create` accepts
  `method_spec_path` + `method_params`; runs `boltzgen check` for BoltzGen)
- Modify: `internal/tools/plan/render.go` (`/plan` BoltzGen section)
- Modify: `internal/tui/plan.go` (render the spec preview + check result)
- Modify: `internal/tui/app.go` (`/plan approve` re-runs `boltzgen check`
  before submitting for a BoltzGen plan)
- Test: `internal/tools/plan/plan_test.go`, `internal/tui/plan_test.go`

- [ ] **Step 1: `plan.create` inputs.** Add optional `method_spec_path`
  (string) and `method_params` (object → `BoltzGenParams`) to the
  `plan.create` schema. When `method` resolves to BoltzGen, `spec_path` is
  required; populate `DesignPlan.MethodConfig`.
- [ ] **Step 2: Check gate.** `plan.create` for BoltzGen runs
  `design.boltzgen_check` on the spec; if invalid, reject the plan with the
  check errors (consistent with the existing install + registration guards).
- [ ] **Step 3: `/plan` rendering.** `render.go` + `tui/plan.go`: when the
  plan has a `MethodConfig`, render protocol, num_designs, budget, the spec
  **absolute path**, a ~15-line spec preview, and the check result (✓ + viz
  path, or errors).
- [ ] **Step 4: `/plan approve` re-check.** In `app.go`, for a BoltzGen plan,
  `/plan approve` re-runs `boltzgen check` (catches user edits to the spec
  file). On failure: show errors, hold. On success: proceed (the agent turn
  `/plan approve` already starts submits `design.boltzgen` with the plan's
  spec + params).
- [ ] **Step 5: Write tests** — `plan.create` with a valid/invalid BoltzGen
  spec; `/plan` renders the BoltzGen section; `/plan approve` re-check
  rejects an edited-to-invalid spec.
- [ ] **Step 6:** `go test ./...` → PASS; `go build ./...` clean.
- [ ] **Step 7: Commit.** `git commit -m "feat(boltzgen): fold the spec +
  params into the design plan with a review/approve flow"`.

**Dependency:** Round 2 — needs Task 3 (`BoltzGenParams`, `MethodConfig`) and
Task 5 (`design.boltzgen_check`).

---

## Parallel execution rounds

The design has a tight core, so true N-way parallelism is not safe. Two rounds:

- **Round 1 (3 parallel agents):**
  - Agent A — Task 1 (container). Files: Containerfile, tools.toml.
  - Agent B — Task 2 (skill). File: `boltzgen-spec.md`.
  - Agent C — Tasks 3 + 4 + 6 (the core: domain types, bespoke tool, adapter
    rework, results ingestion). The param contract and the adapter are tightly
    coupled — one agent keeps them coherent.
- **Round 2 (2 parallel agents, after Round 1 merges):**
  - Agent D — Task 5 (`design.boltzgen_check`).
  - Agent E — Task 7 (plan integration).
- **Integration pass (lead):** merge, resolve `cmd/fova/main.go` overlap
  (Tasks 3 and 5 both register tools), `go build ./... && go test ./...`
  green, then user-validates the container + an end-to-end run on the GB10.

---

## Self-review

**Spec coverage:** design doc §3 (container) → T1; §4 (spec authoring: skill +
check tool) → T2 + T5; §5 (`design.boltzgen` rework) → T3 + T4; §6 (plan
integration) → T7; §7 (results ingestion) → T6; §8 (testing) → tests in every
task. All six components covered.

**Placeholder scan:** the container task (T1) intentionally carries a fallback
ladder — that is a real, justified branch (the fix needs GB10 iteration), not
a placeholder; each rung is concrete. Metrics column mapping (T6) carries
unknown columns through rather than guessing — concrete behavior, not a TODO.

**Type consistency:** `BoltzGenParams` is defined once (shared contracts,
`internal/domain`), referenced by T3/T4/T6/T7. `boltzGenInput` = `spec_path` +
`BoltzGenParams`. `design.boltzgen_check` output `{valid, errors,
visualization_path}` is consistent in T5 and T7. `NewBoltzGenTool` signature
is held stable in T3 so `cmd/fova/main.go` need not change for it.
