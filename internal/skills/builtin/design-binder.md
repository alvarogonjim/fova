# Skill: Designing protein binders

## When to use
The user wants a de novo protein binder against a non-antibody target (cell-surface
receptor, viral protein, soluble protein). For antibodies, use `design-antibody.md`.

## Primary method: BindCraft
Use `design.bindcraft` first. BindCraft has experimental success rates of 10–100%
across diverse targets and typically requires ≤10 designs to be screened to find
high-affinity binders.

**Availability:** BindCraft requires PyRosetta, which has no Python 3.12 aarch64
wheel. On Grace CPU / GB10, `/install bindcraft` fails fast with an arch error
and BoltzGen below is the primary method instead.

## Alternative: BoltzGen
Use `design.boltzgen` when BindCraft is unavailable (e.g. on aarch64 / Grace).
BoltzGen is the SPECS-blessed pure-Python diffusion binder design pipeline
that refolds through Boltz-2 — no PyRosetta dependency. Per Stark et al. 2026
it matches or beats BindCraft success rates on the benchmarked targets.
Standard call: `design.boltzgen` with the `protein-anything` protocol for a
soluble protein target, `peptide-anything` for cyclic-peptide design, or
`protein-small_molecule` for ligand binders.

## Fallback: RFdiffusion + ProteinMPNN
If neither BindCraft nor BoltzGen is suitable, or both yield no hits:
1. `design.rfdiffusion` with target structure and hotspots
2. `design.proteinmpnn` over the generated backbones (8 sequences per scaffold)
3. `fold.esmfold` or `fold.colabfold` to validate
4. Filter on pAE_interaction < 10, pLDDT > 85, ipTM > 0.8

## Required inputs
- Target structure (PDB ID or file path)
- Target chain
- Hotspots (residues defining the desired binding site)

## Standard parameters
- `num_designs`: 100 for BindCraft, 50→10000 for BoltzGen (start low,
  scale up after a warm-up run), 5000 for RFdiffusion campaigns
- `length_range`: [60, 120] residues for mini-binders
- Shortlist 10–24 top designs by ipSAE for wet-lab submission

## Stop conditions
- If <5 designs pass filters, increase `num_designs` 2× and retry once
- If still <5, escalate to the user with a summary of what failed
