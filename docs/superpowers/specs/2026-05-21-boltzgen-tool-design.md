# BoltzGen tool — full development design

**Date:** 2026-05-21
**Branch:** `feat/boltzgen-tool` (off `fix/validation-issues`)
**Upstream:** https://github.com/HannesStark/boltzgen — Stark et al. (2026),
"BoltzGen: Toward Universal Binder Design"

## Goal

BoltzGen is a core design tool in fova — the primary binder method on
aarch64/Grace (the GB10), where BindCraft is blocked by the PyRosetta wheel
gap. Today the integration is a stub: the container crashes, the agent cannot
author a real design specification, and almost none of BoltzGen's CLI surface
is reachable. This spec fully develops the tool so the agent can intelligently
propose a structure, a specification, and run parameters; the user supervises
by editing and approving; and the run completes end-to-end with ranked,
scored designs landing in the designs panel.

---

## 1. Background — current state and its problems

`design.boltzgen` is currently a thin wrapper over the shared `designTool`
(generic `target` / `hotspots` / `num_designs` schema). The adapter
(`internal/backends/local/adapter_boltzgen.go`) generates a hardcoded 8-line
YAML via `buildBoltzGenSpec` and runs the container.

Problems found during the 2026-05-21 validation session:

1. **Container crashes.** The `boltzgen.Containerfile` builds on the NGC
   `nvcr.io/nvidia/pytorch:25.04-py3` base. `pip install boltzgen==0.3.2`
   blanks the NGC numpy pin and installs `numpy==2.0.2`. The NGC base bundles
   `tensorboard` (pulled by PyTorch-Lightning's default `TensorBoardLogger`)
   compiled against NumPy 1.x; it calls the removed `np.string_` and crashes
   at pipeline step 1. (See the `j_759fab5d` job log.)
2. **The agent cannot author a specification.** BoltzGen's expressive power is
   its `specification.yaml`. fova emits a fixed minimal YAML — designed-chain
   length `80..140`, designed chain `B`, target chain `A`, no binding sites
   beyond a residue list, no constraints, no protocol choice. The agent has no
   way to express the design.
3. **CLI surface unreachable.** `protocol`, `budget`, `diffusion_batch_size`,
   `steps`, `alpha`, `filter_biased`, `additional_filters`, the
   inverse-folding knobs — none are exposed.
4. **Results are thin.** The adapter collects the final CIFs but ignores
   BoltzGen's metrics CSVs, so designs land without scores.

---

## 2. BoltzGen reference (for the implementer)

### CLI commands

- `boltzgen run <spec.yaml> --output <dir> [flags]` — the full pipeline.
- `boltzgen check <spec.yaml>` — validate a spec; emits a visualization
  mmCIF. Cheap, no GPU, no weights.
- `boltzgen configure` / `boltzgen execute` — split config generation from
  execution (not used by fova; `run` is sufficient).
- `boltzgen download` — pre-fetch weights (not used; `run` auto-downloads).
- `boltzgen merge` — merge multi-run outputs (out of scope).

### Protocols (`--protocol`)

`protein-anything` (default), `peptide-anything`, `protein-small_molecule`,
`antibody-anything`, `nanobody-anything`, `protein-redesign`. The protocol
sets defaults and which pipeline steps run.

### Pipeline steps (`--steps`)

`design`, `inverse_folding`, `folding`, `design_folding`, `affinity`,
`analysis`, `filtering`.

### Agent-facing run parameters

These map to `boltzgen run` flags and become the `design.boltzgen` schema:

| fova param | BoltzGen flag | Notes |
|---|---|---|
| `protocol` | `--protocol` | enum, default `protein-anything` |
| `num_designs` | `--num_designs` | intermediate designs (10k–60k in practice) |
| `budget` | `--budget` | final diversity-optimized set size |
| `diffusion_batch_size` | `--diffusion_batch_size` | optional |
| `steps` | `--steps` | optional subset |
| `alpha` | `--alpha` | diversity trade-off 0..1 |
| `filter_biased` | `--filter_biased` | bool |
| `additional_filters` | `--additional_filters` | list of `feature>thr` |
| `refolding_rmsd_threshold` | `--refolding_rmsd_threshold` | float |
| `inverse_fold_num_sequences` | `--inverse_fold_num_sequences` | int |
| `inverse_fold_avoid` | `--inverse_fold_avoid` | disallowed AAs |
| `step_scale`, `noise_scale` | `--step_scale`, `--noise_scale` | advanced |
| `reuse` | `--reuse` | resume an interrupted run |

**fova owns (never agent-facing):** `--output`, `--cache` / `HF_HOME`,
`--devices`, `--num_workers`, `--config_dir`, `--moldir`, `--models_token`,
`--design_checkpoints` / `--folding_checkpoint` / `--affinity_checkpoint`,
`--no_subprocess`.

### Specification YAML schema (summary)

`entities` is a list of `protein` / `ligand` / `file`:
- **protein**: `id`, `sequence` (notation: `80..140` length range, fixed AAs,
  `15..20AAAA18` mixed, `C` for fixed Cys), `secondary_structure`,
  `binding_types` (string `uuuBBBuNNN` or `binding:`/`not_binding:` ranges),
  `cyclic` (bool), `residue_constraints` (per-position `allowed`/`disallowed`).
- **ligand**: `id` (or list), `ccd` code or `smiles`, `binding_types`.
- **file**: `path`, `include`/`exclude` (chains + `res_index`),
  `include_proximity` (radius), `binding_types`, `structure_groups`
  (visibility), `design` (redesignable residues), `secondary_structure`,
  `design_insertions`, `fuse`, `msa`, `reset_res_index`.

`constraints`: `bond` (`atom1`/`atom2` as `[chain, res, atom]`), `total_len`
(`min`/`max`).

**Critical caveat:** residue indices are **1-based** and use the canonical
mmCIF `label_asym_id` / `label_seq_id`, NOT the author numbering. File paths
inside a spec are resolved relative to the spec file's directory.

### Output layout (`--output <dir>`)

- `final_ranked_designs/final_<budget>_designs/*.cif` — final quality +
  diversity set (the designs fova ingests).
- `final_ranked_designs/final_designs_metrics_<budget>.csv` — metrics for
  that set.
- `final_ranked_designs/all_designs_metrics.csv` — metrics for all.
- `results_overview.pdf` — plots.
- `intermediate_designs*/`, `config/`, `steps.yaml` — intermediate artifacts.

---

## 3. Component A — container fix (Phase 1)

Keep the NGC `pytorch:25.04` base: the GB10 is Blackwell (sm_121) and NGC's
torch carries the Blackwell kernels that stock PyPI wheels lack. Upstream's
own `cuda:12.2.2` Dockerfile is too old for the GB10, so it cannot be copied.

Resolve the NumPy-2.0 conflict via a fallback ladder, build-tested on the GB10:

1. **Primary — eliminate the TensorBoard import.** BoltzGen runs inference,
   not training; it needs no logger. Disable PyTorch-Lightning's
   `TensorBoardLogger` via BoltzGen's documented `--config <step> key=value`
   override (e.g. `trainer.logger=false`). If it takes, `tensorboard` is
   never imported and no dependency surgery is needed.
2. **If the override is unavailable — repair the env.** After
   `pip install boltzgen`, `pip install -U tensorboard` to a NumPy-2-compatible
   release; verify `torch.from_numpy` still works; reinstall torch only if it
   is genuinely broken.
3. **Last resort — numpy downgrade.** Override BoltzGen's `numpy==2.0.2` pin
   to the NGC-compatible 1.x line; only viable if BoltzGen does not use
   numpy-2-only APIs.

**Smoke test:** replace `boltzgen --help` with `boltzgen check` on a bundled
tiny example spec — validates the install + spec parsing without GPU or
weights. The full GPU run is user-validated.

This phase requires build/run iteration on the GB10; the implementation plan
carries explicit "user builds + validates" checkpoints.

---

## 4. Component B — spec authoring

The agent writes the BoltzGen spec YAML itself (decision: raw YAML validated
by `boltzgen check`, not a Go-modeled schema).

**`design.boltzgen_check` tool** — cheap, no GPU/weights. Input: `spec_path`
(workspace-relative). Runs `boltzgen check` in the container; returns
`{valid, errors[], visualization_path}`. The agent calls it while iterating;
`plan.create` and `/plan approve` also run it so a run never starts on an
invalid spec.

**Skill `internal/skills/builtin/boltzgen-spec.md`** — the grounding that lets
the agent author specs intelligently and automatically. Contents:
- The spec schema from §2 (entities, sequence notation, `binding_types`,
  `structure_groups`, `design`, `secondary_structure`, `constraints`,
  `residue_constraints`, cyclic, ligands).
- The 6 protocols and when each applies.
- The 1-based `label_asym_id` indexing caveat and the relative-path rule.
- The write → `boltzgen.check` → run workflow.
- 2–3 upstream example specs verbatim as grounding: a vanilla protein binder,
  a peptide-with-binding-site, and the PD-L1 fab example.

**Authoring flow:** agent reads the skill → writes `spec.yaml` to the
workspace with `fs.write` → `design.boltzgen_check` → fixes any check errors
→ proceeds. The agent produces the first spec automatically from the target +
the user's intent; the user only edits/approves.

---

## 5. Component C — `design.boltzgen` rework

Replace the thin `designTool` wrapper with a bespoke `boltzGenTool`
implementing `tools.Tool` with its own typed schema:

- `spec_path` (string, required) — the spec YAML the agent authored.
- the run parameters from §2's table, each typed and validated (enum for
  `protocol` and `steps`, bounded ints/floats).

