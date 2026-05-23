## `design.rfdiffusion2` integration

**Date:** 2026-05-23
**Branch:** `feat/rfdiffusion2` (off `dev` at `8f5dfc5`)
**Worktree:** `/home/gonjim/Projects/proteus-rfdiff2`
**Umbrella:** `docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md`
**Tool 6 of 6 — closes the umbrella.** Upstream:
https://github.com/RosettaCommons/RFdiffusion2 — Baker-lab atom-level
flow-matching active-site scaffolding.

## Goal

Fully wire `design.rfdiffusion2` end-to-end. It is currently a tier-3 dead-end:
registered in `cmd/fova/main.go`, accepted by `plan.create` as
`MethodRFdiffusion2`, and installable (the `[tools.rfdiffusion2]` recipe,
`rfdiffusion2.Containerfile`, and `[data.rfdiffusion2_weights]` already exist),
but `RunDesign` returns *"no local adapter on this backend yet"* — the same
defect class `design.chai2` had before retirement and `design.rfantibody` had
before the 2026-05-22 integration.

This wires it end-to-end mirroring `design.rfantibody` exactly: typed schema, a
new adapter driving the full Hydra-driven pipeline, `/plan` method-config,
metrics-CSV score ingestion, grounding skill.

## Branch base & platform

Branches off `dev` at `8f5dfc5` (post-rfantibody-merge; `MethodConfig`
infrastructure + `internal/assets` skill-count assertion at 12).

`design.rfdiffusion2` is **x86-only**: `rfdiffusion2.Containerfile` fails the
build on aarch64 — upstream Graylab does not publish a Python 3.12 aarch64
PyRosetta wheel, which the inference-time idealization step requires. So the
GPU end-to-end run **cannot be validated on the GB10**; everything else (typed
schema, adapter logic, preflight, `/plan` wiring, unit tests, score-CSV parser)
is fully CI-verifiable on any platform.

---

## 1. Background — current state

**Tool:** `internal/tools/design/rfdiffusion2.go` builds `design.rfdiffusion2`
on the shared `designTool` wrapper (generic `target/hotspots/num_designs`
schema, doesn't fit an enzyme active-site scaffolding tool whose primary input
is a catalytic motif).

**Plan:** `MethodRFdiffusion2` is already in `internal/tools/plan/compat.go`
(both `bindcraft`-style alias maps, `toolForMethod`, `designToolForMethod`),
listed in the `binder` and `enzyme` `compat` slices, and in both `method`
schema descriptions in `plan.go`. So the planner can already propose it — but
the plan can't execute, because no adapter exists.

**Recipe/Containerfile/weights:**
`internal/backends/local/tools.toml` `[tools.rfdiffusion2]` already exists with
`image_tag fova/rfdiffusion2:0.1.0`, `disk_gb 9.0`, `gpu true`, and the
`[data.rfdiffusion2_weights]` block listing the two RFD diffusion checkpoints
plus the bundled LigandMPNN tied weights.
`internal/backends/local/containerfiles/rfdiffusion2.Containerfile` is a tier-3
container — NGC PyTorch 25.04 base + cloned upstream + `pyrosetta-installer` +
`ENTRYPOINT python /opt/rfdiffusion2/rf_diffusion/benchmark/pipeline.py`.

**Adapter:** none. `local.RunDesign(ctx, "design.rfdiffusion2", ...)` returns
the placeholder error from `internal/backends/local/runner.go`.

---

## 2. RFdiffusion2 reference (for the implementer)

### Pipeline shape — one entrypoint

Unlike RFantibody's 3-stage driver (`rfdiffusion` → `proteinmpnn` → `rf2`),
RFdiffusion2's `rf_diffusion/benchmark/pipeline.py` is a *single* Hydra-driven
entrypoint that internally runs:

1. backbone diffusion (atom-level flow matching);
2. inference-time idealization (PyRosetta);
3. *(when `stop_step='end'`)* inline LigandMPNN sequence fitting;
4. *(when `stop_step='end'`)* inline Chai-1 fold;
5. metrics emission (CSV under `pipeline_outputs/<timestamp>_<config>/`).

The default `stop_step` halts after step 2 (backbone + motif). `stop_step='end'`
runs all five.

### Invocation surface

The script is Hydra-configured; every parameter is a positional override. The
canonical commands documented upstream:

