---
name: ligandmpnn-design
description: Design protein sequences for a fixed backbone with LigandMPNN (design.ligandmpnn)
---
# Skill: Designing sequences with LigandMPNN

## When to use
The user has a backbone (a `.pdb`) and wants new amino-acid sequences that fold
to it — inverse folding / sequence design. `design.ligandmpnn` is ligand-aware:
it reasons about a bound small molecule, ion, or nucleic acid, so it is the
right tool for enzyme active sites and any ligand-bound pocket. For de novo
*backbone* generation use `design.rfdiffusion`; for binder campaigns use
`design-binder.md`. `design.ligandmpnn` runs as an async GPU job — submit it,
then poll `jobs.result` for the designs.

## The five model types — pick one with `model_type`
`model_type` is an enum; it defaults to `ligand_mpnn`.

| `model_type` | Use for | Notes |
|---|---|---|
| `ligand_mpnn` | A protein with a bound ligand / cofactor / ion — enzyme active sites, metal sites (DEFAULT) | Sees ligand atoms; populates `ligand_confidence`. |
| `soluble_mpnn` | A soluble globular protein with no ligand | Biased away from surface hydrophobics. |
| `protein_mpnn` | The original ProteinMPNN — a plain protein, no ligand context | Equivalent to the separate `design.proteinmpnn` tool. |
| `global_label_membrane_mpnn` | A transmembrane protein, one label for the whole chain | Set `global_transmembrane_label` (0 = soluble, 1 = TM). |
| `per_residue_label_membrane_mpnn` | A transmembrane protein with per-residue burial | Use `transmembrane_buried` / `transmembrane_interface`. |

Rule of thumb: a ligand in the pocket ⇒ `ligand_mpnn`; a soluble protein ⇒
`soluble_mpnn`; spanning a membrane ⇒ one of the membrane models.

## Residue selection
LigandMPNN designs the *whole* structure by default. Constrain it with exactly
one of these — `redesigned_residues` and `fixed_residues` are mutually
exclusive; pick the shorter list:

- `redesigned_residues` — a space-separated allowlist; ONLY these positions are
  redesigned, everything else is held fixed. Use when redesigning a few sites.
- `fixed_residues` — a space-separated blocklist; these positions are held at
  their native identity, everything else is redesigned. Use when freezing a
  catalytic core and redesigning the rest.
- `chains_to_design` — a comma-separated chain list (`A,C`); restricts design to
  whole chains. Combine with `fixed_residues` to also pin residues within them.

Residue references use the LigandMPNN format `<chain><number>[<icode>]`:
`A23` is chain A residue 23; `B42D` is chain B residue 42 insertion-code D.
Lists are space-separated (`"A23 A24 B42D"`); chain lists are comma-separated
(`"A,B"`). Numbering follows the input PDB.

## Amino-acid biases
- `bias_AA` — a global log-odds bias, comma-separated `<AA>:<float>` pairs. A
  positive number encourages a residue, negative discourages. Example:
  `"W:3.0,P:-2.0"` strongly favours tryptophan, penalises proline.
- `omit_AA` — a bare letter string of residues the designer may NEVER place,
  e.g. `"CG"` forbids cysteine and glycine (a common choice to avoid unwanted
  disulfides and backbone flexibility).

Use biases sparingly — they override the model's learned preferences. Per-residue
variants (`bias_AA_per_residue`, `omit_AA_per_residue`) take a workspace JSON
file path and are for advanced position-specific control.

## Side-chain packing
By default LigandMPNN returns sequences only. Set `pack_side_chains: true` to
also build full side-chain coordinates — needed when a downstream step (Rosetta
scoring, a clash check, a visual review of the pocket) needs an all-atom model.
Tune `number_of_packs_per_design` for several packs per sequence and
`pack_with_ligand_context: true` so the packer sees the ligand. Packing costs
extra time; leave it off if you only need sequences for refolding.

## Reading the result scores
`jobs.result` returns one design per record, each with a `scores` map. All
three are in `[0, 1]` and higher is better:

- `overall_confidence` — the model's confidence in the whole designed sequence.
- `ligand_confidence` — confidence around the ligand pocket; `0.0` when the
  model is not ligand-aware (`soluble_mpnn` / `protein_mpnn`).
- `sequence_recovery` — fraction of positions matching the native input. Low
  recovery is normal and often desirable when you asked for a redesign; treat
  it as a diversity readout, not a quality score.

Shortlist designs by `overall_confidence`, and for an enzyme also by
`ligand_confidence`. A missing key just means that score is absent — not an
error.

## Worked example — redesign an enzyme active site around a bound ligand
The user has `designs/kemp/scaffold.pdb`, an enzyme backbone with a bound
ligand in chain A. They want new active-site sequences while keeping the
catalytic triad (A55, A101, A179) fixed, and all-atom models to inspect.

```json
{
  "model_type": "ligand_mpnn",
  "pdb": "designs/kemp/scaffold.pdb",
  "num_designs": 16,
  "temperature": 0.2,
  "fixed_residues": "A55 A101 A179",
  "omit_AA": "C",
  "pack_side_chains": true,
  "pack_with_ligand_context": true,
  "number_of_packs_per_design": 1
}
```

`ligand_mpnn` so the pocket is designed with the ligand in context; the triad
is pinned via `fixed_residues`; cysteine is omitted to avoid stray disulfides;
side chains are packed with the ligand visible. A low `temperature` (0.1–0.3)
keeps designs conservative — raise it toward 0.5 for more diversity.

A minimal soluble-protein call, by contrast, is just:

```json
{ "model_type": "soluble_mpnn", "pdb": "designs/scaffold.pdb", "num_designs": 8 }
```

## Stop conditions
- If the user's structure has a ligand but you chose a non-ligand model (or
  vice versa), switch `model_type` before submitting.
- If `redesigned_residues` / `fixed_residues` tokens do not match
  `<chain><number>`, fix them — preflight rejects malformed references.
- If no `.pdb` backbone is available, ask the user for one — do not invent a
  path.
