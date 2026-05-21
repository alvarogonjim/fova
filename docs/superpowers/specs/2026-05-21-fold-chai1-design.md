# `fold.chai1` — full integration design

**Date:** 2026-05-21
**Branch:** `feat/tool-integration` (off `fix/validation-issues`)
**Umbrella:** `docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md`
**Tool 2 of 6.** Upstream: https://github.com/chaidiscovery/chai-lab —
Chai-1, a multi-modal foundation model for biomolecular structure prediction.

## Goal

Make `fold.chai1` a real Chai-1 integration: the agent proposes a complete,
typed prediction specification (multi-entity complex, restraints, templates,
MSA choice, model parameters); the user supervises through the confirmation
gate; preflight rejects every input-side error before a job starts; and the
predicted structures come back **with their confidence scores populated**.

Scope is the **full Chai-1 inference surface** (umbrella §2 + the chai1
brainstorm decision): multi-entity prediction, restraints, templates, glycans.

---

## 1. Background — current state and its problems

`fold.chai1` is built on the shared `foldJobTool`
(`internal/tools/fold/foldjob.go`). Its input schema is
`{sequences: {chainID: aaSeq}, save_as}`. The adapter
(`internal/backends/local/adapter_chai1.go`) renders every chain as a
`>protein` FASTA record and runs `chai-lab fold <fasta> <out>` with no flags.

Problems:

1. **Protein-only.** `sequences` is `map[chainID]aaSequence`. Ligands (SMILES),
   DNA, RNA, and glycans — all core Chai-1 inputs — cannot be expressed.
2. **No restraints or templates.** Chai-1's restraint file (`constraint_path`)
   and template support are unreachable.
3. **No model control.** `num_trunk_recycles`, `num_diffn_timesteps`,
   `num_diffn_samples`, `num_trunk_samples`, `recycle_msa_subsample`, `seed`
   are all fixed at the Chai-1 defaults.
4. **No MSA control.** Chai-1 runs on ESM embeddings by default (no MSA); the
   ColabFold MSA server and precomputed MSA directories are unreachable.
5. **Scores are dropped.** `parseChai1Output` returns each structure with an
   empty `Scores` map — the `scores.model_idx_N.npz` files are ignored.
6. **No input-side validation.** A bad request fails only inside a started job.

Once this work lands, `chai1Tool` is bespoke and `boltz2Tool` is already
bespoke (tool 1), so **`foldJobTool` has no remaining users** — this spec
retires it.

---

## 2. Chai-1 reference (for the implementer)

### `chai-lab fold` — invocation

The image ENTRYPOINT is `["chai-lab"]`; the command is
`chai-lab fold <input.fasta> <output_dir> [flags]`. The CLI maps to
`run_inference` (`chai_lab/chai1.py`); typer renders each snake_case parameter
as a `--kebab-case` flag.

**fova-owned (never agent-facing):** `--device`, `--low-memory`,
`use_esm_embeddings` (left at its `True` default), the output dir.

**Derived from the typed schema (§3):** `--use-msa-server`,
`--msa-directory`, `--use-templates-server`, `--template-hits-path`,
`--constraint-path` — fova sets these from the schema and the files it
stages, never as raw agent input.

**Agent-facing → schema:** `--num-trunk-recycles` (default 3),
`--num-diffn-timesteps` (200), `--num-diffn-samples` (5),
`--num-trunk-samples` (1), `--recycle-msa-subsample` (0), `--seed`.

### Input FASTA format

One record per entity. Header is `>` + entity type + `|name=<id>`; body is the
sequence / SMILES / glycan string:

```
>protein|name=A
MKQHKAMIVALIVICITAVVAALVTRKDLC
>ligand|name=L
CCCCCCCCCCCCCC(=O)O
>rna|name=R
ACGUACGU
>dna|name=D
ACGTACGT
>glycan|name=G
<chai-1 glycan string>
```

Ligand bodies are SMILES. Modified residues are written inline as
`AAA(SEP)AAA`. Glycan bodies use Chai-1's glycan notation — fova carries the
string verbatim from the schema to the FASTA and does not interpret it (the
grounding skill teaches the agent the notation).

### Restraint file format (`constraint_path`)

A CSV (`pandas.read_csv`) with these columns, in order:

