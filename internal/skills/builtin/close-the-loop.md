# Skill: Closing the experimental loop

## When to use
Adaptyv results have arrived (via webhook, or fetched with `lab.results`). Use this
skill to interpret the kinetic data and turn it into the next design round.

## Reading the kinetics
Pull the per-sequence data with `lab.results({experiment_id})`. For each design:
- `Kd` — dissociation constant; lower is tighter binding (nM is strong, µM is weak).
- `kon` — association rate; how fast the complex forms.
- `koff` — dissociation rate; how fast it falls apart. `Kd = koff / kon`.
- A design with no measurable `Kd` (or expression failure) is a non-binder — record it,
  it is still a useful negative.

## Predicted vs measured
1. Place each design's predicted `score.ipsae` next to its measured `Kd`.
2. Compute the rank correlation between ipSAE and measured affinity across the set.
3. Classify outcomes:
   - True positives — high ipSAE designs that bind (precision).
   - False positives — high ipSAE designs that do not bind.
   - False negatives — real binders that scored poorly on ipSAE.

## Feeding the next round
- Use measured binders as positive controls and as scaffolds.
- If precision is high, redesign by partial diffusion around the top measured hits.
- If precision is low, tighten `score.filter` thresholds (raise the ipSAE cutoff) so
  the next shortlist is more selective.
- If there are strong false negatives, loosen filters or revisit the design method —
  the in-silico metric is missing real binders.

## Persist
Write the comparison, the correlation, and the threshold decision into the project
`notebook.md` so the next round inherits the calibrated thresholds.
