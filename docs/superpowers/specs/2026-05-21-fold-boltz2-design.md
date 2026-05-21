# `fold.boltz2` — full integration design

**Date:** 2026-05-21
**Branch:** `feat/tool-integration` (off `fix/validation-issues`)
**Umbrella:** `docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md`
**Tool 1 of 6.** Upstream: https://github.com/jwohlwend/boltz — Boltz-2,
biomolecular structure (and affinity) prediction.

## Goal

Make `fold.boltz2` a real Boltz-2 integration: the agent proposes a complete,
typed prediction specification (multi-entity complex, MSA choice, model
parameters); the user supervises through the confirmation gate; preflight
rejects every input-side error before a job starts; and the predicted
structures come back **with their confidence scores populated** so the
pipeline can rank them.

Scope is the **core predictor** (umbrella §2 decision): multi-entity structure
prediction. Constraints, templates, and affinity prediction are deferred to a
later iteration.

---

## 1. Background — current state and its problems

`fold.boltz2` is built on the shared `foldJobTool`
(`internal/tools/fold/foldjob.go`). Its entire input schema is
`{sequences: {chainID: aaSeq}, save_as}`. The adapter
(`internal/backends/local/adapter_boltz2.go`) renders every chain as a
`protein` record with `msa: empty` and runs `boltz predict` with a fixed flag
set.

Problems:

1. **Protein-only.** `sequences` is `map[chainID]aaSequence`. DNA, RNA, and
   ligand entities — core Boltz-2 inputs — cannot be expressed at all.
2. **No model control.** `recycling_steps`, `sampling_steps`,
   `diffusion_samples`, `step_scale`, `output_format` are hardcoded or
   absent. The agent cannot trade speed for accuracy or request multiple
   diffusion samples.
3. **No MSA control.** `msa: empty` is hardcoded. Boltz-2 calls
   single-sequence mode "not recommended"; the free ColabFold MSA server and
   precomputed MSA files are unreachable.
4. **Scores are dropped.** `parseBoltz2Output` returns each PDB with an empty
   `Scores` map. Boltz-2 writes a rich `confidence_*.json` per model
   (`confidence_score`, `ptm`, `iptm`, `complex_plddt`, per-chain pTM, …) and
   fova ingests none of it. Predicted structures land unranked.
5. **No input-side validation.** A bad request (empty sequence, unknown chain)
   is only caught after the container starts, inside a failed job.

---

## 2. Boltz-2 reference (for the implementer)

### `boltz predict` — flags fova uses

fova owns the infrastructure flags; the rest map from the typed schema.

**fova-owned (never agent-facing):** `--out_dir`, `--cache` (host weights
mount), `--devices 1`, `--num_workers`, `--override` (the workdir is fresh per
call; without it a stale subdir is a silent no-op), `--no_kernels` (required
on sm_121 / the GB10 per upstream issue #663), `--use_msa_server` (added when
any entity selects the server).

**Agent-facing → schema (§3):** `--recycling_steps` (default 3),
`--sampling_steps` (200), `--diffusion_samples` (1), `--step_scale` (1.638,
useful range 1–2), `--output_format` (`pdb` | `mmcif`).

**Out of scope for v1:** affinity flags (`--sampling_steps_affinity`,
`--diffusion_samples_affinity`, `--affinity_mw_correction`), `--use_potentials`,
`--write_full_pae` / `--write_full_pde`, MSA subsampling
(`--subsample_msa`, `--num_subsampled_msa`, `--max_msa_seqs`),
`--msa_pairing_strategy`.

### YAML input schema — the v1 subset

```yaml
version: 1
sequences:
  - protein:               # or dna / rna
      id: A                # a chain id, or [A, B] for identical copies
      sequence: MKQ...
      msa: empty            # 'empty', a staged .a3m/.csv path, or omitted
      cyclic: true          # optional
  - ligand:
      id: L
      smiles: "CCO"        # exactly one of smiles / ccd
```

`version: 1` is emitted at the document root. MSA keys: `msa: empty` forces
single-sequence; a path uses a precomputed MSA; omitting `msa` lets
`--use_msa_server` fill it. `msa` and `cyclic` apply to protein/dna/rna only;
ligands carry just `id` + `smiles`/`ccd`.

**Deferred (not emitted in v1):** `modifications`, `constraints`, `templates`,
`properties` (affinity).

### Output layout

```
out/predictions/in/
  in_model_0.pdb                 # (or .cif) — one per diffusion sample
  confidence_in_model_0.json
  in_model_1.pdb
  confidence_in_model_1.json
  ...
```

The input file is staged as `in.yaml`, so the prediction stem is `in`.

### Confidence JSON — fields fova ingests

```json
{
  "confidence_score": 0.84, "ptm": 0.84, "iptm": 0.82,
  "ligand_iptm": 0.0, "protein_iptm": 0.82,
  "complex_plddt": 0.84, "complex_iplddt": 0.82,
  "complex_pde": 0.89, "complex_ipde": 5.16,
  "chains_ptm": {"0": 0.85, "1": 0.83},
  "pair_chains_iptm": {"0": {"0": 0.85, "1": 0.81}, "1": {...}}
}
```

