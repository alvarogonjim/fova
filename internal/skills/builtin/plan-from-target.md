# Skill: Planning from a target

## Procedure
Given a user-provided target, produce a `DesignPlan` for approval.

1. Identify the entity:
   - If PDB ID: `knowledge.pdb` for structure
   - If UniProt accession: `knowledge.uniprot` for sequence + features
   - If natural language: search for canonical identifier first
2. Characterize:
   - Domains (`knowledge.interpro`)
   - Known interactions, active sites, hotspots
3. Search literature:
   - `knowledge.europepmc.search` for "<target> de novo binder/antibody/enzyme"
   - Take top 30 results
   - `knowledge.corpus.add --from <search_id> --max 30`
4. Map over corpus:
   - `knowledge.corpus.map "What design methods were used and what was the experimental success rate?"`
   - `knowledge.corpus.grep -i "success rate"` to confirm specific claims
5. Decide application area (binder | antibody | enzyme) — usually obvious from the user prompt.
6. Select method from the relevant `design-*.md` skill.
7. Set filter thresholds from `filter-thresholds.md`.
8. Estimate compute cost and time.
9. Produce a `DesignPlan` object and present to the user for approval.

## Output format
Always emit the plan as a structured object (the TUI renders it as a checklist).
Plain prose summaries are not enough — they cannot be edited or approved.