| Column | Meaning |
|---|---|
| `restraint_id` | unique id — fova generates `restraint_0`, `restraint_1`, … |
| `chainA` | first partner chain id (required) |
| `res_idxA` | residue ref, e.g. `A219`, or atom ref `A219@CA` (optional) |
| `chainB` | second partner chain id (required) |
| `res_idxB` | residue/atom ref (optional) |
| `min_distance_angstrom` | float ≥ 0 (optional) |
| `max_distance_angstrom` | float ≥ 0, ≥ min when both set (optional) |
| `connection_type` | `covalent` \| `contact` \| `pocket` (required) |
| `confidence` | float 0.0–1.0 (optional, default 1.0) |
| `comment` | free text (optional) |

### MSA and templates

Chai-1's real default is ESM embeddings, no MSA — single-sequence is fully
supported (not "not recommended", unlike Boltz-2). MSA is therefore a clean
opt-in: `--use-msa-server` (free ColabFold) or a precomputed `--msa-directory`
of `.aligned.pqt` files. Templates are opt-in via `--use-templates-server` or
a precomputed `--template-hits-path`.

### Output layout

```
out/
  pred.model_idx_0.cif        # one structure per diffusion sample
  scores.model_idx_0.npz      # scores for that sample
  pred.model_idx_1.cif
  scores.model_idx_1.npz
  ...
  msa_depth.pdf               # only when an MSA search ran
```

### `scores.model_idx_N.npz` — keys

`.npz` is a ZIP of `.npy` arrays. `run_inference` writes the dict from
`get_scores()`:

| npz key | shape | fova use |
|---|---|---|
| `aggregate_score` | scalar | `aggregate_score` |
| `ptm` | scalar | `ptm` |
| `iptm` | scalar | `iptm` |
| `per_chain_ptm` | 1-D (per chain) | flattened to `chain_<i>_ptm` |
| `has_inter_chain_clashes` | scalar | `has_inter_chain_clashes` (0/1) |
| `per_chain_pair_iptm` | 2-D | not surfaced in v1 |
| `chain_chain_clashes` | 2-D | not surfaced in v1 |

### Weights

Chai-1 weights (~1.3 GB) are fetched **at install time** by the installer's
post-build hook (`models_cache.EnsureWeights`, declared `[[tools.chai1.weights]]`)
into `~/.fova/models/chai1` and bind-mounted at `/models`
(`CHAI_DOWNLOADS_DIR=/models`). A missing cache means install did not
complete, so the adapter validates the cache exists (`os.Stat`) rather than
creating it — same as `fold.boltz2`.

---

## 3. Component A — bespoke `chai1Tool` + typed schema

Replace the shared `foldJobTool` for chai1 with a bespoke `chai1Tool` in
`internal/tools/fold/chai1.go`. boltz2 (tool 1) already moved off `foldJobTool`;
once chai1 is bespoke, `foldJobTool` has no users — this spec **deletes
`internal/tools/fold/foldjob.go` and `internal/tools/fold/foldjob_test.go`**.
`NewChai1` gains a `workspaceRoot` argument.

**`InputSchema()`** — object with:

