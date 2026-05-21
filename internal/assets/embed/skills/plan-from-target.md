---
name: plan-from-target
description: Turn a target into a structured DesignPlan before running tools
---
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
   - `knowledge.europepmc` (search) for "<target> de novo binder/antibody/enzyme"
   - Take top 30 results
   - `knowledge.corpus_add_from_search` with `from_search=<search_id>`, `max_papers=30`
4. Map over corpus:
   - `knowledge.corpus_map` with `prompt="What design methods were used and what was the experimental success rate?"` returns a `JobID`. Poll `jobs.status(JobID)` until `state=succeeded`; do not block on the result. Use the `elapsed`/`estimated` fields to report progress to the user (matches the v0.6 design-job pattern). Per-paper progress is surfaced as a 0..1 fraction on the job.
   - `knowledge.corpus_grep` with `pattern="success rate"`, `ignore_case=true` to confirm specific claims
5. Decide application area (binder | antibody | enzyme) — usually obvious from the user prompt.
6. Select method from the relevant `design-*.md` skill.
7. Set filter thresholds from `filter-thresholds.md`.
8. Estimate compute cost and time.
9. Produce a `DesignPlan` object and present to the user for approval.

## Citations
After `corpus.map`, when calling `plan.create`, pass the `corpus_paper_id`
of each evidence paper (the ID returned by `corpus_add` / `corpus_search`).
Do not write citation strings yourself — `plan.create` formats them from the
stored paper metadata.

## Output format
Always emit the plan as a structured object (the TUI renders it as a checklist).
Plain prose summaries are not enough — they cannot be edited or approved.
