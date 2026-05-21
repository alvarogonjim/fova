# Skill: Authoring BoltzGen design specifications

## When to use
You are about to author or edit a `design.boltzgen` specification YAML — the
`specification.yaml` file that drives a BoltzGen run. Every `design.boltzgen`
job needs a spec file; the run params (`protocol`, `num_designs`, `budget`,
...) are CLI flags, but *what* gets designed is expressed entirely in the
spec. Read this before writing the YAML, then validate it with
`design.boltzgen_check`.

## Workflow
1. Decide the protocol (table below) from the target type and the user's
   intent.
2. Write the spec YAML to a workspace file with `fs.write` (e.g.
   `designs/<run>/spec.yaml`). Place referenced structure files (`.cif` /
   `.pdb`) in the same directory or a subdirectory — paths in the spec are
   resolved relative to the spec file.
3. Validate with the `design.boltzgen_check` tool (`spec_path` = the file you
   wrote). It runs `boltzgen check` — cheap, no GPU, no weights — and returns
   `{valid, errors[], visualization_path}`.
4. If `valid` is false, fix the errors and re-check. The
   `visualization_path` is an mmCIF you (or the user) can open in a viewer to
   confirm the binding site is highlighted correctly.
5. Reference the validated spec path from `plan.create` (`method_spec_path`),
   then `/plan approve` re-checks it and submits the `design.boltzgen` job.

## Critical caveats
- **Residue indices are 1-based and use the canonical mmCIF `label_asym_id` /
  `label_seq_id`, NOT the author numbering (`auth_asym_id` / `auth_seq_id`).**
  These often differ. To verify an index, open the mmCIF at
  https://molstar.org/viewer/, hover a residue, and read the `label` index
  (bottom-right) — not the author id. Wrong indices silently produce a design
  against the wrong site.
- **File paths inside a spec are relative to the spec file's directory**, not
  the workspace root and not the CWD. Co-locate the `.cif`/`.pdb` with the
  spec.
- Length ranges are sampled per design; `80..140` means each design draws a
  random length in `[80, 140]`.

## Protocols
Pick `protocol` (a `design.boltzgen` run param) by target + design type:

| Protocol | Use for | Notes |
|---|---|---|
| `protein-anything` | Design a protein to bind a protein or peptide target (default) | Includes a `design folding` step. |
| `peptide-anything` | Design a (cyclic) peptide binder | No Cys generated in inverse folding; no `design folding` step; skips hydrophobic-patch metric. |
| `protein-small_molecule` | Design a protein to bind a small-molecule ligand | Adds binding-affinity prediction; includes `design folding`. |
| `antibody-anything` | Design antibody CDRs | No Cys in inverse folding; no `design folding` step. |
| `nanobody-anything` | Design nanobody CDRs | Same settings as `antibody-anything`. |
| `protein-redesign` | Redesign / optimize an existing protein | No `design folding` step; uses a `design_mask`; scores complexes without separate binder/target. |

## The spec schema

Top level has two keys: `entities` (a list) and optional `constraints`.

```yaml
entities:
  - protein: ...   # a protein chain (designed or fixed)
  - ligand: ...    # a small molecule
  - file: ...      # parts imported from a .cif / .pdb file
constraints:
  - bond: ...
  - total_len: ...
```

### `protein` entities
- `id` — unique chain identifier (e.g. `B`, `G`).
- `sequence` — the sequence notation:
  - `80..140` — design a random length between 80 and 140 (inclusive).
  - `18` — design exactly 18 residues.
  - `AAVTTTTPPP` — fixed amino-acid letters (a non-designed chain, or a
    fixed segment).
  - `15..20AAAA18` — mixed: a 15–20-residue design region, then fixed `AAAA`,
    then an 18-residue design region.
  - `C` — a fixed cysteine at that position (used to anchor disulfides /
    staples, e.g. `3..5C6C3`).
- `secondary_structure` — for designed regions: `loop` / `helix` / `sheet`
  over residue ranges, or a string like `HHHLLLEEE`.
- `binding_types` — which residues bind the target. Two forms:
  - String: `uuuBBBuNNN` where `B` = binding, `N` = non-binding, `u` =
    unspecified.
  - Ranges (per chain): `binding: 5..7,13` and/or `not_binding: 9..11` (or
    `not_binding: "all"`).
