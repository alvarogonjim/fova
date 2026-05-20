# Skill: Designing antibodies

## When to use
The user wants a VHH (nanobody) or scFv against a specific epitope. For non-antibody
targets, use `design-binder.md` instead.

## Primary method: RFantibody
Use `design.rfantibody` first (RFdiffusion fine-tuned for antibody CDR loops). It
generates antibody backbones whose CDR loops are diffused to contact the requested
epitope. AlphaFold2 is NOT a reliable filter for antibody-antigen complexes; do not
rank designs by it. Score the RF2-AB complexes with `score.ipsae` instead — ipSAE
works on RosettaFold2-AB output and correlates far better with experimental binding.

## Fallback: Chai-2
If `design.rfantibody` yields <10 high-confidence hits on a challenging target,
try `design.chai2`, which achieves binding for ~50% of targets including
sub-nanomolar affinities.

## Required inputs
- Target structure (PDB ID or file path)
- Epitope hotspots (the CDR loops will be designed to contact these residues)
- Framework selection (default: humanized hu4D5 / trastuzumab)

## Standard parameters
- `num_designs`: 5000 backbones via `design.rfantibody`
- 8 sequences per backbone
- Filter on RF2-AB pAE_interaction < 10, pLDDT > 85 via `score.filter`
- Rank by ipSAE (`score.ipsae`) and shortlist the top 24 for wet-lab submission

## Wet-lab notes
- Adaptyv supports binding assays for antibody designs
- Note that a ~21 day turnaround applies — plan campaign timelines accordingly

## Stop conditions
- If <10 designs pass filters with `design.rfantibody`, fall back to `design.chai2`
- If still <10, escalate to the user with a summary of what failed
