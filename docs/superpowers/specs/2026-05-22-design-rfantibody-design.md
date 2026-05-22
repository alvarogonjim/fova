# `design.rfantibody` integration + retire `design.chai2`

**Date:** 2026-05-22
**Branch:** `feat/rfantibody` (off `dev`)
**Umbrella:** `docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md`
**Tools 4 & 6 of 6.** Upstream: https://github.com/RosettaCommons/RFantibody —
Baker-lab structure-based de novo antibody / nanobody design.

## Goal

Two related changes to fova's antibody-design surface:

1. **Retire `design.chai2`.** Chai-2 is proprietary — no open weights, no code,
   no public API (early-access partner program only). fova's `design.chai2`
   is a phantom: registered and plan-accepted, but with no recipe,
   Containerfile, weights, or adapter, and no possible path to one. Remove it
   so the agent can never propose an unrunnable method.
2. **Fully integrate `design.rfantibody`.** RFantibody is the open
   (Apache-licensed, Baker-lab) antibody-design tool. It already has a
   `[tools.rfantibody]` recipe and Containerfile but **no adapter** — a
   tier-3 dead-end. This wires it end-to-end: typed schema, a new adapter
   driving the full 3-stage pipeline, `/plan` method-config, score ingestion,
   and a grounding skill.

## Branch base & platform

This work branches off `dev` (the `/plan` `MethodConfig` infrastructure and
the `internal/assets` layout live there). `design.rfantibody` is **x86-only**:
`rfantibody.Containerfile` fails the build fast on aarch64 — RFantibody's
`uv.lock` pins `dgl==2.4.0+cu118`, which publishes no aarch64 wheel. So the
GPU end-to-end run **cannot be validated on the GB10**; everything else (typed
schema, adapter logic, preflight, `/plan` wiring, unit tests) is fully
CI-verifiable on any platform.

---

## 1. Background — current state

**chai2:** `internal/tools/design/chai2.go` builds `design.chai2` on the
shared `designTool`. `MethodChai2` exists in `compat.go`; `compat.go` even
mis-describes it ("runs via chai1 weights + design head" — not how Chai-2
works). No `[tools.chai2]` recipe, no Containerfile, no weights, no adapter.

**rfantibody:** `internal/tools/design/rfantibody.go` builds `design.rfantibody`
on the shared `designTool` (generic `target/hotspots/num_designs` schema).
`[tools.rfantibody]` recipe + `rfantibody.Containerfile` + smoke test exist,
so it is installable — but `RunDesign` has no adapter, so an approved
RFantibody plan dead-ends. The generic schema cannot express RFantibody's
framework, per-CDR loop specs, or 3-stage parameters; the pLDDT/pAE scores
RF2 produces are dropped.

---

## 2. RFantibody reference (for the implementer)

### The 3-stage pipeline

RFantibody runs three console scripts from its container `.venv`, invoked
`uv run --project /opt/rfantibody <script>`:

1. **`rfdiffusion`** — antibody-finetuned RFdiffusion; generates
   antibody–target backbone complexes.
   `rfdiffusion -t <target.pdb> -f <framework.pdb> -q designs.qv -n <N> -l <design-loops> -h <hotspots> [--deterministic]`
2. **`proteinmpnn`** — designs CDR-loop sequences on those backbones.
   `proteinmpnn -q designs.qv --output-quiver sequences.qv -n <seqs> -t <temp> [--deterministic]`
3. **`rf2`** — RoseTTAFold2 structure prediction + confidence scoring.
   `rf2 -q sequences.qv --output-quiver predictions.qv -r <recycles> [-s <seed>] [--hotspot-show-prop <p>]`

Stages exchange **Quiver `.qv`** files (a database bundling many designs).
Utilities: `qvextract <file>.qv -o <dir>` (extract PDBs), `qvscorefile <file>.qv`
(emit a TSV of per-design scores).

### Inputs

