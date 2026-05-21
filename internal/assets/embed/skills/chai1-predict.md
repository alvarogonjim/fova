---
name: chai1-predict
description: Predict biomolecular complex structures with Chai-1 (fold.chai1)
---
# Skill: Predicting structures with Chai-1

## When to use
The user wants the 3D structure of a biomolecular complex and either wants a
fast prediction with no MSA, needs glycans, or wants to steer the fold with
explicit restraints. Use `fold.chai1`. Chai-1 handles protein, DNA, RNA,
small-molecule ligands, and glycans in one complex, and its default mode
needs no network. `fold.boltz2` is the alternative predictor — pick
`fold.chai1` when restraints, glycans, or offline operation matter.

`fold.chai1` runs as an async job and goes through the confirmation gate:
propose a complete spec, the user approves it, then poll `jobs.result`.

## The entity model
Every prediction is a list of `entities` — one record per molecular
component of the complex.

- `type` is `protein`, `dna`, `rna`, `ligand`, or `glycan`.
- `id` is one chain letter (`"A"`). Chain ids must be unique across every
  entity. For identical copies, add a separate entity per chain.
- `protein`/`dna`/`rna` carry a `sequence` — protein is the 20 canonical
  amino acids, dna is `ACGT`, rna is `ACGU`.
- a modified residue is written **inline** in the protein sequence as a
  parenthesised token, e.g. `"MKQ(SEP)AAS(TPO)R"` for a phosphoserine and a
  phosphothreonine.
- a `ligand` carries a `smiles` string (e.g. `"CCO"`).
- a `glycan` carries a `glycan` string in Chai-1 glycan notation, e.g.
  `"NAG(4-1 NAG)(4-1 BMA)"` — fova passes it through verbatim.

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
`msa` is a single **request-level** string — Chai-1's MSA inputs are
job-wide, not per-entity.

- `"default"` — ESM embeddings, no MSA. This is Chai-1's real default:
  fast, fully offline, and properly supported (single-sequence mode is
  first-class here, unlike Boltz-2). Use it unless you have a reason not to.
- `"server"` — fova queries the free ColabFold MSA server. Better accuracy,
  especially at interfaces; needs network and adds time.
- a workspace path to a directory of precomputed `.aligned.pqt` files — use
  it when the user already has alignments, to skip the server round-trip.

When in doubt: `default` for a fast result, `server` when interface
confidence matters and the network is available.

## Restraints
The optional `restraints` list steers the fold with known contacts. Each
restraint references two chains and an optional residue/atom on each.

- `connection_type` — `contact` (the two refs should be close),
  `pocket` (a residue sits in a binding pocket), or `covalent` (a covalent
  bond, e.g. a ligand attachment point).
- `chain_a`/`chain_b` are required chain ids; `res_a`/`res_b` are optional.
  A residue ref is `"A219"` (chain `A`, residue 219); an atom ref adds
  `@atom`, e.g. `"A219@CA"`. Leave a ref `""` to restrain the whole chain.
- `min_distance` / `max_distance` — optional bounds in angstroms; set
  `max_distance` for a `contact`. Omit one or both to leave it open.
- `confidence` — optional, 0–1, how strongly to enforce the restraint
  (defaults to 1.0). Lower it for a hypothesis you are less sure of.

Add restraints when the user supplies experimental contacts (crosslinks, a
known catalytic residue, a covalent ligand site); omit the list for an
unbiased prediction.

## Templates
The optional `templates` object supplies structural templates.

- `templates.server: true` — fova searches the template server. Use it when
  a homolog likely exists and the network is available.
- `templates.hits_path` — a workspace path to a precomputed template-hits
  file. Use it when the user already ran a template search.

Omit `templates` for a template-free prediction (the common case).

## Model parameters
All optional — omit them to take Chai-1's own defaults, and leave them alone
unless the user asks.

- `num_trunk_recycles` — trunk refinement passes (default 3).
- `num_diffn_timesteps` — diffusion steps per sample (default 200).
- `num_diffn_samples` — number of predicted models (default 5). Raise it to
  get more models to rank by score.
- `num_trunk_samples` — trunk samples (default 1).
- `recycle_msa_subsample` — MSA subsampling on recycle (default 0).
- `seed` — random seed; set it for a reproducible run.
- `save_as` — workspace path to write the top-ranked structure.

## Reading the result
`jobs.result` returns a designs envelope: one entry per predicted model.
Each model carries a `scores` map. The keys that matter:

- `aggregate_score` — Chai-1's overall ranking score; rank models by it.
- `ptm`, `iptm` — both 0–1, higher is better. `iptm` is the interface
  score for a complex; a low `iptm` means the chains are placed with low
  confidence relative to each other.
- per-chain `chain_0_ptm`, `chain_1_ptm`, … — confidence for each chain.
- `has_inter_chain_clashes` — `0` or `1`; `1` means the model has
  inter-chain clashes and should be distrusted regardless of its scores.

When `num_diffn_samples` > 1, drop any model with
`has_inter_chain_clashes == 1`, then pick the highest `aggregate_score`.

## Worked example: a protein–ligand complex with a contact restraint
Predict a protein bound to a ligand, telling Chai-1 that residue 219
contacts the ligand:

```json
{
  "entities": [
    {"type": "protein", "id": "A",
     "sequence": "MKQLEDKVEELLSKNYHLENEVARLKKLVGER"},
    {"type": "ligand", "id": "L", "smiles": "CC(=O)Oc1ccccc1C(=O)O"}
  ],
  "msa": "default",
  "restraints": [
    {"connection_type": "contact", "chain_a": "A", "res_a": "A219",
     "chain_b": "L", "res_b": "", "max_distance": 6.0, "confidence": 1.0,
     "comment": "catalytic pocket contact"}
  ],
  "num_diffn_samples": 5,
  "seed": 42,
  "save_as": "predicted/complex.cif"
}
```

This proposes five models; after approval, poll `jobs.result`, discard any
model with `has_inter_chain_clashes == 1`, pick the highest
`aggregate_score`, and check `iptm` to confirm the ligand is placed
confidently against the protein.

## Stop conditions
- If the best clash-free model has `iptm` < 0.6, the interface is
  low-confidence — rerun with `msa: "server"` if you used `default`, or
  report the weak interface rather than treating the complex as solved.
- If every model has `has_inter_chain_clashes == 1`, the complex did not
  fold cleanly — revisit the restraints or report the failure to the user.
- An invalid SMILES or an unparseable glycan string surfaces as a job
  failure — fix the entity spec and resubmit.
