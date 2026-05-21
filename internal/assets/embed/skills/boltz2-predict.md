---
name: boltz2-predict
description: Predict biomolecular complex structures with Boltz-2 (fold.boltz2)
---
# Skill: Predicting structures with Boltz-2

## When to use
The user wants the 3D structure of a biomolecular complex from sequence —
a protein monomer, a multi-chain protein complex, a protein–nucleic-acid
assembly, or a protein–ligand pocket. Use `fold.boltz2`. To validate a
designed binder, prefer `fold.boltz2` over `fold.esmfold` when the target
includes DNA, RNA, or a ligand, or when you need an interface score.

`fold.boltz2` runs as an async job and goes through the confirmation gate:
propose a complete spec, the user approves it, then poll `jobs.result`.

## The entity model
Every prediction is a list of `entities` — one record per molecular
component of the complex.

- `type` is `protein`, `dna`, `rna`, or `ligand`.
- `id` is a chain letter (`"A"`), or a list of letters for identical
  copies — a homodimer is one entity with `"id": ["A", "B"]`.
- `protein`/`dna`/`rna` carry a `sequence`; protein is the 20 canonical
  amino acids, dna is `ACGT`, rna is `ACGU`.
- a `ligand` carries exactly one of `smiles` (e.g. `"CCO"`) or `ccd`
  (a PDB Chemical Component Dictionary code, e.g. `"ATP"`) — never both.
- `cyclic: true` marks a head-to-tail cyclic protein/dna/rna; omit it
  otherwise. `cyclic` and `msa` do not apply to a ligand.
- chain ids must be unique across every entity.

A protein monomer:

```json
{"entities": [{"type": "protein", "id": "A", "sequence": "MKQLEDKVEELLSK"}]}
```

A protein with a small-molecule ligand:

```json
{"entities": [
  {"type": "protein", "id": "A", "sequence": "MKQLEDKVEELLSK"},
  {"type": "ligand", "id": "L", "smiles": "CC(=O)Oc1ccccc1C(=O)O"}
]}
```

## MSA choice
Boltz-2 accuracy depends on a multiple-sequence alignment. Each
protein/dna/rna entity sets its own `msa`:

- `"empty"` — single-sequence mode. The default. Fast, fully offline,
  lower accuracy. Use it for a quick sanity check or when the network is
  unavailable.
- `"server"` — fova queries the free ColabFold MSA server. Better
  accuracy, especially at interfaces; needs network and adds time.
  Use it whenever interface confidence matters.
- a workspace path to a precomputed `.a3m`/`.csv` alignment — use it
  when the user already has an MSA, to skip the server round-trip.

When in doubt: `empty` for a fast look, `server` for a result you will
act on.

## Model parameters
All optional — omit them to take Boltz-2's own defaults, and leave them
alone unless the user asks.

- `recycling_steps` — refinement passes (default 3).
- `sampling_steps` — diffusion steps per sample (default 200).
- `diffusion_samples` — number of predicted models (default 1). Raise
  this (e.g. to 5) to get several models you can rank by score.
- `step_scale` — diffusion temperature (default 1.638, useful range
  1–2); lower is more diverse, higher is more conservative.
- `output_format` — `pdb` (default) or `mmcif`.
- `save_as` — workspace path to write the top-ranked structure.

## Reading the result
`jobs.result` returns a designs envelope: one entry per predicted model.
Each model carries a `scores` map. The keys that matter:

- `plddt`, `ptm`, `iptm` — all 0–1, higher is better.
- `iptm` is the interface score for a complex; a low `iptm` means the
  chains are placed with low confidence relative to each other — treat
  that prediction's interface as unreliable.
- per-chain `chain_0_ptm`, `chain_1_ptm`, … — confidence for each chain.
- `confidence_score` — Boltz-2's overall ranking score.

When `diffusion_samples` > 1, rank the models by `iptm` (complex) or
`plddt` (monomer) and report the best.

## Worked example: a two-chain protein complex
Predict a heterodimer with server MSAs for the best interface estimate:

```json
{
  "entities": [
    {"type": "protein", "id": "A",
     "sequence": "MKQLEDKVEELLSKNYHLENEVARLKKLVGER", "msa": "server"},
    {"type": "protein", "id": "B",
     "sequence": "SEEELKKVEDQLKEAYHQLEEVARLKKLAGEN", "msa": "server"}
  ],
  "diffusion_samples": 5,
  "output_format": "pdb",
  "save_as": "predicted/complex.pdb"
}
```

This proposes five models; after approval, poll `jobs.result`, pick the
model with the highest `iptm`, and read `chain_0_ptm`/`chain_1_ptm` to
check both chains folded confidently.

## Stop conditions
- If the top model's `iptm` < 0.6, the interface is low-confidence —
  rerun with `msa: "server"` if you used `empty`, or report the weak
  interface to the user rather than treating the complex as solved.
- An invalid SMILES or an unknown CCD code surfaces as a job failure —
  fix the ligand spec and resubmit.
