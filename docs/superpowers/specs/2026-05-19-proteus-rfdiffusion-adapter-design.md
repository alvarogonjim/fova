# Proteus — SP2: RFdiffusion adapter — Design

**Date:** 2026-05-19
**Status:** Approved, ready for planning
**Milestone:** Design-tool backend wiring — SP2 of 3
**Parent design:** `docs/superpowers/specs/2026-05-19-proteus-design-tool-backend-wiring-design.md` (§6 SP2 outline)
**Predecessor:** SP1 (ProteinMPNN adapter) — merged to `master`.

## 1. Goal & scope

Wire the agent's `design.rfdiffusion` tool to actually run RFdiffusion through
the local backend, as a second `ToolAdapter` following the SP1 pattern.
RFdiffusion generates protein **backbones**; the adapter passes the agent's
contig map straight through to RFdiffusion (contig pass-through).

Automated validation is **Tier 0** (stub `CmdRunner` + fixture PDBs). RFdiffusion
on CPU is impractical, so there is no Tier 1; a real GPU run is Tier 3 and is
gated on the GB10 `sm_121` torch issue — documented, not a gate for SP2.

## 2. Foundation extension — `AdapterEnv.Registry`

The RFdiffusion adapter needs two things SP1's `AdapterEnv` (`Recipe`, `Run`,
`WorkDir`) does not supply: the RFdiffusion **weights** (the `rfdiffusion_weights`
data asset) and a **persistent** output location. Add one additive field:

```go
type AdapterEnv struct {
	Recipe   ToolRecipe
	Run      CmdRunner
	WorkDir  string
	Registry *Registry // for DataAsset lookups and Home() — added in SP2
}
```

`RunDesign` already holds the `*Registry`; it passes it into `AdapterEnv`. The
SP1 ProteinMPNN adapter does not read the field, so this is non-breaking.

## 3. Contract

- **Agent tool:** `design.rfdiffusion`. **Recipe:** `rfdiffusion`.
- **Request** fields the adapter reads:
  - `contigs` — **required**, the RFdiffusion contig string (e.g.
    `A1-100/0 50-70`). The adapter wraps it as `contigmap.contigs=[<contigs>]`.
  - `target` — optional filesystem path to a target PDB. If given →
    `inference.input_pdb=<target>`.
  - `hotspots` — optional, comma-separated residues (e.g. `A30,A33`). If given →
    `ppi.hotspot_res=[<hotspots>]`.
  - `num_designs` — integer, backbones to generate (default 1) →
    `inference.num_designs=N`.
- **Response:** the `{"designs":[…]}` schema, one entry per generated backbone
  PDB — `structure_file` set to an absolute path, `sequence` and `scores` empty
  (RFdiffusion emits backbones only).

`contigs` is added to the shared `designTool.InputSchema()` (`internal/tools/
design/design.go`) as a string property so the agent knows to supply it. The
ProteinMPNN and BindCraft tools harmlessly ignore the extra advertised field.

## 4. Choreography (`Invoke`)

1. Parse the request; validate `contigs` is non-empty — fail fast otherwise.
2. If `target` is given, validate it is an existing `.pdb` file.
3. Validate `rfdiffusion` is installed — `Recipe.InstallDir` and `Recipe.VenvDir`
   both exist as directories (same check as SP1); else an error suggesting
   `/install rfdiffusion`.
4. Resolve the weights: `env.Registry.DataAsset("rfdiffusion_weights")` →
   `TargetDir`. Validate the directory exists; else an error naming
   `rfdiffusion_weights`. Pick the checkpoint: `Complex_base_ckpt.pt` if a
   `target` was given (complex/binder design), else `Base_ckpt.pt`.
5. Create the **persistent** output directory
   `<Registry.Home()>/designs/rfdiffusion-<unix-timestamp>/` and use
   `inference.output_prefix=<that>/out`.
