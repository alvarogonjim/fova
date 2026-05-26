---
name: rfantibody-design
description: Author correct design.rfantibody configurations for de novo antibody CDR design
---
# Skill: Designing antibodies with RFantibody

## When to use
The user wants a de novo antibody — a VHH (nanobody) or an scFv — whose CDR
loops are diffused to contact a specific epitope on a target. `design.rfantibody`
is RFdiffusion fine-tuned for antibody CDR loops; it is the primary antibody
method. For non-antibody binders use `design-binder.md`; for general antibody
campaign strategy see `design-antibody.md`. `design.rfantibody` runs as an async
GPU job — submit it, then poll `jobs.result` for the predicted complexes.

## Framework — pick a scaffold with `framework`
The antibody framework holds the CDR loops; only the loops are designed.
`framework` is an enum, default `nanobody`.

| `framework` | Use for | Bundled scaffold |
|---|---|---|
| `nanobody` (DEFAULT) | A single-domain VHH — three CDRs (H1, H2, H3), small, stable, good for buried/cryptic epitopes | `h-NbBCII10.pdb` |
| `scfv` | A paired VH+VL fragment — six CDRs (H1-H3, L1-L3), more interface area for flat or extended epitopes | `hu-4D5-8_Fv.pdb` |

To use your own framework, set `framework_pdb` to a workspace path. It must be
an **HLT-formatted PDB** (heavy chain = `H`, light chain = `L`, target = `T`,
with the CDR loops marked). `framework_pdb` overrides `framework` — set one or
the other, not both. Use it for a humanised or germline scaffold the bundled
ones do not cover.

## Hotspots — the epitope you design against
`hotspots` is a comma-separated list of target residues, each `<chain><number>`
(e.g. `"T305,T456,T473"`). The CDR loops are diffused to contact these. **RFantibody
is very sensitive to hotspot choice** — a bad epitope is the most common cause of
a failed campaign. Guidance from the RFantibody README:

- Pick **more than ~3 residues**, biased toward **hydrophobic** side chains —
  hydrophobic contacts anchor the interface.
- Leave **~10 Å of target context** around the hotspots so the loops have a real
  surface to fold against; do not pick a single isolated residue.
- **Avoid glycosylated sites and charged/polar patches** — N-glycans and dense
  charge make poor, non-reproducible interfaces.
- Use the target's author numbering as it appears in the target PDB.

## design_loops — per-CDR length specs
`design_loops` is optional; omit it to use each framework's native CDR lengths.
When set, it is comma-separated `<CDR>:<spec>` tokens where `<CDR>` is one of
`H1 H2 H3 L1 L2 L3` and `<spec>` is either a fixed integer or a `<min>-<max>`
range sampled per design:

- `H1:7` — H1 is fixed at 7 residues.
- `H3:5-13` — H3 length is sampled in `[5,13]` per design.
- **Widen H3** (e.g. `H3:9-15`) for **deep pockets / cryptic epitopes** — a long
  H3 reaches into the cavity. Keep H3 shorter for flat epitopes.
- Only name the CDRs you want to constrain; unnamed CDRs keep their native length.
- An scFv may constrain `L1 L2 L3`; a nanobody only has `H1 H2 H3`.

## num_designs — campaign sizing
`num_designs` is the count of RFdiffusion backbones. Antibody hit rates are low,
so size generously: a few hundred for an exploratory pass, **a few thousand
(~1000-5000)** for a real campaign. Each backbone is then sequence-designed
`seqs_per_struct` times (default the stage default), so total predictions ≈
`num_designs × seqs_per_struct`.

## The 3-stage pipeline
One `design.rfantibody` job runs three stages in sequence:

1. **rfdiffusion** — diffuses `num_designs` antibody backbones with CDR loops
   contacting the `hotspots`.
2. **proteinmpnn** — designs `seqs_per_struct` sequences onto each backbone
   (`temperature` controls diversity; `deterministic` for a fixed run).
3. **rf2** — RoseTTAFold2 re-predicts each antibody-antigen complex and scores
   it (`num_recycles`, `seed`, `hotspot_show_prop` tune this stage).

The job extracts the predicted complex PDBs and a `scores.tsv`; `jobs.result`
returns one record per design.

## Reading the result scores
Each `jobs.result` record carries a `scores` map from the RF2 stage:

- `plddt` — RF2 confidence, **0-100, higher is better**. Filter at `plddt > 85`.
- `pae` — predicted aligned error in **Å, lower is better**. `pae < 10` from the
  RF2 stage is a useful interface-quality filter; it correlates with real
  binding far better than AlphaFold2 on antibody complexes.

Shortlist by `pae < 10` and `plddt > 85`, then rank the survivors. A design with
no score row simply has an empty `scores` map — that is not an error.

## Worked example — a nanobody against a target epitope
The user wants a nanobody against a hydrophobic epitope on chain T of a target
in `designs/cd47/target.pdb`, with the epitope at residues T305, T456, T473,
T475 (a shallow groove). A campaign-sized run, default framework:

```json
{
  "target": "designs/cd47/target.pdb",
  "hotspots": "T305,T456,T473,T475",
  "framework": "nanobody",
  "design_loops": "H1:7,H2:6,H3:5-13",
  "num_designs": 2000,
  "seqs_per_struct": 8,
  "temperature": 0.2
}
```

Default `nanobody` framework; four hydrophobic hotspots with surrounding context;
H1/H2 fixed and H3 sampled over its native range; 2000 backbones × 8 sequences
for the campaign. For a **deep pocket** widen H3 (`"H1:7,H2:6,H3:9-15"`).

A minimal exploratory call is just the target and epitope:

```json
{ "target": "designs/cd47/target.pdb", "hotspots": "T305,T456,T473,T475" }
```

## Stop conditions
- If `hotspots` is fewer than ~3 residues, or sits on a glycosylated/charged
  patch, revise the epitope before submitting — do not run a doomed campaign.
- If `design_loops` tokens are malformed (`<CDR>:<spec>` with `<CDR>` in
  `H1 H2 H3 L1 L2 L3`) or name an L-CDR for a nanobody, fix them — preflight
  rejects them.
- If no target structure is available, ask the user for one — do not invent a
  path.