```bash
# One bundled active-site demo.
python rf_diffusion/benchmark/pipeline.py \
    --config-name=open_source_demo \
    sweep.benchmarks=active_site_unindexed_atomic_partial_ligand

# AME-41 enzyme benchmark (the curated reference set).
python rf_diffusion/benchmark/pipeline.py \
    --config-name=enzyme_bench_n41_fixedligand in_proc=True

# Full pipeline through Chai-1 fold.
python rf_diffusion/benchmark/pipeline.py \
    --config-name=open_source_demo stop_step='end'
```

User catalytic motifs ride into the same `--config-name` via Hydra
add-overrides: `+inference.input_pdb=/work/<motif>.pdb
contigmap.contigs=[<contigs>]`. (If pipeline.py rejects the `+` form for
`inference.input_pdb` — pinned during Stream B implementation — the fallback
is to write a one-off benchmark YAML to `/work/` and pass `--config-dir=/work`.)

### Inputs

- **Benchmark** — picks the `--config-name` + bundled `sweep.benchmarks`. Two
  values shipped in v1:
  - `active_site_demo` → `--config-name=open_source_demo sweep.benchmarks=active_site_unindexed_atomic_partial_ligand` (default)
  - `enzyme_bench_n41` → `--config-name=enzyme_bench_n41_fixedligand`
- **Motif PDB** (optional override) — workspace path to a user catalytic motif
  PDB. When set, the adapter stages it into the workdir and **adds**
  `+inference.input_pdb=/work/<motif>.pdb` `contigmap.contigs=[<Contigs>]` to
  the chosen `--config-name`. `Contigs` is required in that case.
- **Contigs** — Hydra-style contig string (e.g. `5-15,A10-30,5-15`); only
  consulted when `MotifPDB` is set.
- **Stop step** — `"design"` (backbone+motif only) | `"end"` (full pipeline,
  default).
- **Inference toggles** — `guidepost_xyz_as_design_bb` (unindexed motif XYZ
  overwrites the matched backbone), `idealize_sidechain_outputs` (PyRosetta
  idealization post-diffusion). Both `*bool`, default to upstream.
- **`num_designs`**, **`seed`** — the standard count + reproducibility knobs.

### Output & scores

Final stage writes a CSV under `pipeline_outputs/<timestamp>_<config>/...` whose
columns include (per upstream README, *exact path and naming pinned against a
real run during Stream B*):

- `metrics.IdealizedResidueRMSD.rmsd_constellation` — backbone idealization gap
- `motif_ideality_diff` — motif-atom ideality
- `contig_rmsd_<a>_<b>_<s>` — pairwise RMSDs across `des`/`pred`/`ref` at
  `backbone`/`c_alpha`/`full_atom`/`motif_atom` granularity

Plus the Chai-1 prediction PDBs (one per design tag) under the same tree when
`stop_step='end'`.

Canonical score-key folding (in the adapter):

- `metrics.IdealizedResidueRMSD.rmsd_constellation` → `idealized_residue_rmsd`
- `motif_ideality_diff` → `motif_ideality_diff`
- `contig_rmsd_des_ref_motif_atom` → `motif_rmsd` (the design-vs-reference
  motif-atom RMSD is the headline "did the motif scaffold succeed?" score)
- every other numeric column carried through under its header name (lower-cased)

A row with no matching prediction PDB is dropped; a prediction PDB with no
score row gets an empty `Scores`. An empty PDB set is an error.

### Weights

