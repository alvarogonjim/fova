# Proteus — Design-tool backend wiring — Design

**Date:** 2026-05-19
**Status:** Approved, SP1 ready for planning
**Predecessor:** v0.2 "Real designs", v0.3 "Plan from target" — merged to `master`.

## 1. Problem

The agent design tools — `design.proteinmpnn`, `design.rfdiffusion`,
`design.bindcraft` — cannot run a real tool through the local backend. Two gaps:

1. **Name mismatch.** `designTool.Execute` calls `backend.Run(tool="design.proteinmpnn")`;
   `localBackend.Run` passes that to `runner.Run` → `registry.Tool("design.proteinmpnn")`.
   The `tools.toml` registry is keyed by recipe name (`proteinmpnn`), so this
   returns `unknown tool "design.proteinmpnn"`.
2. **No request/response adapter.** `localBackend.Run` writes the raw agent JSON
   and the recipe runs e.g. `protein_mpnn_run.py @request.json` — argparse's
   args-file format, not JSON. Real tools need a real invocation choreography,
   and they emit native output (FASTA, PDB, CSV), not the `{"designs":[…]}`
   JSON `designTool.persist` expects.

`go test` is green only because `internal/tools/design/design_test.go` uses a
`stubBackend` that ignores the tool name and returns a fixed fixture — the real
seam was never exercised.

## 2. Goal

Wire the design tools to actually execute real tools on the local backend and
persist real `domain.Design` rows, via a per-tool **`ToolAdapter`**. Validate in
tiers: offline-deterministic → real tool on CPU → local-LLM-driven loop → full
GPU + large model.

## 3. Decomposition

The three tools are genuinely different integrations (argparse vs Hydra vs
settings-JSON; FASTA vs PDB vs CSV; light/CPU vs GPU+weights). They share the
`ToolAdapter` foundation but are independent builds with different validation
profiles. Decomposed into three sub-projects:

| SP | Delivers | Validation profile |
|---|---|---|
| **SP1** | `ToolAdapter` foundation + the ProteinMPNN adapter | Fully validatable now — Tier 0 + Tier 1 (CPU) |
| **SP2** | RFdiffusion adapter | Tier 0 now; real run Tier 3, GB10-torch-blocked |
| **SP3** | BindCraft adapter | Tier 0 now; real run Tier 3, GB10-torch-blocked |

This document fully specs **SP1** (§5). SP2 and SP3 are outlined (§6); each gets
its own plan and a brief design pass when reached, reusing the SP1 foundation
unchanged. Build order: SP1 → SP2 → SP3.

## 4. Architecture — `ToolAdapter`

A `ToolAdapter` turns an agent design request into a real tool invocation and
the tool's native output back into the `{"designs":[…]}` schema. New code lives
in `internal/backends/local/`.

```go
// ToolAdapter runs one design tool: agent request -> real invocation ->
// {"designs":[...]} output.
type ToolAdapter interface {
	AgentTool() string // e.g. "design.proteinmpnn"
	Recipe() string    // e.g. "proteinmpnn" — the tools.toml recipe name
	Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error)
}

// AdapterEnv is everything an adapter needs to run, injected so adapters are
// unit-testable with a stub Run and a temp WorkDir.
type AdapterEnv struct {
	Recipe  ToolRecipe // resolved recipe (expanded InstallDir, VenvDir, ...)
	Run     CmdRunner  // command runner (production: bashRunner; tests: a stub)
	WorkDir string     // a fresh temp directory the adapter may write into
}
```

**Files (`internal/backends/local/`):**
- `adapter.go` — the `ToolAdapter` interface, `AdapterEnv`, the adapter registry
  (`agent tool name → ToolAdapter`), and `RunDesign(ctx, reg *Registry,
  agentTool string, request []byte) ([]byte, error)`. `RunDesign` looks up the
  adapter, resolves its recipe, creates a temp `WorkDir` (removed on return),
  builds `AdapterEnv` with the production `bashRunner`, and calls `Invoke`.
  A request for a design tool with no registered adapter returns a clear
  `"design.X: no local adapter on this backend yet"` error.
