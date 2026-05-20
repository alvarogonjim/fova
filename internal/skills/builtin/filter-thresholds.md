# Skill: Filter thresholds

Standard cutoffs for shortlisting designs. Apply via `score.filter`.
Rank candidates by **ipSAE first** (best experimental correlation per Dunbrack 2025
and the Adaptyv Nipah G binder competition). Fall back to ipTM / pAE_interaction
only when ipSAE is unavailable.

| Metric | Threshold | Priority | Notes |
|---|---|---|---|
| ipSAE_min | > 0.50 | primary | inter-chain score; AF2/AF3/Boltz/Chai-compatible |
| pLDDT (binder mean) | > 80 | required | per-residue confidence on designed chain |
| pLDDT (min) | > 60 | required | catches local disorder |
| pAE_interaction | < 10 | secondary | use only if ipSAE unavailable |
