# Skill: Designing enzymes

## When to use
The user wants a de novo enzyme for a specific reaction. For binders and
antibodies, use `design-binder.md` or `design-antibody.md` instead.

## Primary method: RFdiffusion2 + LigandMPNN
Use `design.rfdiffusion2` (atom-level active site scaffolding) to generate
backbones around the catalytic motif, then `design.ligandmpnn` (ligand-conditioned
sequence design) over those backbones. Validate with `fold.chai1` rather than
AlphaFold2 — Chai-1's side-chain interaction distances align better with native
enzyme active sites.

## Theozyme requirement
A theozyme (idealized arrangement of catalytic functional groups around the
reaction transition state) is required input. Ask the user for it; if not
provided, search the literature with `knowledge.corpus` for related transition
states before proceeding.

## Required inputs
- Theozyme (catalytic functional-group geometry around the transition state)
- Target reaction / substrate
- Catalytic residues to keep fixed during sequence design

## Standard parameters
- `num_designs`: 1000 backbones via `design.rfdiffusion2`
- 8 `design.ligandmpnn` sequences per backbone, catalytic residues fixed
- Filter on pLDDT > 80 and motif RMSD < 1 Å via `score.filter`
- For published successes (serine hydrolases), <96 sequences were tested per case

## Workflow
1. Validate theozyme geometry
2. `design.rfdiffusion2` generates backbones around the theozyme
3. `design.ligandmpnn` designs sequences (fix catalytic residues; design the rest)
4. `fold.chai1` predicts the complex; filter by motif RMSD
5. Optional: PLACER for a dynamic ensemble check on the top designs

## Stop conditions
- If <5 designs pass filters, increase `num_designs` 2× and retry once
- If still <5, escalate to the user with a summary of what failed