- **Framework** — an HLT-format PDB: chains `H` (heavy), `L` (light), `T`
  (target), in that order, with CDR-loop residue indices in PDB Remarks.
  RFantibody bundles example frameworks inside the image:
  - nanobody — `/opt/rfantibody/scripts/examples/example_inputs/h-NbBCII10.pdb`
  - scFv — `/opt/rfantibody/scripts/examples/example_inputs/hu-4D5-8_Fv.pdb`
- **Target** — the antigen PDB, ideally truncated to ~50–60 residues around
  the epitope.
- **Hotspots** — epitope residues, e.g. `T305,T456`.
- **Design loops** — per-CDR length specs: `H1:7` (fixed 7) or `H3:5-13`
  (sampled 5–13); the six CDRs are `H1,H2,H3,L1,L2,L3`; omitted loops stay
  fixed. RFantibody has sensible defaults.

### Output & scores

Final stage produces `predictions.qv`; `qvextract` yields prediction PDBs and
`qvscorefile` a TSV. Per-design scores include **pLDDT** (0–100, higher
better) and **pAE** (Ångström, lower better). The exact TSV column names are
pinned against a real `qvscorefile` run during implementation; unknown numeric
columns are carried through as raw scores.

### Weights

RFantibody weights are install-time fetched into `~/.fova/models/rfantibody/`
and bind-mounted at `/models` (`PROTEUS_RFANTIBODY_WEIGHTS=/models`). The
adapter validates the cache with `os.Stat`; a missing cache is a
"run /install rfantibody" error, never created.

---

## 3. Part 1 — retire `design.chai2`

A mechanical removal, done by the coordinator in the Foundation (§ Plan)
before the parallel streams:

- **Delete** `internal/tools/design/chai2.go`.
- `internal/tools/plan/compat.go` — remove `MethodChai2`, its two alias-map
  entries, its entries in the application-compat slices, the `toolForMethod`
  and `installProbeKey` `case MethodChai2` blocks, and the stale
  "chai1 weights + design head" comments.
- `internal/tools/plan/plan.go` — remove `Chai2` from the two `method`
  schema-description strings (lines ~112, ~264).
- `cmd/fova/main.go` — remove the `registry.Register(designtools.NewChai2Tool(...))`
  line.
- Test references — `cmd/fova/main_test.go`, `internal/tools/design/design_test.go`,
  `internal/tools/plan/compat_test.go`, `internal/tools/plan/plan_test.go`,
  `internal/safety/guard_test.go` each mention chai2; update or drop those
  references so the build and tests stay green.
- **Keep** `domain.OriginChai2` — removing a `DesignOrigin` constant would
  break deserialization of any historical design record; it simply becomes
  unused.

---

## 4. Component A — bespoke `rfantibodyTool` + typed schema

Replace the shared `designTool` wrapper for rfantibody with a bespoke
`rfantibodyTool` in `internal/tools/design/rfantibody.go`, mirroring
`ligandMPNNTool`/`boltzGenTool`: a `RFantibodyParams = domain.RFantibodyParams`
package alias, a typed `InputSchema()`, an `Execute` that validates, resolves
workspace paths, submits the job, and `persist`s designs.
`NewRFAntibodyTool(workspaceRoot, mgr, backend, st)` keeps its signature, so
`cmd/fova/main.go` needs no change for the tool itself (only the chai2-line
removal in Part 1). `RequiresConfirmation` stays `true`.

**Schema (`RFantibodyParams`):**
- `target` — workspace path to the antigen PDB (required)
- `hotspots` — epitope residues, e.g. `T305,T456` (required)
- `framework` — enum `nanobody` (default) | `scfv` — RFantibody's bundled
  in-container example frameworks
- `framework_pdb` — optional workspace path to a user HLT-format framework
  PDB; when set, overrides `framework`
