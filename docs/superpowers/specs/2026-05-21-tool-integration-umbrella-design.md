# Full tool integration — umbrella design

**Date:** 2026-05-21
**Branch:** `feat/tool-integration` (off `fix/validation-issues`)
**Status:** Approved — decomposition spec; each tool gets its own spec under
this umbrella.
**Related:** `docs/superpowers/specs/2026-05-21-boltzgen-tool-design.md`
(BoltzGen, developed in parallel on `feat/boltzgen-tool`).

## Goal

Six design / fold tools — `fold.boltz2`, `fold.chai1`, `design.rfantibody`,
`design.chai2`, `design.rfdiffusion2`, `design.ligandmpnn` — fully integrated
end-to-end on the local backend, so that:

1. the agent can propose a **complete, correct specification + parameters**
   for any of them from the target and the user's intent;
2. the user **supervises** — accepts, edits, or cancels — through the `/plan`
   review surface;
3. an approved plan **runs without input-side errors**, because everything
   checkable without a GPU has been verified before the job starts.

This is the same end-to-end bar the BoltzGen spec sets, applied to the rest of
fova's tool surface. It is the core promise of fova: the agent designs, the
human supervises, and approved work does not fail on preventable mistakes.

---

## 1. Why — current-state gap

A survey of the tool layer (`internal/tools/design`, `internal/tools/fold`,
`internal/backends/local`) found integration is a happy-path stub in three
tiers:

### Tier 1 — thin predictors

`fold.boltz2` and `fold.chai1` expose only `{sequences, save_as}`
(`internal/tools/fold/foldjob.go`). Unreachable today:

- **Boltz-2:** ligands (SMILES / CCD), affinity prediction, pocket / contact /
  bond constraints, MSA control, recycling / diffusion / sampling parameters,
  templates.
- **Chai-1:** ligands, restraints / contacts, MSA, templates, recycle /
  timestep / seed knobs.

Both adapters hardcode their CLI flags. Critically, **neither parses
confidence scores** — `parseBoltz2Output` and `parseChai1Output` leave
`Scores` empty (`chai1.go` comment: *"confidence JSON sidecar isn't surfaced
in v0.7"*). For a pipeline whose purpose is ranking designs, predicted
structures landing unranked is a real end-to-end hole.

### Tier 2 — one generic schema for six different tools

`internal/tools/design/design.go` defines a single `InputSchema()` —
`target / hotspots / num_designs / contigs / settings` — shared by every
`design.*` tool. ProteinMPNN, RFdiffusion, RFantibody, LigandMPNN, Chai-2 and
RFdiffusion2 have very different parameter surfaces; one schema fits none of
them well and advertises fields a given tool ignores.

### Tier 3 — four design tools with no adapter at all

`design.rfantibody`, `design.chai2`, `design.rfdiffusion2` and
`design.ligandmpnn` are registered in `cmd/fova/main.go`, advertised to the
agent, and accepted by `plan.create` / `compat.go` as valid methods — but
have **no `ToolAdapter`** on the local backend. `RunDesign`
(`internal/backends/local/adapter.go`) returns *"no local adapter on this
backend yet"*. An approved plan that picks any of these silently dead-ends at
execution. This is the same defect class as known-issue #6 (BoltzGen, since
fixed), multiplied by four.

Net: of fova's ten design / fold tools, only ESMFold plus the three with
adapters (`rfdiffusion`, `proteinmpnn`, `bindcraft`) actually run, and even
those are under-specified. BoltzGen is being fully developed in parallel.

---

## 2. Scope

### In scope

- Six tools: `fold.boltz2`, `fold.chai1`, `design.rfantibody`,
  `design.chai2`, `design.rfdiffusion2`, `design.ligandmpnn`.
- Local backend only (container adapters; podman / docker on the GB10).
- Bespoke per-tool integration — no shared code framework.
- Serial delivery — one tool fully integrated and merged before the next.

### Out of scope

- **BoltzGen** — developed in parallel on `feat/boltzgen-tool`.
- **Modal backend** — these tools target the local backend only. Modal parity
  is deferred to a later effort.
- **Model training.** fova consumes models, never retrains them.
- **An in-TUI spec editor** — the user edits spec files with their own editor,
  consistent with the BoltzGen decision.
- **`design.rfdiffusion`, `design.proteinmpnn`, `design.bindcraft` schema
  expansion.** These three have working adapters but still share the generic
  schema. They run today; they are under-specified. This is a known remaining
  gap, recorded here as explicit follow-up — not silently dropped — to be
  taken up after the six in-scope tools land.

---

## 3. Per-tool spec template

Each tool gets its own design document, modelled on the BoltzGen spec, with
these sections. Uniform structure keeps six bespoke integrations consistent
without a shared code abstraction.

1. **Upstream reference.** The tool's real CLI / API surface from its actual
   repository: commands, input format, every parameter, output layout, weight
   sources. This is the "revise the corresponding repository" step — the spec
   is grounded in what the tool actually accepts, not a guess.
