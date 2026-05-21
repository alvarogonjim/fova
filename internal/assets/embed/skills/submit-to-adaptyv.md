---
name: submit-to-adaptyv
description: Submit a design shortlist to the Adaptyv wet lab
---
# Skill: Submitting designs to Adaptyv

## When to use
The user wants to validate a shortlist of designs in the wet lab. Submission sends
sequences to Adaptyv Bio's Foundry for synthesis and assay. It costs real money and
takes ~21 days, so treat it as the most expensive action in a campaign — never submit
speculatively or without an explicit user request.

## Pre-flight checks
Before calling `lab.submit_experiment`:
1. Run `score.solubility` on every sequence; warn the user on any predicted insoluble
   candidate and offer to drop it from the shortlist.
2. Re-run `score.filter` so the shortlist still passes thresholds (ipSAE first) — do
   not submit designs that no longer rank.
3. Verify each sequence's length and composition and flag unpaired cysteines.
4. Confirm Adaptyv carries the requested target with `lab.targets_search`; capture the
   Adaptyv target ID.
5. Get a price with `lab.cost_estimate({target_id, assay_type, sequences})`.

## Confirmation
ALWAYS require explicit user confirmation before submission. `lab.submit_experiment`
sets `RequiresConfirmation`, so a confirmation modal is mandatory — never bypass it.
Show the user:
- Target name and Adaptyv target ID
- Assay type (binding | thermostability | expression)
- Number of sequences and the first 3 sequence previews
- Cost in USD (from `lab.cost_estimate`)
- Turnaround (~21 days)
- Webhook URL that results will return to

## Submitting
After the user confirms (`y`), call `lab.submit_experiment` with the same target,
assay, and sequences shown in the modal. It POSTs to Adaptyv, persists a
`domain.Experiment` carrying the returned `experiment_id`, and the wet-lab panel
begins tracking the experiment.

## After submission
- Save `experiment_id` and link it to the relevant `Design` records for provenance.
- Tell the user results arrive via webhook in ~21 days; offer to set a reminder.
- When results land, use `close-the-loop.md` to interpret them.