All pLDDT/pTM-family values are in [0, 1], higher is better; `pde`/`ipde` are
Ångström, lower is better.

### Weights

Boltz-2 weights (~6 GB) are fetched **at install time** by the installer's
post-build hook (`models_cache.EnsureWeights`) into
`~/.fova/models/boltz2`, and bind-mounted at `/models` per run. This is
unlike BoltzGen (runtime HuggingFace download): for boltz2 a missing weights
cache genuinely means install did not complete, so the adapter validates the
cache exists rather than creating it.

---

## 3. Component A — bespoke `boltz2Tool` + typed schema

Replace the shared `foldJobTool` for boltz2 with a bespoke `boltz2Tool` in
`internal/tools/fold/boltz2.go` implementing `tools.Tool`. `fold.chai1` keeps
`foldJobTool` until tool 2; `foldJobTool` retires when both predictors are
bespoke. `NewBoltz2` gains a `workspaceRoot` argument to resolve path inputs
(`msa` files, `save_as`).

**`InputSchema()`** — object with:

- `entities` (array, required, ≥1). Each entity:
  - `type` — enum `protein` | `dna` | `rna` | `ligand` (required)
  - `id` — chain id `string`, or `array` of strings for identical copies
    (required)
  - `sequence` — `string`; required for `protein`/`dna`/`rna`
  - `smiles` / `ccd` — `string`; exactly one required for `ligand`
  - `msa` — `string`: `empty` (default) | `server` | a workspace path to a
    `.a3m`/`.csv` MSA. `protein`/`dna`/`rna` only.
  - `cyclic` — `boolean`, optional. `protein`/`dna`/`rna` only.
- `recycling_steps`, `sampling_steps`, `diffusion_samples` — `integer`,
  optional; omitted ⇒ Boltz-2 default.
- `step_scale` — `number`, optional, useful range 1–2.
- `output_format` — enum `pdb` (default — pipeline / ipSAE compatibility) |
  `mmcif`.
- `save_as` — `string`, optional workspace path for the top-ranked structure.

**Tool behaviour:**

- `RequiresConfirmation` → **true** (was false). The agent's proposed spec now
  passes through the confirmation gate (umbrella §4, predictor surface).
- `EstimatedCostUSD` / `EstimatedDuration` — modest fixed estimates, as today.
- `Execute` runs the **tool-level preflight** (§5) first; on failure it
  returns a clean domain error and submits no job. On success it submits the
  background job exactly as `foldJobTool` does and returns the job ID.

The `id`-as-string-or-array is modelled with a small custom type that
unmarshals both JSON shapes into `[]string`.

---

## 4. Component B — adapter rework (`adapter_boltz2.go`)

The adapter request type grows from `{sequences, save_as}` to mirror the typed
schema (`entities`, model-param pointer fields, `output_format`, `save_as`).
Pointer fields distinguish "unset" (omit the flag) from a real zero.

**YAML compiler.** Replace `writeBoltz2YAML` with a compiler that emits the §2
v1 YAML for any entity mix: `version: 1`; one `protein`/`dna`/`rna`/`ligand`
record per entity; `id` as a scalar or list; `sequence` or `smiles`/`ccd`;
`msa` (`empty` or staged path, omitted for `server`); `cyclic` when set.
Entities are emitted in input order; deterministic for testing.