- `design_loops` — optional per-CDR loop-length spec
  (`H1:7,H2:6,H3:5-13,L1:8-13,L2:7,L3:9-11`); empty ⇒ RFantibody defaults
- `num_designs` — backbone count (rfdiffusion `-n`)
- `deterministic` — `*bool`; reproducible rfdiffusion + proteinmpnn
- `seqs_per_struct` — proteinmpnn sequences per backbone (`-n`)
- `temperature` — `*float64`; proteinmpnn sampling temperature (`-t`)
- `num_recycles` — `*int`; rf2 recycles (`-r`)
- `seed` — `*int`; rf2 seed (`-s`)
- `hotspot_show_prop` — `*float64`; rf2 `--hotspot-show-prop`

---

## 5. Component B — `/plan` integration

`RFantibodyParams` + `RFantibodyParams.Validate()` in `internal/domain`
(committed in the Foundation). `MethodConfig` gains an
`RFantibody *RFantibodyParams` field, alongside `BoltzGen` and `LigandMPNN`.

`plan.create` (`internal/tools/plan/plan.go`): for `MethodRFantibody`, accept
the `method_params` object, unmarshal it into `RFantibodyParams`, run
`Validate()`, and reject the plan on failure — exactly the LigandMPNN
method-config pattern.

`/plan` rendering (`render.go` + `internal/tui/plan.go`): an RFantibody section
in the `MethodConfig` dispatch (BoltzGen / LigandMPNN / RFantibody) showing
framework, target, hotspots, num_designs, and the preflight result. `/plan
approve` re-runs preflight.

`MethodRFantibody` and its aliases already exist in `compat.go` — no compat
change is needed for rfantibody.

---

## 6. Component C — adapter (`adapter_rfantibody.go`)

A brand-new `ToolAdapter`. It:

- unmarshals the typed request;
- resolves the **framework**: the `nanobody`/`scfv` enum → the bundled
  in-container path; a `framework_pdb` → staged into the container workdir;
- stages the target PDB into the workdir;
- writes a **driver script** that runs the full 3-stage pipeline in one
  container invocation — `rfdiffusion` → `proteinmpnn` → `rf2`, each
  `uv run --project /opt/rfantibody <stage>` with the mapped flags, then
  `qvextract predictions.qv` and `qvscorefile predictions.qv`;
- runs the container with the entrypoint overridden to the driver script
  (the image ENTRYPOINT is `uv run … rfdiffusion`, so a multi-stage driver
  needs the override — `ContainerRunArgs` gains an `Entrypoint` field if it
  lacks one; see Risks);
- validates runtime, image, and the `os.Stat` weights cache;
- collects the extracted prediction PDBs + the score TSV into the
  `{"designs":[...]}` envelope.

---

## 7. Component D — preflight

`RFantibodyParams.Validate()` (value-shape, no filesystem) plus the tool's
`Execute` / the adapter's `Invoke` (path existence). Checks:

- `target` non-empty; `hotspots` non-empty and well-formed
  `<chain><residue>` tokens;
- `framework` is `""`/`nanobody`/`scfv`, **or** `framework_pdb` is set;
- `design_loops`, when set, parses — each token `<CDR>:<len>` or
  `<CDR>:<min>-<max>`, `<CDR>` ∈ `{H1,H2,H3,L1,L2,L3}`, `min ≤ max`;
- numeric params positive; `hotspot_show_prop` in `[0,1]`;
- (Execute/Invoke) `target` and any `framework_pdb` resolve inside the
  workspace and exist; the image and weights cache are present.

---

## 8. Component E — score ingestion

The adapter parses the `qvscorefile` TSV: per final RF2 prediction, the
numeric columns become `Design.Scores` — `plddt`, `pae` mapped to standard
keys, other numeric columns carried through. The extracted prediction PDB is
`StructureFile`; the designed antibody H/L chain sequences are
`Design.Sequence`. One fova `Design` per final prediction, persisted with
`Origin OriginRFAntibody`, `Application AppAntibody`. A prediction with no
score row yields an empty `Scores` map, not a failure.

