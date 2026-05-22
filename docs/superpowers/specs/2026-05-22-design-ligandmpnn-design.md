# `design.ligandmpnn` — full integration design

**Date:** 2026-05-22
**Branch:** `feat/ligandmpnn` (off `dev`)
**Umbrella:** `docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md`
**Tool 3 of 6** (delivered before rfdiffusion2 — rfdiffusion2 is x86-only and
cannot be validated on the GB10; LigandMPNN is pure-PyTorch and can).
Upstream: https://github.com/dauparas/LigandMPNN — Dauparas et al.,
ligand-aware protein sequence design.

## Goal

Make `design.ligandmpnn` a real LigandMPNN integration: the agent proposes a
complete, typed sequence-design configuration; the user supervises through the
`/plan` flow; preflight rejects every input-side error before a job starts;
and the designed sequences come back **with their confidence scores
populated**.

Scope is the **full LigandMPNN `run.py` surface** (the ligandmpnn brainstorm
decision): all five model variants, residue selection, AA biases, symmetry,
membrane models, and side-chain packing.

## Branch base

Unlike `fold.boltz2` / `fold.chai1` (predictors, built off
`feat/tool-integration`), `design.ligandmpnn` is a **design method** — it
integrates through the `/plan` flow. That flow's `DesignPlan.MethodConfig`
infrastructure landed on `dev` with BoltzGen, so this work branches off `dev`.

---

## 1. Background — current state and its problems

`design.ligandmpnn` is built on the shared `designTool`
(`internal/tools/design/design.go`) — the generic
`target / hotspots / num_designs / contigs / settings` schema — and has **no
local adapter**. It is registered, advertised, and accepted by `plan.create`
as `MethodLigandMPNN`, but `RunDesign` returns *"no local adapter on this
backend yet"* — an approved LigandMPNN plan dead-ends at execution (umbrella
tier 3).

Problems:

1. **No adapter.** LigandMPNN cannot run on the local backend at all.
2. **Generic schema.** The shared schema cannot express LigandMPNN's surface —
   model type, residue selection, biases, symmetry, membrane labels, packing.
3. **Scores dropped.** LigandMPNN writes per-sequence `overall_confidence`,
   `ligand_confidence`, and `sequence_recovery` into its FASTA headers; fova
   ingests none of it.
4. **No `/plan` configuration.** A LigandMPNN plan carries no method-specific
   run configuration — the agent cannot propose, and the user cannot review,
   anything beyond the bare method name.

---

## 2. LigandMPNN reference (for the implementer)

### Invocation

The image runs `python /opt/ligandmpnn/run.py` (recipe `[tools.ligandmpnn]`,
entrypoint already set). All inputs are command-line flags — there is no spec
file. Checkpoints live under `/models` (bind-mounted from
`~/.fova/models/ligandmpnn/`, fetched at install time by `EnsureWeights`).

### Model types and checkpoints

`--model_type` ∈ `protein_mpnn`, `ligand_mpnn`, `soluble_mpnn`,
`global_label_membrane_mpnn`, `per_residue_label_membrane_mpnn`. Each takes a
matching `--checkpoint_<type>` path. fova defaults `model_type` to
`ligand_mpnn` (the tool is `design.ligandmpnn`; `design.proteinmpnn` is the
separate tool for the plain model) and selects the checkpoint file from
`/models` for the chosen type.

### Agent-facing flags → schema

| schema field | `run.py` flag |
|---|---|
| `model_type` | `--model_type` (+ fova picks `--checkpoint_<type>`) |
| `pdb` | `--pdb_path` |
| `num_designs` | `--number_of_batches` |
| `batch_size` | `--batch_size` |
| `temperature` | `--temperature` |
| `seed` | `--seed` |
| `redesigned_residues` | `--redesigned_residues` |
| `fixed_residues` | `--fixed_residues` |
| `chains_to_design` | `--chains_to_design` |
| `bias_AA` | `--bias_AA` |
| `omit_AA` | `--omit_AA` |
| `bias_AA_per_residue` | `--bias_AA_per_residue` (a staged JSON file) |
| `omit_AA_per_residue` | `--omit_AA_per_residue` (a staged JSON file) |
| `ligand_use_atom_context` | `--ligand_mpnn_use_atom_context` |
| `ligand_use_side_chain_context` | `--ligand_mpnn_use_side_chain_context` |
| `ligand_cutoff` | `--ligand_mpnn_cutoff_for_score` |
| `symmetry_residues` | `--symmetry_residues` |
| `symmetry_weights` | `--symmetry_weights` |
| `homo_oligomer` | `--homo_oligomer` |
| `global_transmembrane_label` | `--global_transmembrane_label` |
| `transmembrane_buried` | `--transmembrane_buried` |
| `transmembrane_interface` | `--transmembrane_interface` |
| `pack_side_chains` | `--pack_side_chains` |
| `number_of_packs_per_design` | `--number_of_packs_per_design` |
| `pack_with_ligand_context` | `--pack_with_ligand_context` |
| `repack_everything` | `--repack_everything` |