- `adapter_proteinmpnn.go` — the ProteinMPNN adapter (§5).

**Dispatch (`internal/backends/backend.go`):** `localBackend` holds the
`*local.Registry`; `localBackend.Run(ctx, tool, input)` delegates to
`local.RunDesign`. Only the three `design.*` tools ever call `backend.Run`, so
the old generic write-JSON/run-`run_command` path is removed.

**Unchanged:** `designTool` / `Execute` / `jobs.Manager` / `designTool.persist` /
the `backendOutput` `{"designs":[…]}` schema / the designs panel. The Modal
backend is untouched — `modal/functions.py` already emits the same schema, so
the backend-symmetry guarantee (SPECS §13.2) holds. The recipe `run_command`
field becomes unused for adapter-backed tools (adapters build their own
commands); it is left in `tools.toml` rather than removed.

## 5. SP1 — ProteinMPNN adapter

### 5.1 Contract

- **Agent tool:** `design.proteinmpnn`. **Recipe:** `proteinmpnn`.
- **Request** (subset of the existing `designTool` input schema):
  - `target` — filesystem path to an existing `.pdb` backbone (required).
  - `num_designs` — integer, sequences to design per backbone (default 1).
  - `hotspots` — accepted but ignored in SP1 (documented deviation).
- **Response:** the `{"designs":[…]}` schema, one entry per designed sequence.

### 5.2 Choreography (`Invoke`)

1. Parse the request; validate `target` is a non-empty path to an existing file
   ending in `.pdb`. On failure return an error — no partial work.
2. Make `<WorkDir>/inputs/` and copy the target PDB into it.
3. Run the parse step:
   `<Recipe.VenvDir>/bin/python <Recipe.InstallDir>/helper_scripts/parse_multiple_chains.py
   --input_path=<WorkDir>/inputs/ --output_path=<WorkDir>/parsed.jsonl`
4. Run inference:
   `<Recipe.VenvDir>/bin/python <Recipe.InstallDir>/protein_mpnn_run.py
   --jsonl_path <WorkDir>/parsed.jsonl --out_folder <WorkDir>
   --num_seq_per_target <num_designs> --sampling_temp 0.1 --seed 37 --batch_size 1`
5. Read every `<WorkDir>/seqs/*.fa`. ProteinMPNN writes the native sequence as
   record 0, then the designed sequences with headers containing
   `score=`, `global_score=`, `seq_recovery=`. Skip record 0.
6. For each designed record emit a design:
   `{"sequence":{"<chain>":"<seq>"}, "structure_file":"",
   "scores":{"score":<f>,"global_score":<f>,"seq_recovery":<f>}}`.
   Wrap them as `{"designs":[…]}`.

Both commands run via `env.Run` (the injected `CmdRunner`) in
`Recipe.InstallDir`. The adapter does not set a device; ProteinMPNN auto-selects
CUDA/CPU. CPU is forced by the caller's environment (`CUDA_VISIBLE_DEVICES=`),
which the production `bashRunner` inherits.

### 5.3 Error handling

- Missing/blank/non-`.pdb` `target`, or the file does not exist → error before
  any command runs.
- `proteinmpnn` not installed (no venv / install dir) → error naming the tool
  and suggesting `/install proteinmpnn`.
- Parse step or inference step fails → error naming the failing step and
  including the command's combined output (which carries the GB10 `sm_121`
  CUDA message when it occurs).
- `seqs/` produced no parseable designed sequence → error, never a silent
  empty-success.
- All errors propagate as the job's failure: the job goes `failed` and the
  message surfaces in the jobs panel and chat (existing path).

### 5.4 Files

- Create `internal/backends/local/adapter.go` — interface, `AdapterEnv`,
  registry, `RunDesign`.
- Create `internal/backends/local/adapter_proteinmpnn.go` — the adapter.
- Create `internal/backends/local/adapter_test.go` — registry / `RunDesign` /
  no-adapter-error tests.
- Create `internal/backends/local/adapter_proteinmpnn_test.go` — adapter tests
  with a stubbed `CmdRunner` and a fixture `.fa`.