The adapter (`adapter_boltzgen.go`) is reworked: it no longer generates the
YAML — it consumes the agent's spec file. It stages the spec plus every
structure file the spec references into the container workdir, preserving the
relative layout BoltzGen expects. It maps the typed params to `boltzgen run`
CLI flags and fixes the infra flags.

`internal/tools/design/boltzgen.go` (the thin wrapper created on
`fix/validation-issues`) is replaced by this bespoke tool. `compat.go` and
`cmd/fova/main.go` registration stay; the registration just constructs the
new tool type.

---

## 6. Component D — plan integration & review

`DesignPlan` gains an optional method-config section: `SpecPath`
(workspace-relative path to the spec YAML) and the typed run parameters.
Populated only for `method = BoltzGen`.

`plan.create` accepts `method_spec_path` + `method_params`. For BoltzGen it
runs `boltzgen check` and rejects the plan if the spec is invalid — consistent
with the existing install-status and tool-registration guards.

`/plan` renders the BoltzGen section: protocol, num_designs, budget, the spec
**path**, a ~15-line spec preview, and the check result (✓ valid + the
visualization mmCIF path, or the errors).

The spec is a plain workspace file. The user edits it with their own editor
(the absolute path is shown in `/plan`). `/plan approve` **re-runs
`boltzgen check`** to catch edits: on failure it shows the errors and holds;
on success it submits the `design.boltzgen` job with the final spec + params
(via the agent turn that `/plan approve` already starts).

