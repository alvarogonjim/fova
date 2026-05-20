# Skill: Designing protein binders

## When to use
The user wants a de novo protein binder against a non-antibody target (cell-surface
receptor, viral protein, soluble protein). For antibodies, use `design-antibody.md`.

## Primary method: BindCraft
Use `design.bindcraft` first. BindCraft has experimental success rates of 10–100%
across diverse targets and typically requires ≤10 designs to be screened to find
high-affinity binders.

## Fallback: RFdiffusion + ProteinMPNN
If BindCraft is unavailable or yields no hits passing filters:
1. `design.rfdiffusion` with target structure and hotspots
2. `design.proteinmpnn` over the generated backbones (8 sequences per scaffold)
3. `fold.esmfold` or `fold.colabfold` to validate
4. Filter on pAE_interaction < 10, pLDDT > 85, ipTM > 0.8

## Required inputs
- Target structure (PDB ID or file path)
- Target chain
- Hotspots (residues defining the desired binding site)

## Standard parameters
- `num_designs`: 100 for BindCraft, 5000 for RFdiffusion campaigns
- `length_range`: [60, 120] residues for mini-binders
- Shortlist 10–24 top designs by ipTM + Rosetta ΔΔG for wet-lab submission

## Stop conditions
- If <5 designs pass filters, increase `num_designs` 2× and retry once
- If still <5, escalate to the user with a summary of what failed
