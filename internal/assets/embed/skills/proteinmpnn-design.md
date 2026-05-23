---
name: proteinmpnn-design
description: Author correct design.proteinmpnn configurations for sequence-from-structure design
---
# Skill: Designing sequences with ProteinMPNN

## When to use
The user has a backbone (a `.pdb`) and wants new amino-acid sequences that fold
to it — inverse folding / sequence design without any ligand awareness.
`design.proteinmpnn` is the classic ProteinMPNN — fast, well-validated, and the
right default when the structure has no bound small molecule, ion, or nucleic
acid. For a ligand-bound active site, prefer `design.ligandmpnn`
(`ligandmpnn-design.md`) which conditions on the ligand atoms. For de novo
*backbone* generation, run `design.rfdiffusion` first and feed its backbones
here. `design.proteinmpnn` runs as an async GPU job — submit it, then poll
`jobs.result` for the designs.

## How many sequences — `num_designs` × `batch_size`
- `num_designs` → `--num_seq_per_target`: how many sequences per target.
  fova defaults to **1** when unset. Use 8-16 for exploration, 32-128 for a
  serious shortlist.
- `batch_size` → `--batch_size`: how many sequences sampled per GPU forward
  pass. fova defaults to **1**; bumping `batch_size` is purely a throughput
  tuning knob (it does not change the design distribution).

The total designs returned equals `num_designs` (one record per designed
sequence; the native record in each FASTA is dropped).

## Sampling temperature — `sampling_temp`
`sampling_temp` (default **0.1**) controls how peaky the per-position
distribution is:

- **0.1 (default)** — conservative, near-MAP sequences. Use for first passes
  and when you want the model's best guess.
- **0.2-0.3** — diverse but still high-quality. Use when you want several
  distinct sequences per backbone.
- **0.5+** — exploratory / noisy. Almost always overkill; only useful when the
  conservative samples all collapse on the same answer.

`seed` (default 37) is fixed for reproducibility; change it (or leave default
and re-submit) to draw a different sample.

## Chain selection — `chains_to_design`
For a multi-chain backbone, restrict design to a subset by passing a
comma-separated chain id list, e.g. `"A,B"`. fova generates the
`--chain_id_jsonl` ProteinMPNN expects.

**Trade-off**: fova writes the simpler `[[<designed>], []]` form (an empty
fixed-chains list), which means any chain not in `chains_to_design` is left to
ProteinMPNN's defaults — it is **not** explicitly held at its native sequence.
If you need other chains hard-pinned to their input identity, also pass a
`fixed_positions` JSONL covering every residue of those chains.

## Position-level control — JSONL paths
ProteinMPNN takes its position-level constraints as JSONL files. fova accepts
workspace paths in the request and stages each file into the container workdir.

- `fixed_positions` → `--fixed_positions_jsonl`: positions held at the input
  identity. One-line JSONL:
  ```json
  {"5L33": {"A": [1, 2, 3, 4, 5]}}
  ```
  The outer key is the PDB stem; the inner map keys are chain ids; the values
  are **1-indexed** residue numbers to fix.

- `bias_AA` → `--bias_AA_jsonl`: a **workspace path to a JSONL file** (not an
  inline letter:weight string — that is what LigandMPNN's `bias_AA` takes).
  This is the most common ProteinMPNN footgun. The JSONL is a one-line object:
  ```json
  {"A": 1.39, "G": -0.5}
  ```
  Positive weights encourage the residue; negative discourage. Keep biases
  small (|weight| ≤ 2) — they override the model's learned distribution.

- `bias_by_residue` → `--bias_by_res_jsonl`: per-residue per-AA bias —
  workspace path to a JSONL whose values are length-20 vectors over the 20
  amino acids. Advanced; use only when you need position-specific guidance.

- `tied_positions` → `--tied_positions_jsonl`: groups of positions sampled to
  the same amino acid (symmetric homo-oligomers, repeats). Workspace path to a
  one-line JSONL.

## Inline omissions — `omit_AAs`
Forbid amino acids everywhere with `omit_AAs` (a bare letter string, e.g.
`"CG"` to skip cysteine and glycine). Common choices:

- `"C"` — avoid stray disulfides in soluble designs.
- `"CG"` — also avoid backbone-flexible glycines in core positions.
- `"X"` — disallow the unknown-residue token.

## Score logging — `save_score`
`save_score: true` appends `--save_score 1`, writing per-design score files
alongside the sequences. The FASTA headers already carry `score`,
`global_score`, and `seq_recovery` — fova parses them either way — so leave
`save_score` off unless you want the raw `.npz` files for offline analysis.

## Reading the result scores
Each `jobs.result` record carries a `scores` map parsed from the FASTA header:

- `score` — the per-design negative log-likelihood. **Lower is better**.
- `global_score` — the whole-sequence NLL. Lower is better.
- `seq_recovery` — fraction of positions matching the input. Low recovery is
  normal for a redesign — treat it as a diversity signal, not a quality
  score.

Shortlist by `score` (lowest first) and inspect `seq_recovery` to spot
designs that collapsed back onto the native (likely overlap with the input).
A missing key just means that score is absent from the header — never an
error.

## Worked example — design sequences for an RFdiffusion backbone
The user has a fresh RFdiffusion backbone at `designs/binder/bb_0.pdb` (chains
A = binder, T = target) and wants 32 binder sequences with the target held
fixed and no cysteines:

```json
{
  "pdb": "designs/binder/bb_0.pdb",
  "num_designs": 32,
  "sampling_temp": 0.2,
  "chains_to_design": "A",
  "fixed_positions": "designs/binder/fix_target.jsonl",
  "omit_AAs": "C"
}
```

`fix_target.jsonl` lists every residue of chain T at the input identity so the
target is pinned. The 0.2 temperature gives diverse but solid samples; 32
designs fits a refold + filter pass downstream (`fold.boltz2`, then a
score/PAE filter).

A minimal call for a single-chain backbone is just:

```json
{ "pdb": "designs/scaffold.pdb", "num_designs": 16 }
```

## Stop conditions
- If the structure has a bound ligand, switch to `design.ligandmpnn` —
  ProteinMPNN ignores the ligand and will design a pocket that does not fit
  it.
- If you set `bias_AA` to a string of letters (LigandMPNN style), fix it —
  here `bias_AA` is a **workspace path to a JSONL file**.
- If no `.pdb` backbone is available, ask the user for one — do not invent a
  path.
