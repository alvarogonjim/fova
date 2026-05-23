# `design.{rfdiffusion,proteinmpnn,bindcraft}` — schema-expansion trio

**Date:** 2026-05-23
**Branch:** `feat/design-trio` (off `dev`)
**Umbrella:** `docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md`
**Tools:** the three remaining schema-expansion follow-ups the umbrella
explicitly scoped out — `design.rfdiffusion`, `design.proteinmpnn`,
`design.bindcraft`.

## Goal

Bring all three tools to bespoke-tool parity with the new pattern already
shipped for `boltzGenTool` / `ligandMPNNTool` / `rfantibodyTool`: typed
full-surface schema, `/plan` `MethodConfig` integration, grounding skill. The
adapters are reworked to consume the typed params; existing score ingestion
(BindCraft's CSV parser, ProteinMPNN's FASTA-header parser) is preserved.

## Branch & dev-merge coordination

Branched off `dev` at the same commit as parallel session B
(`feat/rfdiffusion2`). Both touch `internal/domain/types.go` (`MethodConfig`),
`internal/tools/plan/plan.go`, `internal/tools/plan/render.go`. **This branch
rebases onto post-B `dev` before final merge.** Conflicts are additive (B
adds one `MethodConfig` field + one dispatch case; we add three) and
mechanically resolvable. The `internal/assets/assets_test.go` skill-count
assertion is bumped at the integration commit (+3 here; we may also rebase
that bump after B lands its +1).

---

## 1. Background — current state per tool

### `design.rfdiffusion`

- Tool: shared `designTool` (`internal/tools/design/rfdiffusion.go`), origin
  `OriginRFDiffMPNN`, application `AppBinder`, generic schema
  (`target/hotspots/contigs/num_designs`).
- Adapter (`internal/backends/local/adapter_rfdiffusion.go`): translates the
  request into Hydra overrides
  (`inference.input_pdb=…`, `'contigmap.contigs=[…]'`,
  `'ppi.hotspot_res=[…]'`, `inference.num_designs=N`,
  `inference.ckpt_override_path=…`). Container-mode + a preserved legacy
  venv-mode fallback. Auto-picks Base vs Complex_base ckpt by target presence.
- Score ingestion: **none** — `parseRFdiffusionOutput` returns designs with
  empty `Sequence` and empty `Scores`. RFdiffusion v1 only emits backbones.
- Unreached upstream surface: `inference.deterministic`, `inference.symmetric`
  + `symmetry.*`, `diffuser.partial_T`, `diffuser.noise_scale_ca/frame`,
  `potentials.guiding_potentials` + `potentials.guide_scale`.

### `design.proteinmpnn`

- Tool: shared `designTool` (`internal/tools/design/proteinmpnn.go`), origin
  `OriginRFDiffMPNN`, application `AppBinder`, generic schema
  (`target/num_designs`).
- Adapter (`internal/backends/local/adapter_proteinmpnn.go`): writes a driver
  script that runs `helper_scripts/parse_multiple_chains.py` then
  `protein_mpnn_run.py --num_seq_per_target N --sampling_temp 0.1 --seed 37
  --batch_size 1`. The `sampling_temp` / `seed` / `batch_size` are
  **hardcoded**.
- Score ingestion: **already done** — `parseProteinMPNNOutput` reads
  `seqs/*.fa`, skips record 0 (native), and pulls `score` / `global_score` /
  `seq_recovery` from the headers.
- Unreached: `--chains_to_design`, `--fixed_positions_jsonl`, `--omit_AAs`,
  `--bias_AA_jsonl`, `--bias_by_res_jsonl`, `--tied_positions_jsonl`,
  `--save_score`, and the hardcoded sampling knobs.

### `design.bindcraft`

- Tool: shared `designTool` (`internal/tools/design/bindcraft.go`), origin
  `OriginBindCraft`, application `AppBinder`. Generic schema with an opaque
  `settings` `map[string]any` pass-through to BindCraft's target-settings JSON.
- Adapter (`internal/backends/local/adapter_bindcraft.go`): writes the
  settings JSON, runs the BindCraft pipeline, parses
  `final_design_stats.csv` into per-design sequence + scores.
- Score ingestion: **already done** (CSV parser).
- **x86-only** (PyRosetta — same constraint as rfantibody/rfdiffusion2; the
  Containerfile fails on aarch64).
- Unreached: the `settings` JSON shape is currently opaque — the agent has no
  schema-level visibility into BindCraft's fields, preflight can't validate
  them, and `/plan` can't render them.

---

