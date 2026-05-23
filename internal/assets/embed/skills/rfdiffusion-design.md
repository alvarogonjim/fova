---
name: rfdiffusion-design
description: Author correct design.rfdiffusion configurations for de novo backbone generation against a target or unconditionally
---
# Skill: Designing protein backbones with RFdiffusion

## When to use
The user wants **a new protein backbone** — either a binder against a target
epitope, or an unconditional fold of a given length, or a re-diffused variant
of a starting motif. `design.rfdiffusion` is RFdiffusion v1: backbones only,
no sequence and no scores. It runs as an async GPU job — submit it, poll
`jobs.result` for the backbones, then run a sequence design (`design.proteinmpnn`
or `design.ligandmpnn`) and refold (`fold.boltz2` / `fold.chai1`) to **rank
them**, because RFdiffusion does not score its own output.

For antibody CDR design use `design.rfantibody` (also RFdiffusion-based, but
fine-tuned for HLT-formatted antibody frameworks). For ligand-conditioned
backbones use `design.rfdiffusion2`.

## Required field — `contigs`
`contigs` is the **RFdiffusion contig map** — the only required input. It is a
space-separated string that names the chains to copy from the target and the
new lengths to diffuse. The shape of `contigs` decides the campaign:

- **Unconditional length range:** `"50-100"` — a single new chain whose length
  is sampled in `[50, 100]` per design. Use when `target` is empty.
- **Binder against a target chain:** `"A1-100/0 60-80"` — copies residues 1-100
  of chain A from the target (the epitope context), then `/0` is a chain break,
  then a new 60-80 residue binder chain is diffused next to it.
- **Motif scaffolding:** `"5-15/A20-35/10-20"` — diffuse 5-15 residues, splice
  in the rigid motif from A20-35 of the target, then diffuse 10-20 more — the
  motif is held fixed, the rest is sampled.

The contig syntax is unforgiving — a typo silently changes what gets diffused.
When in doubt, ask the user to confirm the contig before running a large batch.

## Unconditional vs target-conditioned — the implicit ckpt switch
fova auto-selects the RFdiffusion checkpoint based on whether `target` is set:

- `target == ""` → `Base_ckpt.pt` (unconditional / motif scaffolding).
- `target == "<.pdb path>"` → `Complex_base_ckpt.pt` (binder against a target).

Set `target` to a workspace path to a `.pdb` file when the contig copies any
target chain. Leave it empty for purely unconditional generation. The adapter
rejects a non-`.pdb` target or a path that does not exist on disk.

## `hotspots` — the epitope to engage (binder mode)
`hotspots` is a comma-separated list of target residues, each `<chain><number>`
(e.g. `"A30,A33,A47"`). The diffused binder is pulled toward those residues.
**A bad hotspot is the most common cause of a failed binder campaign**:

- Pick **>~3 residues** biased toward **hydrophobic** side chains — hydrophobic
  contacts anchor the interface.
- Leave **~10 Å of target context** around the hotspots; do not pick a single
  isolated residue.
- **Avoid glycosylated patches and dense charge** — non-reproducible interfaces.

Hotspots are optional for unconditional runs.

## Symmetric assemblies
For a symmetric oligomer set `symmetric: true`, pick a `symmetry_kind` from
`{cyclic, dihedral, tetrahedral, octahedral, icosahedral}`, and set `n_chains`
to the assembly size. The contig still names one protomer — RFdiffusion stamps
out the copies. Example: a homo-tetramer with C4 symmetry:

```json
{ "contigs": "60-80", "symmetric": true, "symmetry_kind": "cyclic", "n_chains": 4 }
```

## `partial_t` — re-diffuse from a starting motif
`partial_t` is the partial-diffusion start step. With `partial_t: 0` (or unset)
RFdiffusion samples from pure noise. With `partial_t: 10-20` it starts from a
**partially noised version of the input PDB** — useful for *varying* a known
backbone without losing its overall fold. Higher `partial_t` → more variation;
lower → more conservative. Always pass an input PDB (via the contig motif
splice or `target`) when using `partial_t`.

## Guiding potentials — bias the diffusion
`guiding_potentials` is an array of named potential expressions; `guide_scale`
(scalar) controls how strongly they apply. The common ones:

- `binder_ROG` — penalise an extended binder (favour a compact radius of
  gyration). Helpful for shallow epitopes.
- `monomer_ROG` — same, applied to an unconditional monomer.
- `interface_ncontacts` — reward more inter-chain contacts; densify the
  interface.

A `guide_scale` of `1-5` is the usable range — start at `2`, raise it if
samples look too loose.

## `num_designs`, determinism, noise scales
- `num_designs` — backbones per submission. **20-100** for an exploratory pass,
  **a few hundred** when you mean it. Each backbone needs sequence design +
  a refold to be ranked, so total cost ≈ `num_designs × seqs × fold_cost`.
- `deterministic: true` — fix RFdiffusion's RNG. Use for reproducibility runs.
- `noise_scale_ca` / `noise_scale_frame` — tighten or loosen the sampling
  envelope. The defaults are well-tuned; only change them if a campaign is
  producing implausible geometry.

## RFdiffusion emits no scores — you MUST refold to rank
**This is the most important property of `design.rfdiffusion`.** The job
returns one record per backbone with `structure_file` set and `scores` **empty**.
Do not try to rank or filter on those — there is nothing to rank on yet. The
mandatory follow-up:

1. Run `design.proteinmpnn` (or `design.ligandmpnn` for a ligand-bound active
   site) on each backbone to design sequences.
2. Run `fold.boltz2` (preferred) or `fold.chai1` on each (backbone, sequence)
   complex to get `ipsae` / `iptm` / `plddt`.
3. Rank survivors by the refold's interface metrics, then shortlist.

`close-the-loop.md` walks through this end-to-end pipeline.

## Worked example — binder against a hydrophobic patch on chain A
The user wants a 60-80 residue binder against residues A30, A33, A47 of a
target in `designs/pdl1/target.pdb`:

```json
{
  "target": "designs/pdl1/target.pdb",
  "contigs": "A1-180/0 60-80",
  "hotspots": "A30,A33,A47",
  "num_designs": 50,
  "guiding_potentials": ["binder_ROG"],
  "guide_scale": 2
}
```

The contig copies residues 1-180 of chain A as context, then diffuses a 60-80
residue binder chain. `binder_ROG` keeps the binder compact against the
shallow patch. After 50 backbones land, hand them to `design.proteinmpnn`,
then to `fold.boltz2` for `ipsae` / `iptm` scores.

## Worked example — unconditional C3 homotrimer
A symmetric homotrimer of 70-90 residue monomers, no target:

```json
{
  "contigs": "70-90",
  "symmetric": true,
  "symmetry_kind": "cyclic",
  "n_chains": 3,
  "num_designs": 20
}
```

The contig names one protomer; symmetry stamps out the three chains. Refold
each design as a 3-chain complex to score the interfaces.

## Stop conditions
- If `contigs` is unset, refuse — RFdiffusion has nothing to diffuse.
- If `target` is set but does not exist on disk, ask the user to confirm the
  path (`fs.read_structure`) before submitting — do not invent it.
- If `hotspots` is fewer than ~3 residues, or sits on a glycosylated/charged
  patch, push back on the epitope before burning GPU.
- If `symmetric: true` but `symmetry_kind` is missing or `n_chains < 1`,
  preflight rejects — fix before re-submitting.
- After backbones land, **do not** report them as final designs. They must
  be sequence-designed and refolded for ranking.