---

## 7. Component E — results ingestion

On job completion the adapter:
- reads `final_ranked_designs/final_<budget>_designs/*.cif` as the design
  structure files;
- parses `final_ranked_designs/final_designs_metrics_<budget>.csv` and
  attaches every metric column to the corresponding `Design.Scores`;
- persists one fova `Design` per row, so designs land in the designs panel
  with real numbers instead of bare CIFs;
- copies `results_overview.pdf` to a known workspace location and surfaces
  its path.

The exact CSV column → score-key mapping is settled against a real BoltzGen
CSV during implementation; unknown columns are carried through as raw scores
rather than dropped. If fova specifically needs ipSAE and BoltzGen does not
emit it, the agent can run the existing `score.ipsae` tool on the refolded
CIFs — out of scope for this spec.

---

## 8. Component F — testing

- **Unit:** param → CLI-flag mapping (table test); metrics-CSV parsing against
  a fixture CSV; `boltzgen check` result parsing; `/plan` BoltzGen-section
  rendering; spec + referenced-file staging.
- **Container:** the `boltzgen check` smoke test runs at install time.
- **End-to-end:** the full GPU run is user-validated on the GB10 — it cannot
  be unit-tested (needs the GPU + ~6 GB of weights).

---

## 9. Phasing

1. **P1 — container fix.** Build + smoke-test green; user-validated on the GB10.
2. **P2 — tools + adapter + skill.** `design.boltzgen_check`, the bespoke
   `design.boltzgen` schema, the adapter rework (spec from file, params →
   CLI), the `boltzgen-spec` skill. Build + unit tests green.
3. **P3 — plan integration.** `DesignPlan` spec section, `plan.create`,
   `/plan` rendering, `/plan approve` re-check.
4. **P4 — results ingestion.** Metrics-CSV parsing, designs persisted with
   scores, `results_overview.pdf` surfaced.

Every phase ends build + `go test ./...` green. P1 and the final end-to-end
run are validated by the user on the GB10.

---

## 10. Files touched

- `internal/backends/local/containerfiles/boltzgen.Containerfile` — rewrite
- `internal/backends/local/tools.toml` — boltzgen recipe (smoke test)
- `internal/backends/local/adapter_boltzgen.go` — major rework
- `internal/tools/design/boltzgen.go` — replace wrapper with bespoke tool
- `internal/tools/design/boltzgen_check.go` — new `design.boltzgen_check`
- `internal/skills/builtin/boltzgen-spec.md` — new skill
- `internal/domain/types.go` — `DesignPlan` method-config section
- `internal/tools/plan/plan.go` — `plan.create` spec inputs + check
- `internal/tools/plan/render.go`, `internal/tui/plan.go` — `/plan` rendering
- `internal/tui/app.go` — `/plan approve` re-check for BoltzGen
- `cmd/fova/main.go` — register `design.boltzgen_check`
- tests alongside each

---

## 11. Out of scope

- `boltzgen merge` / multi-run parallelism.
- `boltzgen configure`/`execute` split (fova uses `run`).
- BoltzGen model training.
- An in-TUI YAML editor — the spec is edited with the user's own editor.
- Modal-backend execution of BoltzGen — local backend only here.
- Generalizing the spec-file/method-config mechanism to other design tools.

---

## 12. Open risks

- **Container.** The NumPy-2 fix cannot be fully settled from code; the
  fallback ladder is build-tested on the GB10. If all three rungs fail, the
  fallback is to raise it with upstream BoltzGen.
- **Weak orchestration model.** Authoring a correct spec needs reasoning; a
  3B-active local model may produce subtly wrong specs (wrong chain, wrong
  indices). Mitigations: the `boltzgen-spec` skill grounds the agent,
  `boltzgen check` + its visualization catch structural errors, and the
  human review/edit step is the backstop.
- **Metrics mapping.** BoltzGen's CSV columns are mapped to fova score keys
  during implementation against a real CSV; unknowns are carried through.