## 2. Per-tool upstream reference (for the implementer)

### RFdiffusion v1 — Hydra `key=value` overrides

Run via `python /opt/rfdiffusion/scripts/run_inference.py <overrides>`. The
adapter already wires the v1 surface; this spec exposes the rest:

| schema field | Hydra path |
|---|---|
| `target` | `inference.input_pdb` |
| `hotspots` | `ppi.hotspot_res` (`[A30,A33]`) |
| `contigs` | `contigmap.contigs` (`[…]`) |
| `num_designs` | `inference.num_designs` |
| `deterministic` | `inference.deterministic` |
| `symmetric` | `inference.symmetric` |
| `symmetry_kind` | `symmetry.symmetry_kind` (`cyclic`/`dihedral`/`tetrahedral`/`octahedral`/`icosahedral`) |
| `n_chains` | `symmetry.n_chains` |
| `partial_t` | `diffuser.partial_T` (partial-diffusion start step) |
| `noise_scale_ca` | `diffuser.noise_scale_ca` |
| `noise_scale_frame` | `diffuser.noise_scale_frame` |
| `guiding_potentials` | `potentials.guiding_potentials` (list of potential names) |
| `guide_scale` | `potentials.guide_scale` |

fova owns: `inference.output_prefix`, `inference.ckpt_override_path` (still
auto-picked from `target` presence — Base if absent, Complex_base if present).

### ProteinMPNN — `protein_mpnn_run.py` flags

Run via the existing driver script (parse → infer). The fova-facing flags:

