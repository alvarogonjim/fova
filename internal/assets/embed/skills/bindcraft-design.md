---
name: bindcraft-design
description: Author correct design.bindcraft configurations for de novo protein binder campaigns
---
# Skill: Designing binders with BindCraft

## When to use
The user wants a de novo protein binder — a small, non-antibody binder — that
docks against a specific epitope on a target. `design.bindcraft` runs the
BindCraft pipeline (RFdiffusion-style hallucination + AlphaFold2-guided
filtering + PyRosetta refinement) and returns the accepted binder structures
with per-design scores. It is the primary non-antibody binder method; for
antibodies use `design.rfantibody`, and for general binder strategy see
`design-binder.md`. `design.bindcraft` runs as an async GPU job — submit it,
then poll `jobs.result` for the accepted designs.

## Platform constraint — x86 only
BindCraft depends on **PyRosetta**, which is only built for x86_64. The
Containerfile fails on aarch64 (and on Apple Silicon under Linux VMs). If the
user is on an arm64 host (incl. NVIDIA GB10 dev kits), do not propose this
tool — recommend `design.rfantibody` or `design.rfdiffusion` + a downstream
sequence-design step instead.

## The five required fields
Every `design.bindcraft` call must set these — the schema rejects calls that
omit any of them:

| Field | What it is |
|---|---|
| `starting_pdb` | Workspace path to the target structure (`.pdb`). |
| `chains` | Target chain id(s) in `starting_pdb`, e.g. `"A"` or `"A,B"`. |
| `target_hotspot_residues` | Comma-separated epitope residues as chain+number, e.g. `"A30,A33,A47"`. |
| `length_min` | Minimum binder length in residues (≥1). |
| `length_max` | Maximum binder length in residues (≥`length_min`). |

`length_min`/`length_max` define the range BindCraft samples per design — a
useful starting span is **`length_min: 60, length_max: 150`** for a small
binder, or `80`–`120` if the pocket constrains the geometry tighter.

## Hotspot selection — the single highest-leverage choice
BindCraft, like RFantibody, is very sensitive to hotspot choice. A bad
epitope is the most common cause of a failed campaign. Guidance:

- Pick **more than ~3 residues**, biased toward **hydrophobic** side chains —
  hydrophobic contacts anchor the interface.
- Leave **~10 Å of target context** around the hotspots so the binder has a
  real surface to fold against; do not pick a single isolated residue.
- **Avoid glycosylated sites and dense charged/polar patches** — N-glycans
  and high net charge yield non-reproducible interfaces.
- Use the target's author numbering as it appears in the target PDB.

The preflight rejects malformed tokens (must match `<chain><number>`).

## Optional knobs
- `binder_chain` (default `"B"`) — chain id assigned to the designed binder
  in the output PDBs. Override only if your downstream pipeline expects
  another id.
- `number_of_final_designs` — target count of accepted final designs.
  BindCraft keeps running until it hits this number or exhausts
  `design_runs`. For exploration leave it small (5-10); for a real campaign
  raise it to a few hundred.
- `design_runs` — upper bound on total BindCraft design trajectories. Acts
  as a budget cap; omit it to let BindCraft choose.
- `protocol_name` — enum, optional. Pick one when you need to constrain the
  search space:

  | `protocol_name` | Use for |
  |---|---|
  | `beta_only` | β-sheet biased binders — flat, sheet-rich interfaces. |
  | `ss_only` | Secondary-structure constrained — limits to canonical SS. |
  | `fixed_seq` | Sequence-pinned — keep a known motif at its native sequence. |

  Omit `protocol_name` to use BindCraft's default (mixed protocol).
- `template_pdb` — workspace path to a binder template PDB to seed the
  trajectory. Use it to bias designs toward a known scaffold (e.g. a four-
  helix bundle); omit it for fully de novo runs.
- `omit_aas` — one-letter codes to forbid in the binder, e.g. `"C"` to
  avoid cysteines (which is a common choice to skip unwanted disulfides).
- `binder_name` — a short prefix used in BindCraft's output filenames.
  Cosmetic; safe to omit.

## What gets executed
fova compiles the typed params into BindCraft's `settings.json` (zero-value
fields are omitted so BindCraft applies its own defaults — there is **no
opaque settings blob** to author by hand), runs `bindcraft.py --settings
settings.json` inside the BindCraft venv, and parses the
`Accepted/<design>.pdb` outputs.

## Reading the result scores
Each `jobs.result` record carries a `scores` map sourced from
`final_design_stats.csv`. The keys depend on which BindCraft protocol ran,
but typical entries include:

- `Average_pLDDT` — AlphaFold2 confidence over the binder, in `[0,1]` (or
  `[0,100]` in older versions). Higher is better; filter at `> 0.85`.
- `Average_i_pTM` — interface pTM, `[0,1]`. Higher is better; the BindCraft
  authors flag `> 0.65` as a strong interface.
- Per-design Rosetta scores (`dG_separated`, `ddG`, `sap_score`, etc.) are
  carried through verbatim under their CSV header names. Lower
  `dG_separated`/`ddG` is better (more favourable binding); lower
  `sap_score` is better (less surface-aggregation propensity).

Shortlist by `Average_i_pTM > 0.65` and `Average_pLDDT > 0.85`, then sort the
survivors by `dG_separated`. A design without a particular score key just has
that key missing — not an error.

## Worked example — a binder against PD-L1
The user wants a small de novo binder against the PD-1-binding face of
PD-L1, with the target structure at `designs/pdl1/target.pdb` (chain A
contains PD-L1, the canonical PD-1 contact residues are A54, A56, A66, A115
— a shallow hydrophobic patch). A focused exploratory run with the β-only
protocol:

```json
{
  "binder_name": "PDL1_binder",
  "starting_pdb": "designs/pdl1/target.pdb",
  "chains": "A",
  "target_hotspot_residues": "A54,A56,A66,A115",
  "length_min": 80,
  "length_max": 120,
  "number_of_final_designs": 10,
  "binder_chain": "B",
  "protocol_name": "beta_only",
  "omit_aas": "C"
}
```

Four hotspots biased toward hydrophobics, surrounding target context kept
intact, a tight `[80,120]` length range, `beta_only` to bias toward a sheet
interface, and `omit_aas: "C"` to skip cysteines. For a fully de novo
exploratory pass drop `protocol_name`:

```json
{
  "starting_pdb": "designs/pdl1/target.pdb",
  "chains": "A",
  "target_hotspot_residues": "A54,A56,A66,A115",
  "length_min": 60,
  "length_max": 150
}
```

## Stop conditions
- If the host is arm64 (incl. GB10), do not run BindCraft — recommend
  `design.rfantibody` (antibodies) or `design.rfdiffusion` (general
  backbones) instead.
- If `target_hotspot_residues` is fewer than ~3 residues, sits on a
  glycosylated/charged patch, or is a single isolated residue, revise the
  epitope before submitting — do not run a doomed campaign.
- If `length_max < length_min` or any length is `<1`, fix them — preflight
  rejects them.
- If no target structure is available, ask the user for one — do not invent
  a path.