6. Build the command:
   `<VenvDir>/bin/python <InstallDir>/scripts/run_inference.py
   inference.output_prefix=<outdir>/out inference.num_designs=<N>
   inference.ckpt_override_path=<ckpt> 'contigmap.contigs=[<contigs>]'`
   — plus `inference.input_pdb=<target>` when a target is given, and
   `'ppi.hotspot_res=[<hotspots>]'` when hotspots are given.
7. Run it via `env.Run` in `Recipe.InstallDir`.
8. Glob `<outdir>/out_*.pdb`. For each, emit a design:
   `{"sequence":{}, "structure_file":"<absolute path>", "scores":{}}`.
9. Zero output PDBs → error (never a silent empty-success). Return
   `{"designs":[…]}`.

The adapter builds the command from `Recipe.VenvDir`/`Recipe.InstallDir`; it does
not use the recipe's `run_command` template. The temp `WorkDir` is unused by
this adapter — RFdiffusion writes by `output_prefix` into the persistent dir.

## 5. Output persistence

RFdiffusion's output is the design itself (PDB backbones), and
`domain.Design.StructureFile` must point at a file that survives the job.
`RunDesign`'s `WorkDir` is a temp directory removed on return — unusable for
durable output. The adapter therefore writes structures to
`<Registry.Home()>/designs/rfdiffusion-<timestamp>/`, a persistent location
under `$PROTEUS_HOME`. A per-project designs directory is a later refinement
(v0.2/v0.3 use a single default project); a flat `$PROTEUS_HOME/designs/` is
sufficient for SP2.

## 6. Error handling

Missing/blank `contigs`; a `target` path that is not an existing `.pdb`;
`rfdiffusion` not installed; the `rfdiffusion_weights` directory absent; the
RFdiffusion run failing (error includes the command's combined output, which
carries the GB10 `sm_121` CUDA message when it occurs); zero output PDBs — each
returns a clear, step-named error. All propagate as the job's failure (job goes
`failed`, surfaced in the jobs panel and chat — existing path).

## 7. Files

- Create `internal/backends/local/adapter_rfdiffusion.go` — the adapter, its
  `init()` registration, request struct, output parser.
- Create `internal/backends/local/adapter_rfdiffusion_test.go` — Tier 0 tests
  with a stub `CmdRunner`.
- Create `internal/backends/local/testdata/rfdiffusion_sample/out_0.pdb` and
  `out_1.pdb` — minimal fixture backbone PDBs.
- Modify `internal/backends/local/adapter.go` — add `Registry` to `AdapterEnv`;
  `RunDesign` passes the registry.
- Modify `internal/tools/design/design.go` — add `contigs` to the shared
  `InputSchema()`.

## 8. Validation tiers

- **Tier 0 — offline / CI (`go test ./...`).** The adapter with a stub
  `CmdRunner` and fixture `out_*.pdb` files: asserts the command string
  (`contigs`, `input_pdb`, `hotspot_res`, `ckpt_override_path`, `output_prefix`,
  `num_designs`), the parsed designs (`structure_file` set), checkpoint
  selection (Base vs Complex), and every error path. Required for SP2 acceptance.
- **Tier 1 — real tool, CPU.** Not applicable — RFdiffusion is impractical on CPU.
- **Tier 3 — full GPU run.** Manual; gated on the GB10 torch fix.

## 9. Acceptance criteria

1. A `design.rfdiffusion` request with a non-empty `contigs` builds the correct
   `run_inference.py` command and, given output PDBs, returns `{"designs":[…]}`
   with `structure_file` populated and `sequence`/`scores` empty.
2. The checkpoint is `Complex_base_ckpt.pt` when a `target` is supplied,
   `Base_ckpt.pt` otherwise.
3. Generated structure files are written under `$PROTEUS_HOME/designs/` and
   outlive the job's temp `WorkDir`.
4. Missing `contigs`, an uninstalled tool, an absent weights directory, and
   zero output PDBs each yield a clear error.
5. `go test ./...` and `go vet ./...` pass.

## 10. Out of scope

SP3 (BindCraft); adapter-built contigs / PDB structure parsing; PDB-ID fetching;
RFdiffusion `.trb` metadata parsing into scores; per-project designs
directories; the GB10 torch fix.