| schema field | flag |
|---|---|
| `pdb` (was `target`) | input PDB — staged into `inputs/`, parsed via `parse_multiple_chains.py` |
| `num_designs` | `--num_seq_per_target` |
| `batch_size` | `--batch_size` |
| `sampling_temp` | `--sampling_temp` |
| `seed` | `--seed` |
| `chains_to_design` | `--chain_id_jsonl` (a generated JSONL listing designed/fixed chains) |
| `fixed_positions` | `--fixed_positions_jsonl` (workspace path to a user JSONL) |
| `omit_AAs` | `--omit_AAs` (inline letters, e.g. `"CG"`) |
| `bias_AA` | `--bias_AA_jsonl` (workspace path to a user JSONL — **upstream is file-based, unlike LigandMPNN's inline string**) |
| `bias_by_residue` | `--bias_by_res_jsonl` (workspace path) |
| `tied_positions` | `--tied_positions_jsonl` (workspace path) |
| `save_score` | `--save_score 1` (writes `scores/*.npz`) |

fova owns: `--jsonl_path` (the parse step's output), `--out_folder`. The
existing FASTA-header score ingestion is preserved.

### BindCraft — target-settings JSON

fova compiles a `settings.json` from the typed `BindCraftParams`:

```json
{
  "binder_name": "<BinderName>",
  "starting_pdb": "<staged StartingPDB>",
  "chains": "<Chains>",
  "target_hotspot_residues": "<TargetHotspotResidues>",
  "lengths": [<LengthMin>, <LengthMax>],
  "number_of_final_designs": <N>,
  "binder_chain": "<BinderChain or \"B\">",
  "design_runs": <DesignRuns>,
  "protocol_name": "<ProtocolName>",
  "template_pdb": "<staged TemplatePDB>",
  "omit_AAs": "<OmitAAs>"
}
```

Fields with zero / empty values are omitted from the compiled JSON so
BindCraft applies its own defaults. fova never advertises the opaque
`settings` blob to the agent again.

---

## 3. Component A per tool — bespoke tool + typed schema

Mirror the established pattern (`ligandMPNNTool`, `rfantibodyTool`): a
package-local alias (`type XParams = domain.XParams`), an `xTool` struct
(`workspaceRoot`/`mgr`/`backend`/`store`), `NewXTool(workspace, mgr, backend,
st) *xTool` **with the signature unchanged** (`cmd/fova/main.go` untouched),
and the full `tools.Tool` interface with a typed `InputSchema()`.

`Execute` does: unmarshal → `Validate()` → resolve workspace paths → re-marshal
→ submit job → `persist`. `persist` keeps each tool's existing `Origin` /
`Application` tags (`OriginRFDiffMPNN`/`AppBinder` for rfdiffusion+proteinmpnn;
`OriginBindCraft`/`AppBinder` for bindcraft).

`RequiresConfirmation` stays `true` (design jobs are long, GPU-bound).

---

## 4. Component B per tool — adapter rework

Each adapter is updated to consume `domain.XParams` (import + own unmarshal
target) and to drive its existing tool invocation with the new fields:

- **RFdiffusion adapter:** existing Hydra-override builder gets new override
  lines for the unreached fields (potentials, symmetry, partial-diffusion,
  noise, deterministic). Container-mode and venv-mode paths kept; ckpt
  auto-pick kept.
- **ProteinMPNN adapter:** the driver-script builder is parameterised on the
  typed request — `sampling_temp` / `seed` / `batch_size` taken from
  `ProteinMPNNParams` (fova-owned defaults when unset); the new
  per-residue/chain JSONL files are staged into the workdir and referenced;
  `--save_score 1` added when requested.
- **BindCraft adapter:** the opaque-`settings`-map pass-through is replaced
  with a typed JSON compiler that builds `settings.json` from
  `BindCraftParams` (omitting zero-value fields so BindCraft defaults apply).
  The existing CSV score parser is unchanged.

All three preserve their existing weights-cache + container-runtime guards.

---

## 5. Component C — `/plan` integration (coordinator Foundation)

Foundation adds, for each tool, `applyXMethodConfig` in `plan.go` (mirrors
`applyLigandMPNNMethodConfig` / `applyRFantibodyMethodConfig` exactly) and a
`MethodX` branch in `plan.create`. `render.go` gains a `renderXSection` for
each tool and a new case in the `MethodConfig` render dispatch (alongside the
existing `BoltzGen` / `LigandMPNN` / `RFantibody` cases).

`MethodRFdiffusion`, `MethodProteinMPNN`, `MethodBindCraft` and their compat
entries / aliases already exist in `compat.go` — no compat change is needed.

---

## 6. Component D per tool — preflight (`XParams.Validate()`)

Value-shape validation only (no filesystem; path-existence checks live in
each tool's `Execute` and adapter's `Invoke`).

- **`RFdiffusionParams.Validate`:** `contigs` non-empty; hotspot/contigs token
  shape; `num_designs ≥ 0`; `symmetry_kind` in the enum when set; `n_chains > 0`
  if symmetric; `partial_t ≥ 0`; positive numerics; `guiding_potentials`
  entries non-empty.
- **`ProteinMPNNParams.Validate`:** `pdb` non-empty; `num_designs`/`batch_size`
  non-negative; `sampling_temp > 0` when set; `omit_AAs` letters only; the
  JSONL-path fields, when set, are non-empty (existence is checked in
  Execute).
- **`BindCraftParams.Validate`:** `starting_pdb`, `chains`,
  `target_hotspot_residues` non-empty; hotspot tokens
  `^[A-Za-z][0-9]+$` (chain+residue); `length_min ≥ 1`,
  `length_max ≥ length_min`; `number_of_final_designs ≥ 0`; `protocol_name`
  in the BindCraft protocol enum when set.

---

## 7. Component E — score ingestion (no change)

- ProteinMPNN: FASTA-header parser stays.
- BindCraft: `final_design_stats.csv` parser stays.
- RFdiffusion: no native scores — left empty; the agent runs a refold
  (`fold.boltz2` / `fold.chai1` / similar) on the backbones to score them.
  Out of scope for this spec.

---

## 8. Component F — grounding skills

Three new skills under `internal/assets/embed/skills/`, each ~90–120 lines
with YAML frontmatter, mirroring `ligandmpnn-design.md` / `rfantibody-design.md`:

- `rfdiffusion-design.md` — contigs syntax, hotspots, ckpt auto-pick, when to
  use potentials/symmetry/partial-diffusion; how to refold for scoring.
- `proteinmpnn-design.md` — chains_to_design, fixed_positions /
  bias_AA_jsonl (file format, with one small example),
  `sampling_temp` choice, reading the FASTA-header scores.
- `bindcraft-design.md` — starting_pdb + chains + hotspot selection (>~3
  hydrophobic residues, ~10 Å context), length range, the typed settings
  fields (no more opaque blob).

---

## 9. Testing

- **Unit (per tool):** schema; param mapping (Hydra-override / driver-script /
  settings-JSON); `Validate` (table tests); the existing score parsers stay
  covered by their existing tests (no regression).
- **Foundation tests:** `plan.go` method-config handling for each tool;
  `render.go` per-tool render sections.
- **`design_test.go`:** the existing `TestDesignToolSchemaAdvertisesSettings`
  asserts an opaque `settings` property on BindCraft's schema — once bindcraft
  is bespoke that property is gone, replaced by typed fields. The test is
  **updated** to assert the new typed fields (or removed if the per-tool
  schema test in `bindcraft_test.go` covers it).
- **End-to-end:** RFdiffusion + ProteinMPNN are aarch64-compatible →
  GB10-validatable in the umbrella's batched validation pass. **BindCraft is
  x86-only** → not GB10-validatable; CI layers cover it.

Every phase ends `go build ./...` and `go test ./...` green.

---

## 10. Phasing

1. **Foundation (coordinator), one or two commits:**
   - Commit 1: domain — 3 new files (`rfdiffusion.go`, `proteinmpnn.go`,
     `bindcraft.go`) with `XParams` + `Validate`; `MethodConfig` updated in
     `types.go`; per-file tests.
   - Commit 2: `/plan` integration — three `applyXMethodConfig` in `plan.go`,
     three `renderXSection` in `render.go`, dispatch cases, tests; the
     `design_test.go` `TestDesignToolSchemaAdvertisesSettings` update.
2. **Three parallel Opus agents** — one per tool (tool file + adapter +
   skill).
3. **Integration:** merge the three streams; bump
   `internal/assets/assets_test.go` skill count by 3; build + full suite +
   gofmt green.
4. **Wait for `feat/rfdiffusion2` to merge into `dev`,** then rebase this
   branch onto post-B `dev` (additive conflicts on `MethodConfig` / `plan.go`
   / `render.go`).
5. **Merge into `dev`** (offer to the user, the established pattern).

---

## 11. Files touched

**Foundation:**
- `internal/domain/types.go` — `MethodConfig` gains three pointer fields.
- `internal/domain/rfdiffusion.go`, `proteinmpnn.go`, `bindcraft.go` — new
  (each with the typed struct + `Validate`).
- `internal/domain/{rfdiffusion,proteinmpnn,bindcraft}_test.go` — new.
- `internal/tools/plan/plan.go`, `plan_test.go` — three `applyXMethodConfig`
  + dispatch + tests.
- `internal/tools/plan/render.go`, `render_test.go` — three `renderXSection`
  + dispatch + tests.
- `internal/tools/design/design_test.go` — `TestDesignToolSchemaAdvertisesSettings`
  update.

**Streams:**
- A — `internal/tools/design/rfdiffusion.go`,
  `internal/tools/design/rfdiffusion_test.go` (new),
  `internal/backends/local/adapter_rfdiffusion.go`,
  `internal/backends/local/adapter_rfdiffusion_test.go`,
  `internal/assets/embed/skills/rfdiffusion-design.md` (new).
- B — `internal/tools/design/proteinmpnn.go`,
  `internal/tools/design/proteinmpnn_test.go` (new),
  `internal/backends/local/adapter_proteinmpnn.go`,
  `internal/backends/local/adapter_proteinmpnn_test.go`,
  `internal/assets/embed/skills/proteinmpnn-design.md` (new).
- C — `internal/tools/design/bindcraft.go`,
  `internal/tools/design/bindcraft_test.go` (new),
  `internal/backends/local/adapter_bindcraft.go`,
  `internal/backends/local/adapter_bindcraft_test.go`,
  `internal/assets/embed/skills/bindcraft-design.md` (new).

**Integration:**
- `internal/assets/assets_test.go` — skill-count bump (+3 vs the post-rebase
  baseline).

`cmd/fova/main.go` — **unchanged** (every `NewXTool` signature stays).

---

## 12. Out of scope

- Modal-backend execution.
- The editable confirmation surface (parallel session C is owning it).
- A new score parser for RFdiffusion v1 (no native scores).
- The rfdiffusion legacy venv-mode path — kept as-is.
- Re-introducing chai2 in any form.

---

## 13. Risks

- **B-vs-D conflict at dev merge.** Both branches add to `MethodConfig`,
  `plan.go`, `render.go`. Resolved by rebasing this branch after B lands.
  Additive only — the rebase is mechanical.
- **BindCraft is x86-only.** Not GB10-validatable. CI-verifiable in full.
- **ProteinMPNN bias-AA file vs inline.** Upstream uses `--bias_AA_jsonl`
  (file), unlike LigandMPNN's inline `bias_AA "W:3.0,P:-2.0"`. The schema
  field `bias_AA` is a **workspace path to a JSONL**, not an inline string —
  the grounding skill makes that explicit so the agent doesn't confuse the
  two tools.
- **`TestDesignToolSchemaAdvertisesSettings`.** This shared-`designTool` test
  expects an opaque `settings` field on BindCraft's schema. After bespoke
  BindCraft, that field is gone. The Foundation updates the test to assert
  the new typed fields, or removes it (the per-tool schema test covers the
  same intent).
