---
name: rfdiffusion2-design
description: Author correct design.rfdiffusion2 configurations for atom-level catalytic-motif scaffolding
---
# Skill: Scaffolding enzyme active sites with RFdiffusion2

## When to use
The user wants a de novo enzyme backbone scaffolded **around a catalytic motif**
(a theozyme, a metal-binding triad, a bound transition-state analogue) at atom
resolution. `design.rfdiffusion2` is atom-level flow-matching scaffolding — it
sees the motif's individual atoms, not just residue indices. Versus
`design.rfdiffusion` (per-residue, backbone only, no inline sequence/fold step),
RFdiffusion2 runs a **single Hydra-driven pipeline** (`pipeline.py`) that does
backbone diffusion → PyRosetta idealization → inline LigandMPNN sequence fitting
→ inline Chai-1 fold → metrics emission. For non-catalytic *binders* use
`design.rfdiffusion`; for plain inverse folding use `design.ligandmpnn`. Runs as
an async GPU job — submit it, poll `jobs.result` for the scored complexes.

## Benchmark — pick a bundled sweep with `benchmark`
`benchmark` is an enum, default `active_site_demo`.

| `benchmark` | Use for | Coverage |
|---|---|---|
| `active_site_demo` (DEFAULT) | Exploratory active-site scaffolding — the open-source active-site demo over unindexed atomic partial-ligand motifs | Broad; the right starting point |
| `enzyme_bench_n41` | Benchmarking against the curated AME-41 enzyme reference set | Narrower; validated against the 41 reference enzymes |

Pick `enzyme_bench_n41` when reproducing the AME-41 numbers. For real
catalytic-motif scaffolding work, leave it on `active_site_demo` and supply a
user motif via `motif_pdb` + `contigs` (next section).

## Supplying a user catalytic motif — `motif_pdb` + `contigs`
To override the benchmark's bundled motif with your own, set both:

- `motif_pdb` — workspace path to a `.pdb` containing **the catalytic residues
  you want scaffolded** (a single chain, ideally PyRosetta-idealized so the
  geometry is clean). The adapter stages it into the container.
- `contigs` — a Hydra contig string. Syntax is comma-separated tokens of the
  shape `<flanker_length>,<chain><start>-<end>,<flanker_length>`. The
  `<chain><start>-<end>` token is the **fixed motif region** (the residues
  copied verbatim from `motif_pdb`); the bare-length tokens are **flanker
  ranges** the diffuser fills in around it.

Example: `"5-15,A10-30,5-15"` means *5-15 residues of flanker, A10-A30 from the
motif PDB held fixed, 5-15 more residues of flanker*. Multiple motif regions
chain: `"5-10,A10-12,5-10,A45-47,5-10"`.

`contigs` is **required** when `motif_pdb` is set (the override is meaningless
without it). Preflight rejects `motif_pdb` alone.

## `guidepost_xyz_as_design_bb` — leave off for indexed motifs
`*bool`, defaults to the upstream default (off for normal indexed motifs).
**Leave it off** for the standard case: a motif PDB with residue indices that
the contigs reference (A10-A30 etc.). **Turn it on** only when supplying
unindexed motif XYZ coordinates that should overwrite the matched backbone
positions directly — a rare, advanced workflow. If unsure, leave it unset.

## `idealize_sidechain_outputs` — leave on for production
`*bool`, default on. The PyRosetta idealization pass post-diffusion produces a
clean motif geometry that downstream LigandMPNN scoring handles much better.
**Leave it on** for production runs. **Turn it off** only to shave time off
exploratory sweeps where you do not care about the LigandMPNN/Chai-1 scores.

## `stop_step` — full pipeline vs backbone-only
`stop_step` is an enum, default `end`.

- `end` (DEFAULT) — full pipeline through Chai-1 fold and metrics. Scored
  designs out the other side. **Right choice ~95% of the time.**
- `design` — stop after backbone diffusion + idealization. Useful when you want
  to chain `design.ligandmpnn` + a separate `fold.chai1` (or `fold.boltz2`)
  yourself, or when iterating on the motif geometry and you do not need
  sequences yet.

## Reading the scores
Each `jobs.result` record carries a `scores` map populated from the
pipeline's metrics CSV. The headline keys:

- `motif_rmsd` — design-vs-reference motif-atom RMSD in Å, **lower is better**.
  Filter at `motif_rmsd < 1.0` for a real scaffold, `< 0.5` for a tight one.
- `idealized_residue_rmsd` — gap between the diffused backbone and PyRosetta's
  idealized output. High values mean idealization could not clean the motif —
  inspect those designs before trusting them.
- `motif_ideality_diff` — motif-atom ideality. Lower is better.
- Chai-1 confidence columns (pLDDT-like) are present when `stop_step='end'`
  and carried through under their CSV header names (lower-cased).

Shortlist by `motif_rmsd < 1.0`, then rank survivors by the Chai-1 confidence.
A design with no score row simply has an empty `scores` map — not an error.

## First-run cost
When `stop_step='end'` (the default), the inline Chai-1 fold step downloads
`chai_lab`'s weights from `chaiassets.com` **on first invocation inside the
container** (~1.3 GB). Subsequent runs reuse the in-container cache. (The
RFD diffusion + LigandMPNN weights are pre-staged into `/models/rfdiffusion2/`
by the recipe — that download does not happen at run time.)

## Worked example — scaffold a catalytic triad
The user has a 3-residue catalytic triad in `designs/triad/motif.pdb` (chain A,
residues 10-12) and wants a small batch of scaffolded backbones with full
sequences and folds:

```json
{
  "benchmark": "active_site_demo",
  "motif_pdb": "designs/triad/motif.pdb",
  "contigs": "5-15,A10-12,5-15",
  "num_designs": 16,
  "seed": 7,
  "idealize_sidechain_outputs": true
}
```

`stop_step` is omitted, so the full pipeline runs (backbone → idealize →
LigandMPNN → Chai-1). Filter the results downstream with
`{ "scores.motif_rmsd": { "<": 0.5 } }` for tight scaffolds.

A minimal exploratory call with no user motif is just:

```json
{ "num_designs": 8 }
```

— that runs the bundled `active_site_demo` sweep end-to-end.

## Platform note
RFdiffusion2 is **x86-only**. It will not run on the GB10 (no Python-3.12
aarch64 PyRosetta wheel). For aarch64 work, fall back to `design.rfdiffusion`
(per-residue scaffolding) + `design.proteinmpnn` (or `design.ligandmpnn` if a
ligand is bound) and a separate fold tool.

## Stop conditions
- If `motif_pdb` is set without `contigs`, ask the user for the contig string —
  preflight rejects the combination.
- If `contigs` references a chain/range not in the motif PDB, the run will fail
  inside the container; verify the chain ID and residue numbering match the
  PDB before submitting.
- If no catalytic motif is available, use the bundled `active_site_demo`
  benchmark for an exploratory run, or ask the user for the motif — do not
  invent a PDB path.