2. **Typed input schema.** A bespoke `InputSchema()` replacing the generic
   `target / hotspots / num_designs`. Enums for finite choices; bounded ints /
   floats; required vs optional made explicit; descriptions the agent can act
   on.
3. **Adapter.** Maps the typed request to the tool's real invocation
   (CLI flags / YAML / FASTA / restraint files). For the four design tools
   this is a **brand-new `ToolAdapter`**; for the two predictors it is a
   rework of the existing thin adapter. fova owns infra flags (output dir,
   weights cache, devices, workers); the agent never sees them.
4. **Preflight.** A cheap, no-GPU validation pass — see §5.
5. **Review integration.** The surface depends on tool kind (see §4): design
   tools get a bespoke method-config on `DesignPlan` plus a `/plan` render
   section; predictors (`fold.boltz2`, `fold.chai1`) go through the tool
   confirmation gate. Either way the agent proposes the spec / parameters and
   the user accepts, edits, or cancels.
6. **Score ingestion.** Parse the tool's native confidence / metrics output
   into `Design.Scores`. See §6.
7. **Grounding skill.** A `internal/skills/builtin/<tool>-*.md` skill that
   teaches the agent how to author a correct spec / parameter set for the
   tool — the per-tool analogue of `boltzgen-spec.md`. This is what makes
   "the agent proposes" reliable.
8. **Testing & risks.** Unit tests (schema, param→invocation mapping, output
   parsing against fixtures, `/plan` rendering); an install-time container
   smoke test; and the tool-specific risks surfaced during upstream research.

Each tool's lifecycle: own branch off the latest integrated state → write the
spec → `writing-plans` → implement → non-GPU verification green → merge →
next tool.

---

## 4. Review mechanism — bespoke per tool, split by tool kind

The user chose bespoke integration over a shared framework. Each tool
implements the "agent proposes, user supervises" loop itself, following the
BoltzGen pattern but with its own types. The *surface* depends on the tool
kind.

### Design tools — `rfantibody`, `chai2`, `rfdiffusion2`, `ligandmpnn`

A design tool is chosen as a `DesignPlan` method, so it supervises through the
existing `/plan` flow:

- The tool's method-config is its **own Go type**, hung off `DesignPlan` as an
  optional field. BoltzGen's config and each of the four are independent — no
  shared union, no collision.
- `plan.create` accepts that tool's parameters / spec path, runs the tool's
  **preflight** (§5), and rejects the plan if preflight fails — consistent
  with the existing install-status and tool-registration guards.