**fova owns (never agent-facing):** `--out_folder`, the `--checkpoint_*`
paths, `--checkpoint_path_sc` (the side-chain packer), `--verbose`,
`--file_ending`. fova always passes `--save_stats 0`.

Residue references use the LigandMPNN convention `<chain><number>[<icode>]`,
e.g. `A23`, `B42D`; lists are space-separated. `chains_to_design` is
comma-separated. `bias_AA` is `W:3.0,P:-2.0`; `omit_AA` is a bare letter
string `CG`.

### Output layout

```
out/
  seqs/<pdb_stem>.fa            # designed sequences, scored in the headers
  backbones/<pdb_stem>_<i>.pdb  # backbone PDB per design (+ side chains when packed)
  packed/<pdb_stem>_<i>_<p>.pdb # only when --pack_side_chains 1
```

The `seqs/<stem>.fa` FASTA: the **first** record is the input/native sequence;
each subsequent record is a design. Design record headers are comma-separated
`key=value` tokens and include `overall_confidence`, `ligand_confidence`, and
`sequence_recovery` (all in `[0,1]`, higher is better; `ligand_confidence` is
`0.0` when the model is not ligand-aware).

### Weights

Checkpoints (~1.5 GB: 4 ProteinMPNN + 4 LigandMPNN + 4 SolubleMPNN + 2
membrane + 1 side-chain packer) are install-time fetched into
`~/.fova/models/ligandmpnn/`. A missing cache means install did not complete —
the adapter validates it with `os.Stat`, never creates it.

---

## 3. Component A — bespoke `ligandMPNNTool` + typed schema

Replace the shared `designTool` wrapper for ligandmpnn with a bespoke
`ligandMPNNTool` in `internal/tools/design/ligandmpnn.go`, mirroring
`boltzGenTool` (`internal/tools/design/boltzgen.go`): a `LigandMPNNParams =
domain.LigandMPNNParams` package alias, a typed `InputSchema()` advertising
every field in §2 with enum/range constraints, an `Execute` that validates,
resolves the workspace path fields (`pdb`, `bias_AA_per_residue`,
`omit_AA_per_residue`), submits the background job, and `persist`s the designs.

`NewLigandMPNNTool(workspaceRoot, mgr, backend, st)` keeps its current
signature, so `cmd/fova/main.go` is **unchanged**. `RequiresConfirmation`
stays `true` (design jobs are long and GPU-bound). The shared `designTool`
stays for the still-generic tools.

`InputSchema()` requires `pdb`; `model_type` is an enum defaulting to
`ligand_mpnn`; bounded numerics carry `minimum`/`maximum`.

---

## 4. Component B — `/plan` integration

A new `LigandMPNNParams` struct in `internal/domain/types.go`, mirroring
`BoltzGenParams` — every agent-facing §2 field, with `*float64`/`*bool`/`*int`
pointers where "unset" must be distinguished from a zero so the flag is
omitted and `run.py` keeps its own default.

`MethodConfig` (`internal/domain/types.go`) gains a
`LigandMPNN *LigandMPNNParams` pointer field, alongside the existing
`BoltzGen`. The two are independent — no shared union.

`plan.create` (`internal/tools/plan/plan.go`): for `MethodLigandMPNN`, accept
the `method_params` object, unmarshal it into `LigandMPNNParams`, run
**preflight** (§6), and reject the plan if preflight fails — consistent with
the BoltzGen `method_spec_path` / `boltzgen_check` guard.

`/plan` rendering (`internal/tools/plan/render.go` + `internal/tui/plan.go`):
a LigandMPNN section showing the model type, the input PDB, the key
parameters, and the preflight result. `/plan approve` re-runs preflight to
catch edits.

---

## 5. Component C — adapter (`adapter_ligandmpnn.go`)