- `entities` (array, required, ≥1). Each entity:
  - `type` — enum `protein` | `dna` | `rna` | `ligand` | `glycan` (required)
  - `id` — chain id `string` (required)
  - `sequence` — `string`; required for `protein`/`dna`/`rna`
  - `smiles` — `string`; required for `ligand`
  - `glycan` — `string` (Chai-1 glycan notation); required for `glycan`
  - `msa` — `string`: `default` (ESM embeddings — Chai-1's default) | `server`
    | a workspace path to a `.aligned.pqt` MSA. `protein` only.
- `restraints` (array, optional). Each restraint:
  - `connection_type` — enum `contact` | `pocket` | `covalent` (required)
  - `chain_a` (required), `res_a` (optional — `219` or `219@CA`)
  - `chain_b` (required), `res_b` (optional)
  - `min_distance`, `max_distance` — `number`, optional
  - `confidence` — `number` 0–1, optional
  - `comment` — `string`, optional
- `templates` (object, optional): `server` (`boolean`), `hits_path`
  (`string`, workspace path).
- Model params (optional → Chai-1 default): `num_trunk_recycles`,
  `num_diffn_timesteps`, `num_diffn_samples`, `num_trunk_samples`,
  `recycle_msa_subsample` (`integer`); `seed` (`integer`).
- `save_as` — `string`, optional workspace path for the top-ranked structure.

**Tool behaviour:**

- `RequiresConfirmation` → **true** — the proposed spec passes through the
  confirmation gate (umbrella §4, predictor surface).
- `EstimatedCostUSD` / `EstimatedDuration` — modest fixed estimates.
- `Execute` runs the **tool-level preflight** (§5) first; on failure it
  returns a clean domain error and submits no job. On success it resolves
  workspace paths, re-marshals, and submits the background job.

---

## 4. Component B — adapter rework (`adapter_chai1.go`)

The adapter request type grows from `{sequences, save_as}` to mirror the typed
schema. Pointer fields distinguish "unset" from a real zero.

**FASTA compiler.** Replace `writeChai1FASTA` with a compiler that emits one
`>`-record per entity for all five types (§2 FASTA format), in input order,
deterministic.

**Restraint-file compiler.** When `restraints` is non-empty, write the §2 CSV
to the workdir: fova generates `restraint_id` (`restraint_0`, …), maps each
typed restraint to a row, leaves optional cells blank. The adapter passes
`--constraint-path` pointing at it.

**CLI mapping.** A table-driven `chai1Args(req)` maps the model-parameter
fields to flags (mirrors BoltzGen's `boltzGenArgs` / `boltz2Args`): an unset
pointer omits the flag. fova fixes the infrastructure flags from §2 and
derives `--use-msa-server` (any entity `msa: server`), `--msa-directory` (a
staged precomputed MSA), `--use-templates-server` / `--template-hits-path`
(from `templates`), `--constraint-path` (when restraints exist).

**File staging.** Precomputed MSA directories and `template_hits_path` files
referenced by the schema are staged into the container workdir; the YAML/flags
reference the staged paths.

**Infra guards (kept).** Container runtime available, image present, **weights
cache exists** (`os.Stat`, not `MkdirAll` — §2 weights note).

---

## 5. Component C — preflight

`fold.chai1` preflight is **tool-level** input validation, run at the top of
`Execute` before any job is submitted.

Checks:

- at least one entity; each `type` is one of the five enums;
- `protein`/`dna`/`rna` have a non-empty `sequence` over a valid alphabet
  (modified-residue tokens like `(SEP)` permitted in protein sequences);
- `ligand` has a non-empty `smiles`; `glycan` has a non-empty `glycan`;
- chain `id`s are unique across all entities;
- every restraint's `chain_a` / `chain_b` references a declared entity id;
  `connection_type` is one of the three enums; `max_distance ≥ min_distance`
  when both set; `confidence` in 0–1;
- `msa` is `default` / `server` / a path that resolves inside the workspace;
  `templates.hits_path` resolves inside the workspace;
- model parameters are positive integers.

A malformed proposal returns a clean, recoverable error to the agent — no
doomed job is launched. The infrastructure checks (image, weights cache,
runtime) remain in the adapter (§4).

---

## 6. Component D — score ingestion (Go `.npz` reader)

A small, self-contained `.npz` reader — new file `internal/backends/local/npz.go`:

- `.npz` is a ZIP archive of `.npy` members; open it with `archive/zip`.
- Each `.npy` member has a header — the `\x93NUMPY` magic, a version, and an
  ASCII dict literal giving `descr` (dtype), `fortran_order`, `shape` — then
  raw little-endian array bytes.
- The reader decodes **scalars** (shape `()`) and **1-D arrays** for the
  dtypes Chai-1 emits: `<f4`, `<f8`, `<i8`, and `bool`. 2-D arrays are read as
  "present but unsupported" and ignored by the caller.
- Exposed as `readNPZ(path) (map[string]npzValue, error)` where `npzValue`
  carries a scalar float and/or a `[]float64`.

`parseChai1Output` pairs each `pred.model_idx_N.cif` with its
`scores.model_idx_N.npz` sibling and populates `designOut.Scores`:

- `aggregate_score`, `ptm`, `iptm` — scalars;
- `per_chain_ptm` — flattened to `chain_<i>_ptm`;
- `has_inter_chain_clashes` — `0`/`1`.

A missing or unparseable `.npz` yields an empty `Scores` map for that model
and **no error**. Structures are returned sorted by model index.

---

## 7. Component E — grounding skill

New `internal/skills/builtin/chai1-predict.md`:

- the entity model — protein/dna/rna/ligand(SMILES)/glycan; modified residues
  as `(XXX)` tokens;
- restraints — when and how to add `contact` / `pocket` / `covalent` entries;
- templates — when to enable the template server vs supply a hits file;
- the MSA choice — ESM embeddings (default, fast, offline), `server`
  (ColabFold, better accuracy, needs network), precomputed `.aligned.pqt`;
- model-parameter defaults and when to raise `num_diffn_samples`;
- how to read the scores (`aggregate_score`, `ptm`, `iptm`,
  `has_inter_chain_clashes`).

---

## 8. Component F — testing

**Unit:**

- `InputSchema()` shape;
- the FASTA compiler — a table test across all five entity types;
- the restraint-CSV compiler — a table test, including optional blank cells;
- `chai1Args` param → flag mapping;
- the `.npz` reader — against a small committed fixture `.npz` holding a
  scalar, a 1-D array, and a bool;
- `parseChai1Output` — fixture `pred`+`scores` pair, including the
  missing-`.npz` case;
- preflight — a table of valid and invalid requests.

**Container:** the existing install-time `smoke_test` (a 30-aa monomer fold)
remains valid and is kept.

**End-to-end:** the full GPU run is user-validated on the GB10 in the
umbrella's batched validation pass.

Every phase ends `go build ./...` and `go test ./...` green.

---

## 9. Phasing

1. **P1 — tool + schema.** Bespoke `chai1Tool`, typed `InputSchema()`,
   `NewChai1(workspaceRoot, …)`, `RequiresConfirmation` true; delete
   `foldjob.go` + `foldjob_test.go`; update `cmd/fova/main.go`. Build + tests
   green.
2. **P2 — adapter.** FASTA compiler, restraint-CSV compiler, `chai1Args`,
   staging, the reworked `Invoke`. Build + unit tests green.
3. **P3 — score ingestion.** `npz.go` reader + `parseChai1Output`;
   fixture-based tests.
4. **P4 — grounding skill** + final pass: `chai1-predict.md`, the GPU
   validation checklist entry, docs.

---

## 10. Files touched

- `internal/tools/fold/chai1.go` — replace the `foldJobTool` constructor with
  the bespoke `chai1Tool`
- `internal/tools/fold/foldjob.go` — **delete** (no remaining users)
- `internal/tools/fold/foldjob_test.go` — **delete**
- `internal/tools/fold/boltz2.go` / `boltz2_test.go` — only if they reference
  symbols from `foldjob.go`; reuse helpers must move to a surviving file
- `internal/backends/local/adapter_chai1.go` — major rework
- `internal/backends/local/npz.go` — new `.npz` reader
- `internal/skills/builtin/chai1-predict.md` — new grounding skill
- `cmd/fova/main.go` — `NewChai1` signature (add `workspace`)
- tests alongside each

---

## 11. Out of scope

- Modal-backend execution — local backend only.
- The **editable** confirmation surface — its own cross-cutting spec
  (umbrella §4); chai1 ships with the enriched binary gate.
- 2-D score matrices (`per_chain_pair_iptm`, `chain_chain_clashes`).
- `fasta_names_as_cif_chains` and custom `msa_server_url`.

---

## 12. Risks

- **`foldJobTool` retirement.** Deleting `foldjob.go` requires that boltz2's
  bespoke tool no longer depends on it. boltz2's `boltz2_test.go` reuses
  `stubBackend` / `newFoldTestDeps` / `waitJob` — defined in `foldjob_test.go`.
  Those helpers must move into a surviving test file (e.g. a new
  `internal/tools/fold/fold_test_helpers_test.go`) as part of P1, or boltz2's
  tests break. The implementation plan must sequence this explicitly.
- **`.npz` / `.npy` parsing.** The `.npy` format is simple and stable, but the
  reader must handle the dtypes Chai-1 actually emits. Mitigation: the fixture
  `.npz` is generated from a real Chai-1 run during implementation; the reader
  treats unknown dtypes / 2-D arrays as "ignored", never a hard failure.
- **Glycan notation.** fova carries the glycan string verbatim and does not
  validate it beyond non-empty; a malformed glycan string surfaces from the
  container. The grounding skill documents the notation.
- **MSA server / template server network dependency.** Both are opt-in; the
  default ESM-embeddings path is fully offline.
