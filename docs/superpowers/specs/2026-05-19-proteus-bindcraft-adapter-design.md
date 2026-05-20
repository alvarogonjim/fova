# Proteus — SP3: BindCraft adapter — Design

**Date:** 2026-05-19
**Status:** Approved, ready for planning
**Milestone:** Design-tool backend wiring — SP3 of 3 (final)
**Parent design:** `docs/superpowers/specs/2026-05-19-proteus-design-tool-backend-wiring-design.md` (§6 SP3 outline)
**Predecessors:** SP1 (ProteinMPNN) and SP2 (RFdiffusion) — merged to `master`.

## 1. Goal & scope

Wire the agent's `design.bindcraft` tool to run BindCraft through the local
backend, as the third and final `ToolAdapter`, following the SP1/SP2 pattern.
The agent supplies BindCraft's target-settings as a JSON object (pass-through);
the adapter writes it and runs `bindcraft.py`.

Automated validation is **Tier 0** (stub `CmdRunner` + a fixture results dir).
BindCraft is GPU- and AlphaFold-bound, so there is no Tier 1; a real run is
Tier 3, gated on the GB10 `sm_121` torch issue — documented, not a gate for SP3.

No foundation change is needed: SP2 already added `AdapterEnv.Registry`, which
this adapter reuses for the `alphafold_params` data asset and `Home()`.

## 2. Contract

- **Agent tool:** `design.bindcraft`. **Recipe:** `bindcraft`.
- **Request** field the adapter reads:
  - `settings` — **required**, a JSON object: BindCraft's target-settings
    (`starting_pdb`, `chains`, `target_hotspot_residues`, `lengths`,
    `number_of_final_designs`, …). The agent constructs it (pass-through).
- The adapter **overrides exactly one key**: it injects its own `design_path`
  into the settings before writing them, so output lands in a directory the
  adapter controls and can parse. Any `design_path` the agent supplied is
  replaced. Every other key is passed through verbatim.
- **Response:** the `{"designs":[…]}` schema — one entry per accepted BindCraft
  design, `structure_file` set, `sequence`/`scores` populated from the stats CSV
  when present.

`settings` is added to the shared `designTool.InputSchema()`
(`internal/tools/design/design.go`) as an object property so the agent knows to
supply it. The ProteinMPNN and RFdiffusion tools harmlessly ignore it.

## 3. Choreography (`Invoke`)

1. Parse the request; validate `settings` is present and a non-empty JSON
   object — fail fast otherwise.
2. Unmarshal `settings` into a `map[string]any`. If it carries a non-empty
   `starting_pdb` string, validate that path is an existing file.
3. Validate `bindcraft` is installed — `Recipe.InstallDir` and `Recipe.VenvDir`
   both exist as directories; else an error suggesting `/install bindcraft`.
4. Validate the AlphaFold params: `env.Registry.DataAsset("alphafold_params")`
   → its `ExtractTo` directory must exist; else an error naming
   `alphafold_params`.
5. Create the **persistent** output directory
   `<Registry.Home()>/designs/bindcraft-<unix-nano-timestamp>/`. Set the
   settings map's `design_path` to that directory.
6. Marshal the (modified) settings map to `<WorkDir>/settings.json`.
7. Run: `<VenvDir>/bin/python <InstallDir>/bindcraft.py --settings <WorkDir>/settings.json`
   via `env.Run` in `Recipe.InstallDir`. `--filters` and `--advanced` are not
   passed — BindCraft falls back to its bundled default settings files.
8. Parse the results directory (§4). Zero accepted designs → error.
9. Return `{"designs":[…]}`.

The temp `WorkDir` holds only the input `settings.json`; durable output goes to
`design_path` (§5).

## 4. Output parsing

After a run, `<design_path>/` contains accepted designs in `Accepted/*.pdb` and
a stats CSV `final_design_stats.csv`.

- Glob `<design_path>/Accepted/*.pdb` — one design per file, `structure_file`
  set to the absolute path.
- If `<design_path>/final_design_stats.csv` exists, parse it header-driven:
  - a column whose name is `Sequence` (case-insensitive) → `sequence["A"]`;
  - every other column whose value parses as a float → an entry in `scores`
    under that column name;
  - join a CSV row to a PDB by the design-name stem (the PDB's base filename
    without extension, matched against the row's first/`Design` column).
- A PDB with no matching CSV row still yields a design (`structure_file` only).
- Zero `Accepted/*.pdb` files → error (never a silent empty-success).

## 5. Output persistence

As in SP2, BindCraft's output is the design itself and `domain.Design.StructureFile`
must point at a file that outlives the job. `RunDesign`'s `WorkDir` is a temp
directory removed on return. The adapter therefore sets `design_path` to
`<Registry.Home()>/designs/bindcraft-<timestamp>/`, a persistent location under
`$PROTEUS_HOME`.

## 6. AlphaFold params

The adapter validates the `alphafold_params` data asset directory
(`DataAsset("alphafold_params").ExtractTo`) exists, failing fast with a clear
message otherwise. How BindCraft *locates* AF2 params at run time is a Tier-3
concern (a real GPU run) and is not exercised by Tier-0 validation; SP3 does not
attempt to verify or rewire it.

## 7. Error handling

Missing/empty `settings`; a `starting_pdb` that does not exist; `bindcraft` not
installed; the `alphafold_params` asset directory absent; the BindCraft run
failing (error includes the command's combined output, which carries the GB10
`sm_121` CUDA message when it occurs); zero accepted designs — each returns a
clear, step-named error. All propagate as the job's failure.

## 8. Files

- Create `internal/backends/local/adapter_bindcraft.go` — the adapter, its
  `init()` registration, request struct, settings injection, results parser.
- Create `internal/backends/local/adapter_bindcraft_test.go` — Tier 0 tests
  with a stub `CmdRunner` and a synthesized fixture results directory.
- Modify `internal/tools/design/design.go` — add `settings` to the shared
  `InputSchema()`.

After SP3, all three `design.*` tools have local adapters; the milestone is
complete.

## 9. Validation tiers

- **Tier 0 — offline / CI (`go test ./...`).** The adapter with a stub
  `CmdRunner` and a synthesized results directory (`Accepted/*.pdb` + a
  `final_design_stats.csv`): asserts the command, the injected `design_path`,
  the parsed designs (`structure_file`, `sequence`, `scores`), and every error
  path. Required for SP3 acceptance.
- **Tier 1 — real tool, CPU.** Not applicable — BindCraft is GPU/AF2-bound.
- **Tier 3 — full GPU run.** Manual; gated on the GB10 torch fix.

## 10. Acceptance criteria

1. A `design.bindcraft` request with a non-empty `settings` object writes a
   `settings.json` whose `design_path` is the adapter's persistent directory,
   builds the correct `bindcraft.py --settings` command, and — given an
   `Accepted/` directory of PDBs — returns `{"designs":[…]}` with
   `structure_file` populated.
2. When `final_design_stats.csv` is present, designs carry `sequence` and
   `scores` parsed from it and joined to the PDBs by name.
3. Generated structure files are written under `$PROTEUS_HOME/designs/` and
   outlive the job's temp `WorkDir`.
4. Missing `settings`, an uninstalled tool, an absent `alphafold_params`
   directory, and zero accepted designs each yield a clear error.
5. `go test ./...` and `go vet ./...` pass.

## 11. Out of scope

Adapter-built settings (the agent supplies the full target-settings JSON);
`--filters`/`--advanced` customization; BindCraft AF-params runtime rewiring;
`Trajectory/` or `MPNN/` intermediate outputs; per-project designs directories;
the GB10 torch fix.