**CLI mapping.** A table-driven `boltz2Args(req)` maps the model-param fields
to flags (mirrors BoltzGen's `boltzGenArgs`): an unset pointer omits the flag.
fova fixes the infrastructure flags from §2 and appends `--use_msa_server`
when any entity's `msa` is `server`.

**MSA file staging.** A precomputed `.a3m`/`.csv` MSA referenced by an entity
is copied into the container workdir (like BoltzGen stages referenced files);
the YAML references the staged relative path.

**Infra guards (kept).** The adapter retains its existing pre-run checks —
container runtime available, image present, **weights cache exists** (`os.Stat`,
not `MkdirAll` — see §2 weights note) — each returning a clear "run /install
boltz2" error.

---

## 5. Component C — preflight

`fold.boltz2` preflight is **tool-level** input validation, run at the top of
`Execute` before any job is submitted — so a malformed proposal returns a
clean, recoverable error to the agent and never becomes a failed job.

Checks:

- at least one entity;
- each `type` is one of the four enums;
- `protein`/`dna`/`rna` entities have a non-empty `sequence` over a valid
  alphabet (protein: the 20 canonical amino acids; dna/rna: nucleotides);
- `ligand` entities have exactly one of `smiles` / `ccd`, non-empty;
- chain `id`s are unique across all entities;
- `msa` is `empty`, `server`, or a path that resolves inside the workspace
  and exists; `msa`/`cyclic` not set on a `ligand`;
- model parameters within bounds — positive integers; `step_scale` in 1–2;
  `output_format` in the enum.

A full RDKit-grade SMILES parse is not possible in Go; preflight does the
non-empty / mutually-exclusive check, and an invalid SMILES surfaces from the
container. The infrastructure checks (image, weights cache, runtime) remain in
the adapter (§4) — still pre-GPU. Surfacing infra-readiness before `Submit` is
a possible refinement for the implementation plan, not a requirement.

---

## 6. Component D — score ingestion

Rework `parseBoltz2Output`: for each predicted model file
`predictions/in/in_model_<N>.{pdb,cif}`, read the sibling
`confidence_in_model_<N>.json` and populate that model's `designOut.Scores`:

- standard keys: `plddt` ← `complex_plddt`, `ptm`, `iptm`;
- carried through: `confidence_score`, `ligand_iptm`, `protein_iptm`,
  `complex_iplddt`, `complex_pde`, `complex_ipde`;
- `chains_ptm` flattened to `chain_<k>_ptm`.

`pair_chains_iptm` (nested, non-scalar) and the `.npz` PAE/PDE matrices are not
surfaced in v1 — `--write_full_pae`/`--write_full_pde` stay off. A missing or
unparseable confidence file yields an empty `Scores` map for that model and
**no error** — a successful prediction without scores still returns the
structure. Models are returned sorted by file name so `save_as` copies a
deterministic top structure.

---

## 7. Component E — grounding skill

New `internal/skills/builtin/boltz2-predict.md` so the agent authors correct
specs automatically:

- the entity model (protein/dna/rna/ligand; `id`, `sequence`, `smiles`/`ccd`;
  `id` lists for homo-oligomers; `cyclic`);
- the MSA choice and its accuracy trade-off — `empty` (fast, offline, the
  default), `server` (free ColabFold API, better accuracy, needs network),
  precomputed file;
- sane model-parameter defaults and when to raise `diffusion_samples` or
  `sampling_steps`;
- how to read the confidence scores (`plddt`, `ptm`, `iptm` ranges; what a
  low `iptm` means for an interface).

---

## 8. Component F — testing

**Unit:**

- `InputSchema()` shape;
- the typed-schema → YAML compiler — a table test across entity types, `id`
  scalar vs list, all three MSA modes, `cyclic`;
- `boltz2Args` param → CLI-flag mapping (table test);
- confidence-JSON → `Scores` parsing against a fixture JSON, including
  `chains_ptm` flattening and the missing-file case;
- preflight — a table of valid and invalid requests.

**Container:** the existing install-time `smoke_test` (a 30-aa monomer fold)
remains valid and is kept.

**End-to-end:** the full GPU run is user-validated on the GB10 in the umbrella's
batched validation pass — it needs the GPU and ~6 GB of weights.

Every phase ends `go build ./...` and `go test ./...` green.

---

## 9. Phasing

1. **P1 — tool + schema.** Bespoke `boltz2Tool`, typed `InputSchema()`,
   `NewBoltz2(workspaceRoot, …)`, `RequiresConfirmation` true, registration in
   `cmd/fova/main.go`. Build + tests green.
2. **P2 — adapter + preflight.** YAML compiler, `boltz2Args`, MSA staging,
   tool-level preflight. Build + unit tests green.
3. **P3 — score ingestion.** `parseBoltz2Output` reads `confidence_*.json`;
   fixture-based tests.
4. **P4 — grounding skill** + final pass: `boltz2-predict.md`, the GPU
   validation checklist entry, docs.

---

## 10. Files touched

- `internal/tools/fold/boltz2.go` — replace the `foldJobTool` constructor with
  the bespoke `boltz2Tool`
- `internal/tools/fold/foldjob.go` — unchanged (chai1 still uses it)
- `internal/backends/local/adapter_boltz2.go` — major rework
- `internal/skills/builtin/boltz2-predict.md` — new grounding skill
- `cmd/fova/main.go` — `NewBoltz2` signature (add `workspace`)
- tests alongside each (`boltz2_test.go`, `adapter_boltz2_test.go`)

---

## 11. Out of scope

- **Affinity prediction**, constraints (bond/pocket/contact), structural
  templates, per-residue `modifications` — deferred Boltz-2 features.
- Full PAE/PDE matrix surfacing.
- The **editable** confirmation surface — its own cross-cutting spec (umbrella
  §4); boltz2 ships with the enriched binary gate.
- Modal-backend execution — local backend only.
- MSA subsampling / pairing-strategy flags.

---

## 12. Risks

- **`--use_msa_server` network dependency.** The ColabFold server can be slow
  or unreachable; it is opt-in (default `empty`) so the offline path is never
  blocked, and the grounding skill states the trade-off.
- **Confidence-JSON drift.** The Containerfile floats Boltz-2 on `main`. Field
  names could change between releases. Mitigation: the parser is tested
  against a pinned fixture and treats a missing/unparseable file as
  "no scores", not a failure.
- **Weak orchestration model.** A small local model may propose a subtly wrong
  entity set. Mitigations: the `boltz2-predict` skill, the preflight, and the
  confirmation gate are three independent backstops.