- Create `internal/backends/local/testdata/proteinmpnn_sample.fa` — a fixture
  ProteinMPNN FASTA output (native + 2 designed records).
- Modify `internal/backends/backend.go` — `localBackend` holds the registry;
  `Run` delegates to `local.RunDesign`; remove the generic path.
- Modify `internal/backends/backend_test.go` — update for the new dispatch.

## 6. SP2 / SP3 — outlines

### SP2 — RFdiffusion adapter
- `design.rfdiffusion` → recipe `rfdiffusion`. New `adapter_rfdiffusion.go`.
- Invoke `scripts/run_inference.py` with Hydra args: `inference.input_pdb=<target>`,
  `inference.output_prefix=<WorkDir>/out`, `inference.num_designs=N`,
  `'contigmap.contigs=[…]'`, `inference.ckpt_override_path=<rfdiffusion_weights>`.
  Requires the 4 GB `rfdiffusion_weights` data asset.
- Output: generated backbone PDBs → designs with `structure_file` set, empty
  `sequence`.
- Validation: Tier 0 (fixture PDBs + stub runner) now; Tier 1 (CPU) impractical;
  real run is Tier 3, gated on the GB10 torch fix.

### SP3 — BindCraft adapter
- `design.bindcraft` → recipe `bindcraft`. New `adapter_bindcraft.go`.
- Translate the agent request into a BindCraft **settings JSON**, run
  `bindcraft.py --settings <WorkDir>/settings.json`. Requires the 5.3 GB
  `alphafold_params` data asset.
- Output: BindCraft results dir (PDB designs + a metrics CSV) → designs with
  `structure_file`, `sequence`, and `scores` (AF2 pLDDT / ipTM / …).
- Validation: Tier 0 (fixture results dir) now; real run Tier 3, GB10-blocked.

Each reuses the SP1 `ToolAdapter` interface unchanged; each gets its own plan.

## 7. Validation tiers

The "validate offline first, then with large models" strategy, concrete:

- **Tier 0 — offline / CI (`go test ./...`).** Adapter tested with a stubbed
  `CmdRunner` and a fixture `.fa`: asserts the parsed `{"designs":[…]}`, the
  exact command sequence built, and every error path. The `RunDesign` registry
  and no-adapter error are tested. No GPU, no network, no real tool. Required
  to pass for SP1 acceptance.
- **Tier 1 — real tool, CPU.** The real ProteinMPNN adapter against an installed
  `proteinmpnn`, on the bundled `inputs/PDB_monomers/` backbones, with
  `CUDA_VISIBLE_DEVICES=`. A documented manual check. Proves the adapter drives
  the real binary — no GPU, no LLM. Required for SP1 acceptance.
- **Tier 2 — local-LLM-driven loop.** A vLLM model drives the agent end to end:
  `plan.create` → `design.proteinmpnn` → designs appear in the panel, tools on
  CPU. Manual; validates the local model's tool-calling.
- **Tier 3 — full.** GPU tools + large model. Manual; partly blocked by the
  GB10 `sm_121` torch issue — documented, not a gate for SP1.

## 8. Acceptance criteria (SP1)

1. A `design.proteinmpnn` request with a valid local `.pdb` `target` runs
   ProteinMPNN and returns `{"designs":[…]}`; the designs are persisted and
   appear in the designs panel.
2. A `design.rfdiffusion` / `design.bindcraft` request returns a clear
   `"no local adapter on this backend yet"` error (not `unknown tool`).
3. Tier 0 tests pass under `go test ./...`; `go vet ./...` is clean.
4. Tier 1: a real CPU ProteinMPNN run through the adapter produces designs with
   `score` / `global_score` / `seq_recovery` populated (manual check).

## 9. Out of scope

RFdiffusion/BindCraft adapters (SP2/SP3); PDB-ID fetching (file path only);
`hotspots` / fixed-positions; Modal re-verification; the GB10 torch fix;
chaining `fold.esmfold` / `score.*` after design (separate agent steps).