- `/plan` renders the tool's section; `/plan approve` re-runs preflight to
  catch edits, then hands control to the agent turn it already starts (per the
  known-issue #1 fix).

### Predictors — `fold.boltz2`, `fold.chai1`

A predictor is called by the agent repeatedly mid-pipeline; it is not a
`DesignPlan` method and does not belong in the project-level `/plan` (one
design plan per project). It supervises through the **tool confirmation gate**
(`internal/agent/loop.go`): the tool sets `RequiresConfirmation` true, the gate
renders the full proposed spec, and preflight runs at the top of `Execute` so
no doomed job is ever submitted.

### Editable review — a cross-cutting sub-project

The confirmation gate is binary today (accept / decline); true inline editing
lives only in the `/plan` flow. A faithful "accept / **edit** / cancel" review
for predictor specs needs an editable confirmation surface. That surface is
cross-cutting — it would serve all six tools and BoltzGen — so it is **its own
spec** under this umbrella rather than being buried in one tool's bespoke
work. Until it lands, predictors ship with the enriched binary gate and "edit"
means decline-with-correction.

### Shared-file footprint

Because integration is bespoke, the only **physically shared** edits per tool
are tiny and conflict-trivial: one registration line in `cmd/fova/main.go`;
for design tools, one dispatch case in the `/plan` renderer
(`internal/tools/plan/render.go`, `internal/tui/plan.go`); `compat.go` aliases
(already present for all four design methods). Serial delivery touches these
one tool at a time — no multi-way merge reconciliation.

---

## 5. The preflight — the "no errors" guarantee

Each tool defines a **preflight**: a cheap, no-GPU validation of everything
that can be known before a job starts. Representative checks:

- declared input files exist, are readable, and parse (PDB / CIF / FASTA);
- sequences use a valid amino-acid alphabet; lengths within tool limits;
- ligand SMILES / CCD codes parse, where the tool accepts ligands;
- the container image is built and present;
- the weights-cache directory is resolvable (created on demand for
  runtime-download tools, per the known-issue #11 fix);
- every parameter is within its enum / numeric bounds;
- spec-file cross-references resolve (chain IDs, residue indices).

Preflight runs at `plan.create` and again at `/plan approve` (catching edits),
and the agent may call it while iterating on a proposal. It does **not**
guarantee the GPU run succeeds — genuine runtime failures (OOM, model
divergence, upstream bugs) remain possible. It guarantees that no job is ever
launched with a preventable **input-side** error. That is the achievable form
of "verified everything": every error fova can detect without a GPU is caught
before the user's compute is spent.

---

## 6. Score ingestion

Each tool parses its native confidence / metrics output into `Design.Scores`,
closing the "predictions and designs land unranked" hole.

- **Standard keys** where the tool emits the corresponding metric: `plddt`,
  `ptm`, `iptm`, `pae`, `affinity`. A shared key vocabulary lets the scoring
  and ranking tools (`score.filter`, `score.metrics`, `score.ipsae`) and the
  designs panel treat outputs from different tools uniformly.
- **Unknown columns are carried through** as raw scores rather than dropped,
  so a tool-specific metric is never lost.
- The exact native-output → key mapping is settled in each tool's spec
  against a real output sample, not guessed.

`fold.boltz2` establishes this pattern first (it is delivered first); the
remaining tools follow it.

---

## 7. Delivery order

Strictly serial. Predictors first — they share the score-ingestion problem and
`fold.boltz2` is the closest analogue to BoltzGen, so the pattern is
established once and carried forward.

1. **`fold.boltz2`** — YAML-spec predictor; ligands, affinity, constraints,
   MSA control, model parameters; establishes score ingestion.
2. **`fold.chai1`** — FASTA + restraints predictor; ligands, restraints,
   model parameters.
3. **`design.rfdiffusion2`** — contig-based backbone design; brand-new
   adapter.
4. **`design.rfantibody`** — antibody design (framework + target + CDR
   hotspots); brand-new adapter.
5. **`design.ligandmpnn`** — sequence design from structure with ligand
   context; brand-new adapter; fastest of the six (no diffusion sampling).
6. **`design.chai2`** — design tool. **Phase 1 of its spec confirms that
   Chai-2 has a runnable open-weights release.** If it does not, that spec
   documents the blocker and recommends de-registering `design.chai2` /
   `MethodChai2` rather than wiring a phantom tool.

---

## 8. Branch & coordination

- This umbrella and all six tool specs / implementations live on
  `feat/tool-integration`, branched off `fix/validation-issues`.
- Bespoke method-config means **no hard dependency** on `feat/boltzgen-tool`.
  The two efforts touch the same files (`internal/domain` `DesignPlan`,
  `plan.go`, `render.go`, `internal/tui/plan.go`, `main.go`) but only add
  independent optional fields and dispatch cases. If `feat/boltzgen-tool`
  merges first, later tools rebase onto the result; conflicts, if any, are
  small and additive.
- Each tool is implemented in its own worktree off `feat/tool-integration`,
  consistent with the project's worktree-per-unit-of-work convention.

---

## 9. Testing & GPU validation

**Automated, gates every merge:**

- Unit tests per tool: input schema; parameter → CLI / YAML / FASTA mapping
  (table tests); output confidence / metrics parsing against fixture files;
  preflight checks; `/plan` section rendering.
- Install-time **container smoke test** per tool — the cheapest invocation
  that proves the image and the tool's entrypoint work (no GPU, minimal or no
  weights), in the style of BoltzGen's `boltzgen check`.
- `go build ./...` and `go test ./...` green at the end of every phase.

**User-validated, batched at the end:**

- The full GPU end-to-end run of each tool cannot be unit-tested — it needs
  the GB10 and each tool's weights. After all six tools merge, one batched
  validation pass runs each tool on the GB10. The checklist:

  1. `/install <tool>` succeeds and the smoke test passes.
  2. The agent proposes a spec / parameters; the review surface (a `/plan`
     section for design tools, the confirmation gate for predictors) renders
     it; preflight reports valid.
  3. Approval submits the job; the job completes.
  4. Designs / predicted structures land **with populated scores** (in the
     designs panel for design tools; in the job result envelope for
     predictors).
  5. Provenance records the tool call and parameters.

---

## 10. Risks

- **Chai-2 open-weights availability.** Chai-2 may not have a runnable public
  release. Resolved in that tool's spec, phase 1, before any code is written;
  the honest outcome (document + de-register) is acceptable.
- **Weak local orchestration model.** A small-active local model may author
  subtly-wrong specs (wrong chain, wrong residue indices). Mitigations: the
  per-tool grounding skill, the preflight, and the human review / edit gate
  are three independent backstops.
- **Per-tool container / dependency conflicts.** BoltzGen hit a NumPy-2 break;
  any tool may surface its own dependency issue. Each tool's spec carries a
  container phase with explicit "user builds + validates on the GB10"
  checkpoints, since container fixes cannot be fully settled from code.
- **Upstream output drift.** Confidence / metrics file formats can change
  between tool releases. Mitigation: parsers tested against pinned fixtures;
  unknown columns carried through rather than assumed.

---

## 11. Deliverables

- This umbrella spec.
- Six per-tool design specs under `docs/superpowers/specs/`, each followed by
  its own implementation plan and implementation.
- One cross-cutting spec — **editable tool-call review** (§4) — for the inline
  accept / edit / cancel surface shared by predictors and design tools.
  Scheduled after the predictors land, so it has concrete consumers to design
  against.
- For each tool: a bespoke tool type, a local-backend adapter, a preflight, a
  review-integration section, score ingestion, a grounding skill, and tests.
- A follow-up note to expand `design.rfdiffusion` / `design.proteinmpnn` /
  `design.bindcraft` beyond the generic schema (out of scope here).