- `cyclic` — boolean; the chain is a cyclic peptide.
- `residue_constraints` — per-position `allowed` / `disallowed` amino-acid
  sets, restricting what the designer may place at given residues.

### `ligand` entities
- `id` — single id or a list (a list copies the entity).
- `ccd` — a Chemical Component Dictionary code (e.g. `WHL`, `SAH`), OR
- `smiles` — a SMILES string (e.g. `'N[C@@H](Cc1ccc(O)cc1)C(=O)O'`).
- `binding_types` — usually just `B`.

### `file` entities
Import a target (or scaffold) from a structure file.
- `path` — path to the `.cif`/`.pdb`, **relative to the spec file**.
- `include` — `"all"` or a list of chains; per chain you can give
  `res_index` ranges (`2..50,55..`). Unspecified `include` uses all chains.
- `exclude` — chains/residues to drop from the included set.
- `include_proximity` — include residues within `radius` Å of a chain/range.
- `binding_types` — per-chain `binding` / `not_binding` ranges on the target,
  telling the designer where to bind.
- `structure_groups` — visibility of target regions: `visibility: 1` =
  structure specified (default), `0` = structure hidden, `2` = a separate
  group whose relative position is unconstrained. Can be `"all"` for the
  simple case.
- `design` — residues in the imported chain that should themselves be
  redesigned.
- `secondary_structure` — secondary-structure constraints on designed
  residues within the file's chains.
- `design_insertions`, `fuse`, `msa`, `reset_res_index` — advanced.

### `constraints`
- `bond` — a covalent bond: `atom1` and `atom2`, each `[chain, res, atom]`
  (e.g. `[R, 4, SG]`). Index residues as if the minimum length was sampled.
- `total_len` — `min` / `max` on the total polymeric length.

## Examples (verbatim from the BoltzGen repo)

### 1. Vanilla protein binder against a target (no binding site specified)
`example/vanilla_protein/1g13prot.yaml` — design an 80–140-residue protein
against chain A of a target structure.

```yaml
entities:
  # Specify a designed protein chain
  # random number between 80 and 140 of designed residues (inclusive)
  - protein: 
      id: C
      sequence: 80..140
  # Specification of the target which is extracted from a .cif file
  - file:
      path: 1g13.cif
      # Which chain and residues in the .cif file to use as target (uses only chain A here)
      include: 
        - chain:
            id: A
```

### 2. Peptide binder with a target binding site
`example/vanilla_peptide_with_target_binding_site/beetletert.yaml` — design a
12–20-residue peptide that binds residues 343, 344, 251 of chain A.

```yaml
entities:
  # Specify a designed protein chain
  # random number between 12 and 20 of designed residues (inclusive)
  - protein: 
      id: G
      sequence: 12..20

  # Specification of the target which is extracted from a .cif file
  - file:
      path: 5cqg.cif
      # Which chain and residues in the .cif file to use as target (uses only chain A here)
      include:
        - chain:
            id: A
      # Which regions of the target the design should or should NOT bind to
      # Here we specify that the design should bind to residues 343, 344, and 251 on chain A
      binding_types:
        - chain:
            id: A
            binding: 343,344,251
      # Which regions of the target should have their structure specified
      # Here we specify that all included target residues should have their structure specified
      structure_groups: "all"
```

### 3. Peptide against a specific site (large binding-site residue list)
`example/peptide_against_specific_site_on_ragc/rragc.yaml` — a 5–20-residue
peptide that binds a specific, explicitly enumerated site on chain G.

```yaml
entities:
  - protein: 
      id: P
      sequence: 5..20
  - file:
      path: 6wj3.cif
       
      include: 
        - chain:
            id: G

      binding_types:
        - chain:
            id: G
            binding: 190,193,194,258,259,262,263,205,214,215,216,217,218,219,220,221,222,232,236,239,278,279,280,281,282,283,284,285,286,240,245,246,249,250,253,254,256,257,261,262
```

## Stop conditions
- If `design.boltzgen_check` keeps reporting the same error after a fix,
  re-read the schema section above and verify chain ids and `label_seq_id`
  indices against the actual mmCIF before retrying.
- If the target structure file is missing or the user has not provided one,
  ask the user for it — do not invent a path.