The two `[data.rfdiffusion2_weights]` blocks already pull the RFD diffusion
checkpoints and the bundled LigandMPNN tied weights into
`~/.fova/models/rfdiffusion2/` (the recipe's `weights_paths = ["/models/rfdiffusion2"]`).

**Chai-1 weights are not pre-staged** — when `stop_step='end'`, the inline
Chai-1 fold step will pull `chai_lab`'s weights from `chaiassets.com` on first
run **inside the container** (no reuse of `design.chai1`'s `/models/chai1/`
cache). The first full-pipeline run takes the chai_lab download hit (~1.3 GB);
subsequent runs reuse the in-container cache layer. *Phase-2 follow-up:
pre-stage chai1 weights into `/models/rfdiffusion2/chai1/` and set
`CHAI_DOWNLOADS_DIR`. Document the first-run cost in the grounding skill; not a
v1 blocker.*

---

## 3. Component A — bespoke `rfdiffusion2Tool` + typed schema

Replace the shared `designTool` wrapper in `internal/tools/design/rfdiffusion2.go`
with a bespoke `rfdiffusion2Tool`, mirroring `rfantibodyTool`:

- `type RFdiffusion2Params = domain.RFdiffusion2Params` package alias
- typed `InputSchema()` covering the 8 fields below
- `Execute` that validates, resolves workspace paths, submits the job, and
  `persist`s designs.

`NewRFdiffusion2Tool(workspaceRoot, mgr, backend, st)` keeps its signature so
`cmd/fova/main.go` is untouched. `RequiresConfirmation` returns `true`.

**Schema (`RFdiffusion2Params`):**

- `benchmark` — enum `active_site_demo` (default) | `enzyme_bench_n41` — the
  bundled active-site sweep.
- `motif_pdb` — optional workspace path to a user catalytic motif PDB; when
  set, overrides the benchmark's bundled motif.
- `contigs` — Hydra-style contig string (e.g. `5-15,A10-30,5-15`); required
  when `motif_pdb` is set, ignored otherwise.
- `num_designs` — design count (positive integer).
- `seed` — `*int`; reproducibility.
- `guidepost_xyz_as_design_bb` — `*bool`; map `inference.guidepost_xyz_as_design_bb`.
- `idealize_sidechain_outputs` — `*bool`; map `inference.idealize_sidechain_outputs`.
- `stop_step` — enum `design` | `end` (default `end`); the full-pipeline knob
  (decision Q1).

No JSON-schema `required` array — every field has either a default
(`benchmark`, `stop_step`) or is a conditional override (`motif_pdb` / `contigs`,
the `*bool` toggles, `seed`, `num_designs`). `Validate` enforces the
conditional shape (see §6).

---

## 4. Component B — `/plan` integration

`RFdiffusion2Params` + `RFdiffusion2Params.Validate()` in `internal/domain`
(committed in the Foundation). `MethodConfig` gains an
`RFdiffusion2 *RFdiffusion2Params` field, alongside `BoltzGen`, `LigandMPNN`,
and `RFantibody`. **Pure addition** to the existing struct so the
schema-expansion trio rebases cleanly post-merge (per the brief).

`plan.create` (`internal/tools/plan/plan.go`): for `MethodRFdiffusion2`,
unmarshal `method_params` into `RFdiffusion2Params`, run `Validate()`, attach
to `p.MethodConfig.RFdiffusion2`. Exactly the LigandMPNN / RFantibody
method-config pattern. **Additive** `if method == MethodRFdiffusion2 { ... }`
block alongside the existing three.

`/plan` rendering (`render.go`): an `RFdiffusion2` section in the
`MethodConfig` dispatch (BoltzGen / LigandMPNN / RFantibody / RFdiffusion2)
showing benchmark, motif PDB (when set), contigs (when set), num designs, stop
step. **Additive** `case mc.RFdiffusion2 != nil:` in the existing switch.

`compat.go` already maps `MethodRFdiffusion2` + aliases + tool keys (line ~27
and lines 113-114, 165-166, 193-194). The `method` schema-description strings
in `plan.go` (lines ~111-117, ~263-265) already list `RFdiffusion2`. **No
change needed in `compat.go` or those schema strings.**

---

## 5. Component C — adapter (`adapter_rfdiffusion2.go`)

A brand-new `ToolAdapter`. It:

- unmarshals the typed request;
- stages the motif PDB (when `MotifPDB` is set) into the container workdir;
- writes a **driver script** that:
  - cds to `/work`;
  - invokes `python /opt/rfdiffusion2/rf_diffusion/benchmark/pipeline.py` with
    the assembled Hydra overrides:
    - `--config-name=open_source_demo sweep.benchmarks=active_site_unindexed_atomic_partial_ligand` *(active_site_demo)*
    - **or** `--config-name=enzyme_bench_n41_fixedligand in_proc=True` *(enzyme_bench_n41)*
    - **plus**, when `MotifPDB` set: `+inference.input_pdb=/work/<base>.pdb contigmap.contigs=[<Contigs>]`
    - **plus** any of `inference.guidepost_xyz_as_design_bb=<bool>`, `inference.idealize_sidechain_outputs=<bool>`, `inference.num_designs=<n>`, `seed=<n>`, `stop_step=<step>` whose params are set
    - **plus** `outdir=/work/out hydra.run.dir=/work/out` so the output landing tree is deterministic;
- runs the container with `Entrypoint: "bash"`, `Cmd: ["/work/run.sh"]` —
  the `Entrypoint` field on `ContainerRunArgs` already exists, added by
  `rfantibody`. The image ENTRYPOINT (`python pipeline.py`) is overridden
  because we want the script to set `outdir` deterministically and chain
  post-run extraction;
- validates runtime, image, and the `os.Stat` weights cache for `rfdiffusion2`
  (the `~/.fova/models/rfdiffusion2/` mount → `/models/rfdiffusion2`);
- collects the metrics CSV + the Chai-1 prediction PDBs into the
  `{"designs":[...]}` envelope.

Mounts: `{env.WorkDir:/work}` + `{modelsCache:/models/rfdiffusion2}`. GPU on
(env.Recipe.GPU).

---

## 6. Component D — preflight (`RFdiffusion2Params.Validate`)

Value-shape only (no filesystem); the tool's `Execute` and the adapter's
`Invoke` handle path existence. Checks:

- `Benchmark`, when set, is `""`/`active_site_demo`/`enzyme_bench_n41`;
- `MotifPDB` non-empty ⇒ `Contigs` non-empty after `strings.TrimSpace`
  (further contig-string structural parsing is `pipeline.py`'s job — Hydra
  surfaces an error if the contigs are malformed);
- `MotifPDB`, when set, has a `.pdb` suffix;
- `NumDesigns`, when non-zero, is `> 0`;
- `Seed`, when set, is `>= 0`;
- `StopStep`, when set, is `""`/`design`/`end`.

Path existence: `Execute` resolves `MotifPDB` via `tools.ResolveWorkspacePath`;
adapter's `Invoke` `os.Stat`s the resolved path and rejects if missing or a
directory.

---

## 7. Component E — score ingestion

The adapter parses the metrics CSV emitted by `pipeline.py`. The exact filename
under `pipeline_outputs/<timestamp>_<config>/` is **pinned against a real run
during Stream B** (the README documents column shapes but not the file path);
the parser keys by column header so it survives upstream renames.

Per row:

- the design tag column links the row to its extracted Chai-1 prediction PDB
  (also discovered by tag in the same output tree);
- numeric columns become `designOut.Scores`, with the canonical-key folding in
  §2 (`idealized_residue_rmsd`, `motif_ideality_diff`, `motif_rmsd` from
  `contig_rmsd_des_ref_motif_atom`), and every other numeric column carried
  through under its header name (lower-cased).

One fova `Design` per scored prediction PDB. Persisted with
`Origin: domain.OriginRFDiff2MPNN` (already present in `types.go:132`),
`Application: domain.AppEnzyme`. A prediction with no score row → empty
`Scores`, never a failure. An empty PDB set is an error.

When `stop_step='design'` the run produces no Chai-1 PDBs; the adapter instead
collects the diffusion-stage backbone PDBs under the same tree as
`StructureFile`, and `Scores` will only contain idealization/motif columns (no
Chai-1 confidence). A `stop_step='design'` run with no backbone PDBs is an
error.

---

## 8. Component F — grounding skill

New `internal/assets/embed/skills/rfdiffusion2-design.md` (with `name` +
`description` frontmatter, house style — match `rfantibody-design.md` and
`ligandmpnn-design.md`):

- the `benchmark` choice (`active_site_demo` vs `enzyme_bench_n41`) — which is
  the right fast-path for a catalytic motif scaffolding run;
- supplying a user motif: the `motif_pdb` + `contigs` pair (with contigs
  syntax — `<flanker>,<chain><start>-<end>,<flanker>` — worked example);
- when to flip `guidepost_xyz_as_design_bb` (unindexed motif coords) vs leave
  upstream default;
- when to leave `idealize_sidechain_outputs` on (PyRosetta-clean motifs)
  vs off (faster but raw geometry);
- `stop_step` choice — full pipeline (`end`, default) vs backbone-only
  (`design`) — and when each is the right call;
- reading the scores: `motif_rmsd` (lower better, < 1 Å for a tight scaffold),
  `idealized_residue_rmsd`, `motif_ideality_diff`; the Chai-1 confidence
  columns when present;
- first-full-run cost: the inline Chai-1 fold step downloads chai_lab's
  weights inside the container on first invocation (~1.3 GB; subsequent runs
  reuse the in-container cache);
- one worked example: scaffolding a 3-residue catalytic triad onto a designed
  backbone, full pipeline, `motif_rmsd < 0.5 Å` filter.

---

## 9. Testing

**Unit:**

- `RFdiffusion2Params.Validate` table test (every valid + each invalid case).
- Schema (every documented property + the `benchmark` and `stop_step` enums).
- Hydra-override builder per `Benchmark` × motif-override × inference-toggle
  matrix (table test; pinned strings).
- Metrics-CSV parser against a fixture CSV that mirrors the upstream column
  shape — verifies the canonical-key folding and unknown-column carry-through.
- Adapter `Invoke` with a stubbed container runtime that creates a fake output
  tree (PDBs + CSV) under `/work/out/`; verifies the envelope shape and
  weights/image guards.
- `plan.create` RFdiffusion2 method-config (`applyRFdiffusion2MethodConfig`).
- `/plan` RFdiffusion2-section rendering.

**Container:** the existing `[tools.rfdiffusion2]` install-time smoke test
(`python -c 'import torch; assert torch.cuda.is_available(); print(torch.__version__)'`)
stays valid; this spec doesn't change install.

**End-to-end:** **x86-only**. The full GPU pipeline cannot run on the GB10.
It is validated on an x86 GPU box when one is available; the umbrella's
batched GB10 validation does not cover it.

Every phase ends `go build ./...` and `go test ./...` green; final integration
adds `gofmt -l internal/ cmd/` printing nothing.

**TUI test hygiene** (from the brief — three prior flake fixes confirm the
pattern): if any test in `internal/tui/` triggers `/plan approve` on an
RFdiffusion2 plan, it will spawn an agent-loop goroutine via `startTurn`. **Use
the existing `drainTurn(t, m)` helper in `app_test.go`** before the test
returns, or the test will flake. The streams in this spec don't add new TUI
tests, but the rendering stream (C) must verify any existing `/plan` TUI tests
still pass after the new `MethodConfig.RFdiffusion2` dispatch case lands.

---

## 10. Phasing

1. **Foundation (coordinator):** `RFdiffusion2Params` + `MethodConfig.RFdiffusion2`
   + `Validate()` in `internal/domain`. Build + tests green. One commit.
2. **Streams (parallel, 4 worktrees off the foundation):** A bespoke tool; B
   adapter; C `/plan` integration; D grounding skill. *No chai2-style retirement
   needed — chai2 is already gone.*
3. **Integration:** merge the streams; bump `internal/assets/assets_test.go`
   skill-count 12→13 (rebase to whatever count `dev` shows + 1 if a sibling
   session lands first); build + full suite + `gofmt -l internal/ cmd/` clean.
4. **Merge to `dev`** (user-confirmed; the dev merge is outward — see brief).

---

## 11. Files touched

**Foundation:** `internal/domain/types.go` (add `RFdiffusion2Params`,
`MethodConfig.RFdiffusion2`); `internal/domain/rfdiffusion2.go` + `_test.go`
(new — `Validate`).

**Streams:**

- A: `internal/tools/design/rfdiffusion2.go` (replace),
  `internal/tools/design/rfdiffusion2_test.go` (new),
  `internal/tools/design/design_test.go` (drop the rfdiffusion2 designTool row).
- B: `internal/backends/local/adapter_rfdiffusion2.go` (new),
  `internal/backends/local/adapter_rfdiffusion2_test.go` (new).
  `ContainerRunArgs.Entrypoint` field is already present from rfantibody — no
  `runtime_exec.go` change.
- C: `internal/tools/plan/plan.go`, `internal/tools/plan/plan_test.go`,
  `internal/tools/plan/render.go`, `internal/tools/plan/render_test.go`. (No
  `internal/tui/plan.go` change is needed; it renders generically through
  `RenderPlan`/`RenderPlanWithOpts`.)
- D: `internal/assets/embed/skills/rfdiffusion2-design.md` (new).

**Integration:** `internal/assets/assets_test.go` (skill-count bump 12→13;
rebase as noted).

---

## 12. Out of scope

- Modal-backend execution (the local backend only — though Modal is the only
  current x86 path; deferred).
- Sweep cardinality / multi-target sweeps (`sweep.*` beyond `sweep.benchmarks`).
- `in_proc=False` (SLURM distribution).
- Diffusion-step counts and other deep Hydra knobs (the "maximal" set rejected
  in scoping Q3).
- User-authored full benchmark YAMLs (only Hydra-override motif injection in
  v1; pinning the benchmark YAML schema is a Phase-2 follow-up if the override
  path proves insufficient).
- Pre-staging Chai-1 weights into the rfdiffusion2 container (Risk 2 below).
- The optional `dev/show_bench.py` PyMOL visualization utility.
- Rebuilding the closed-loop wet-lab tie-in chai2 used to anchor — that
  surface area is now owned by the `submit-to-adaptyv` skill.

---

## 13. Risks

- **x86-only.** RFdiffusion2 cannot be GPU-validated on the GB10 (no
  Python-3.12 aarch64 PyRosetta wheel). Integration is fully CI-verifiable;
  the end-to-end run waits for an x86 GPU box. *Known, accepted constraint;
  Containerfile already fails the build on aarch64 with a clear "use
  design.rfdiffusion + design.proteinmpnn instead" message.*
- **Chai-1 weights not pre-staged.** When `stop_step='end'` (the default), the
  inline Chai-1 fold pulls `chai_lab`'s weights from `chaiassets.com` inside
  the container on first run (no reuse of `design.chai1`'s `/models/chai1/`
  cache). First full-pipeline run takes the ~1.3 GB chai_lab download cost;
  later runs reuse the in-container cache. *Phase-2 follow-up: pre-stage into
  `/models/rfdiffusion2/chai1/` and set `CHAI_DOWNLOADS_DIR`. Document in the
  grounding skill; not a v1 blocker.*
- **Metrics-CSV exact path + columns.** The README documents column-name
  shapes but not the file path under `pipeline_outputs/<timestamp>_<config>/`.
  The adapter glob-searches for the CSV; the parser keys by header (with the
  canonical-key folding in §2 + unknown-column carry-through) so it survives
  small renames. *Pinned against a real run during Stream B; if the file name
  or path materially differs from expectation, the parser changes are isolated
  to `adapter_rfdiffusion2.go` and don't ripple.*
- **Hydra `+inference.input_pdb` override.** Standard Hydra add-prefix syntax;
  works for any Hydra-config app, but `pipeline.py` may enforce a stricter
  schema. *Pin during Stream B. If rejected, the fallback is writing a one-off
  benchmark YAML to `/work/` and passing `--config-dir=/work`; this is purely
  an adapter-internal change and doesn't touch the schema or `/plan`.*
- **Cross-session friction (per brief).** Running concurrently with
  `feat/knowledge-pdb-search` (zero overlap) and `feat/editable-review` (light
  overlap on `internal/tui/` only — no design-tool touch). After this branch
  merges to `dev`, the schema-expansion trio
  (`design.rfdiffusion` / `proteinmpnn` / `bindcraft`) will add similar
  `MethodConfig.<Tool>` fields. **Every `MethodConfig` / plan-dispatch /
  render-dispatch edit in this spec is purely additive (no reordering, no
  rename) so the trio rebases cleanly.**
- **Asset skill-count race.** `internal/assets/assets_test.go` currently
  asserts 12. This spec bumps it to 13 in the integration commit. If a sibling
  session lands a built-in skill first, the integration step rebases against
  whatever count `dev` shows + 1.

---

## 14. Acceptance criteria

- `go build ./...` and `go test ./...` green on every commit.
- `gofmt -l internal/ cmd/` prints nothing.
- `cmd/fova/main_test.go`'s expected-tools list keeps `design.rfdiffusion2` in
  it (no removal).
- An RFdiffusion2 plan created via `plan.create` (with `method_params` carrying
  at minimum a benchmark) renders with the new RFdiffusion2 section under
  `/plan` and is approvable.
- `RFdiffusion2Params.Validate` rejects every invalid shape in §6.
- The adapter's `Invoke`, against a stubbed container runtime that emits a
  fixture CSV + prediction PDBs, returns a `{"designs":[...]}` envelope whose
  per-design `Scores` carry `motif_rmsd`, `motif_ideality_diff`, and
  `idealized_residue_rmsd` correctly folded from the fixture columns.
- The `rfdiffusion2-design` skill loads via the assets discovery path
  (`go test ./internal/assets/` green after the skill-count bump).