---

## 9. Component F — grounding skill

New `internal/assets/embed/skills/rfantibody-design.md` (with `name` +
`description` frontmatter): the nanobody-vs-scFv framework choice; epitope and
hotspot selection (the README's "> ~3 hydrophobic residues, ~10 Å context,
avoid glycosylation" guidance, and that RFantibody is *sensitive* to hotspot
choice); per-CDR loop-length specs and when to widen H3; the 3-stage pipeline;
reading pLDDT / pAE (RF2 pAE < 10 as a useful filter).

---

## 10. Testing

**Unit:** schema; the 3-stage flag mapping per stage (table tests); the
framework-resolution (enum → bundled path, `framework_pdb` → staged);
`qvscorefile`-TSV → `Scores` parsing against a fixture TSV; preflight (valid /
invalid table, including `design_loops` parsing); `plan.create` RFantibody
method-config; `/plan` RFantibody-section rendering; the chai2-retirement
leaves the build + all tests green.

**Container:** the existing install-time smoke test (`rfdiffusion --help`)
stays valid.

**End-to-end:** **x86-only** — the full GPU pipeline cannot run on the GB10.
It is validated on an x86 GPU box (or the Modal backend) when one is
available; the umbrella's batched GB10 validation does not cover it.

Every phase ends `go build ./...` and `go test ./...` green.

---

## 11. Phasing

1. **Foundation (coordinator):** Part 1 chai2 retirement; `RFantibodyParams`
   + `MethodConfig.RFantibody` + `Validate()` in `internal/domain`. Build +
   tests green.
2. **Streams (parallel):** A bespoke `rfantibodyTool`; B adapter; C `/plan`
   integration; D grounding skill.
3. **Integration:** merge the streams; build + full suite + gofmt green.

---

## 12. Files touched

**Foundation:** `internal/tools/design/chai2.go` (delete); `internal/tools/plan/compat.go`;
`internal/tools/plan/plan.go` (chai2 strings); `cmd/fova/main.go` (chai2 line);
the five chai2-referencing test files; `internal/domain/types.go` +
`internal/domain/rfantibody.go` (new — `RFantibodyParams`, `Validate`).

**Streams:** `internal/tools/design/rfantibody.go` + `rfantibody_test.go` +
`internal/tools/design/design_test.go` (drop the rfantibody designTool row);
`internal/backends/local/adapter_rfantibody.go` + test (and possibly an
`Entrypoint` field in `internal/backends/local/runtime_exec.go`);
`internal/tools/plan/plan.go` + `render.go` + tests, `internal/tui/plan.go`;
`internal/assets/embed/skills/rfantibody-design.md`.

---

## 13. Out of scope

- Modal-backend execution (the local backend only — though Modal is the
  x86 path RFantibody's Containerfile points to; deferred).
- The editable confirmation surface (its own cross-cutting spec).
- The optional external Rosetta `ddG` filter.
- `qvsplit` / multi-run Quiver merging.

---

## 14. Risks

- **x86-only.** RFantibody cannot be GPU-validated on the GB10. The
  integration is fully CI-verifiable; the end-to-end run waits for an x86 GPU
  box. This is a known, accepted constraint.
- **Entrypoint override.** The 3-stage driver needs the container entrypoint
  overridden (the image ENTRYPOINT is stage 1 only). If `ContainerRunArgs`
  has no `Entrypoint` field, the adapter work adds one — a small, additive
  change to `runtime_exec.go`; it must not disturb the other adapters, which
  pass no entrypoint and keep the image default.
- **Quiver score-TSV columns.** The exact `qvscorefile` column names are
  pinned against a real run during implementation; the parser keys by name
  and carries unknown numeric columns through rather than failing.
- **chai2 test references span five packages.** The Foundation must update
  every one so the build and `go test ./...` stay green before the streams
  start.