A brand-new `ToolAdapter` (`internal/backends/local/adapter_ligandmpnn.go`),
following `adapter_proteinmpnn.go` (LigandMPNN's `run.py` is the same family).
It:

- unmarshals the typed request;
- stages the input PDB and any per-residue bias/omit JSON files into the
  container workdir;
- maps the typed params to `run.py` flags via a table-driven `ligandMPNNArgs`
  (an unset pointer / empty string omits the flag);
- selects `--checkpoint_<model_type>` (and `--checkpoint_path_sc` when
  `pack_side_chains`) from `/models`;
- validates the container runtime, image, and weights cache (`os.Stat`);
- runs the container with `env.WorkDir` mounted at `/work` and the weights
  cache at `/models`;
- collects outputs (§7) into the `{"designs":[...]}` envelope.

---

## 6. Component D — preflight

Run at `plan.create` and at the top of the tool's `Execute`:

- `pdb` is set, resolves inside the workspace, exists, and is a `.pdb`;
- `model_type` is one of the five enum values;
- residue references (`fixed_residues`, `redesigned_residues`,
  `transmembrane_buried`, `transmembrane_interface`, `symmetry_residues`) are
  well-formed `<chain><number>[<icode>]` tokens;
- `chains_to_design` is a comma-separated chain list;
- `bias_AA` parses as `<AA>:<float>` pairs; `omit_AA` is one-letter codes;
- `bias_AA_per_residue` / `omit_AA_per_residue`, when set, resolve inside the
  workspace and exist;
- `global_transmembrane_label` is `0` or `1`; numeric params are in range;
- the container image and weights cache are present.

A malformed proposal is rejected before any job is submitted.

---

## 7. Component E — score ingestion

The adapter parses `out/seqs/<stem>.fa`:

- the **first** FASTA record is the input/native sequence — skipped;
- each subsequent record is one design: the sequence body becomes the
  `Design.Sequence`, and the header's comma-separated `key=value` tokens are
  read for `overall_confidence`, `ligand_confidence`, `sequence_recovery` →
  `Design.Scores`;
- the `StructureFile` is the matching `backbones/<stem>_<i>.pdb`, or the
  `packed/<stem>_<i>_1.pdb` when `pack_side_chains` was set;
- one fova `Design` per design record, persisted with `Origin
  OriginRFDiff2MPNN`, `Application AppEnzyme`.

A FASTA record whose header lacks a score yields an empty entry for that key
rather than a failure.

---

## 8. Component F — grounding skill

New `internal/assets/embed/skills/ligandmpnn-design.md` (with the YAML
frontmatter — `name`, `description` — that dev's `internal/assets` layer
requires on every embedded skill):

- the five model types and when to pick each (`ligand_mpnn` for ligand-bound
  active sites, `soluble_mpnn` for soluble globular proteins, the membrane
  models for transmembrane proteins);
- residue selection — `redesigned_residues` vs `fixed_residues` vs
  `chains_to_design`, and the `A23` / `B42D` reference format;
- `bias_AA` / `omit_AA` syntax and when to use them;
- when to enable side-chain packing;
- how to read `overall_confidence` / `ligand_confidence` / `sequence_recovery`.

---

## 9. Component G — testing

**Unit:**

- `InputSchema()` shape;
- `ligandMPNNArgs` param → `run.py`-flag mapping (table test, including unset
  pointers omitting flags and checkpoint selection per `model_type`);
- FASTA-header score parsing against a fixture `.fa` (first record skipped;
  missing-score case);
- preflight — a table of valid and invalid requests;
- `plan.create` LigandMPNN method-config handling;
- `/plan` LigandMPNN-section rendering.

**Container:** the existing install-time `smoke_test` (a `run.py` design on
the bundled `1BC8.pdb`) remains valid.

**End-to-end:** the full GPU run **is validatable on the GB10** — LigandMPNN
is pure-PyTorch (ProDy), no PyRosetta. It runs in the umbrella's batched
validation pass.

Every phase ends `go build ./...` and `go test ./...` green.

---

## 10. Phasing

1. **P1 — domain + tool.** `LigandMPNNParams` + `MethodConfig.LigandMPNN` in
   `internal/domain`; the bespoke `ligandMPNNTool` with typed schema and
   `Execute`. Build + tests green.
2. **P2 — adapter + preflight.** `adapter_ligandmpnn.go`, `ligandMPNNArgs`,
   PDB/JSON staging, the preflight. Build + unit tests green.
3. **P3 — `/plan` integration.** `plan.create` method-config handling, `/plan`
   rendering, `/plan approve` re-check.
4. **P4 — score ingestion + skill.** FASTA-header parsing, `ligandmpnn-design`
   skill, the GPU-validation checklist entry.

---

## 11. Files touched

- `internal/domain/types.go` — `LigandMPNNParams`; `MethodConfig.LigandMPNN`
- `internal/tools/design/ligandmpnn.go` — replace the `designTool` wrapper
  with the bespoke `ligandMPNNTool`
- `internal/backends/local/adapter_ligandmpnn.go` — new adapter
- `internal/tools/plan/plan.go` — `plan.create` LigandMPNN method-config
- `internal/tools/plan/render.go`, `internal/tui/plan.go` — `/plan` rendering
- `internal/assets/embed/skills/ligandmpnn-design.md` — new grounding skill
- tests alongside each
- `cmd/fova/main.go` — **unchanged** (`NewLigandMPNNTool`'s signature is kept)

---

## 12. Out of scope

- Modal-backend execution — local backend only.
- The **editable** confirmation surface — its own cross-cutting spec
  (umbrella §4).
- `--pdb_path_multi` batch mode and the `_multi` per-PDB JSON variants — fova
  designs one backbone per call.
- `--save_stats` output (logits / probabilities) — not surfaced.

---

## 13. Risks

- **FASTA header format drift.** The score keys are read by name from the
  header; an upstream rename would silently drop a score. Mitigation: the
  parser is tested against a pinned fixture and treats a missing key as "no
  score", never a failure; the fixture is cross-checked against a real
  LigandMPNN run during GPU validation.
- **`/plan` is shared surface.** `plan.go` / `render.go` / `domain/types.go`
  also carry the BoltzGen method-config. LigandMPNN only **adds** an
  independent `MethodConfig.LigandMPNN` field and an independent `plan.create`
  / render branch — no BoltzGen code is changed.
- **Weak orchestration model.** A small local model may propose malformed
  residue selections. Mitigations: the `ligandmpnn-design` skill, the
  preflight, and the `/plan` review gate are three independent backstops.
