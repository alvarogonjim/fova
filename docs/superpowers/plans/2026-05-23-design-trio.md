# `design.{rfdiffusion,proteinmpnn,bindcraft}` Trio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring all three remaining "umbrella follow-up" design tools to bespoke parity with the new pattern (boltzgen / ligandmpnn / rfantibody): typed full-surface schemas, `/plan` `MethodConfig` integration, grounding skills, preserved score ingestion.

**Architecture:** A coordinator **Foundation** (3 domain structs + `MethodConfig` fields + `Validate`, plus `/plan` integration for all three) is committed first. Then three file-disjoint streams build in parallel — one per tool, each owning the tool file + adapter rework + grounding skill.

**Tech Stack:** Go 1.22+, `tools.Tool`, `jobs.Manager`, the local container backend, RFdiffusion v1 / ProteinMPNN / BindCraft (each via its installed image).

**Spec:** `docs/superpowers/specs/2026-05-23-design-trio-design.md`

---

## Parallel execution model

The **Foundation** is committed on `feat/design-trio` by the coordinator before any stream starts. Each stream then runs in its own worktree off the foundation commit:

| Stream | Branch | Files (exclusive) |
|---|---|---|
| A — rfdiffusion | `trio/rfdiffusion` | `internal/tools/design/rfdiffusion.go`, `internal/tools/design/rfdiffusion_test.go` (new), `internal/backends/local/adapter_rfdiffusion.go`, `internal/backends/local/adapter_rfdiffusion_test.go`, `internal/assets/embed/skills/rfdiffusion-design.md` (new) |
| B — proteinmpnn | `trio/proteinmpnn` | `internal/tools/design/proteinmpnn.go`, `internal/tools/design/proteinmpnn_test.go` (new), `internal/backends/local/adapter_proteinmpnn.go`, `internal/backends/local/adapter_proteinmpnn_test.go`, `internal/assets/embed/skills/proteinmpnn-design.md` (new) |
| C — bindcraft | `trio/bindcraft` | `internal/tools/design/bindcraft.go`, `internal/tools/design/bindcraft_test.go` (new), `internal/backends/local/adapter_bindcraft.go`, `internal/backends/local/adapter_bindcraft_test.go`, `internal/assets/embed/skills/bindcraft-design.md` (new) |

No file appears twice. The Foundation is committed first, so every stream's package compiles and tests standalone. `cmd/fova/main.go` is **not touched** — every `NewXTool` signature stays.

Each stream agent runs `go build ./...` and `go test ./<its package>` green before its final commit.

---

## THE CONTRACT — three `domain.XParams` types

Defined once in the Foundation; imported (not copied) by every stream.

### `RFdiffusionParams` (`internal/domain/rfdiffusion.go`)

```go
type RFdiffusionParams struct {
	Target            string   `json:"target,omitempty"`           // workspace path to target PDB; empty = unconditional
	Hotspots          string   `json:"hotspots,omitempty"`         // "A30,A33"
	Contigs           string   `json:"contigs"`                    // required, RFdiffusion contig string
	NumDesigns        int      `json:"num_designs,omitempty"`
	Deterministic    *bool     `json:"deterministic,omitempty"`
	Symmetric        *bool     `json:"symmetric,omitempty"`
	SymmetryKind      string   `json:"symmetry_kind,omitempty"`    // cyclic|dihedral|tetrahedral|octahedral|icosahedral
	NChains           int      `json:"n_chains,omitempty"`
	PartialT          int      `json:"partial_t,omitempty"`        // partial-diffusion start step
	NoiseScaleCA     *float64  `json:"noise_scale_ca,omitempty"`
	NoiseScaleFrame  *float64  `json:"noise_scale_frame,omitempty"`
	GuidingPotentials []string `json:"guiding_potentials,omitempty"`
	GuideScale       *float64  `json:"guide_scale,omitempty"`
}
```

### `ProteinMPNNParams` (`internal/domain/proteinmpnn.go`)

```go
type ProteinMPNNParams struct {
	PDB             string   `json:"pdb"`                            // workspace path (required)
	NumDesigns      int      `json:"num_designs,omitempty"`          // → --num_seq_per_target
	BatchSize       int      `json:"batch_size,omitempty"`
	SamplingTemp   *float64  `json:"sampling_temp,omitempty"`
	Seed           *int      `json:"seed,omitempty"`
	ChainsToDesign  string   `json:"chains_to_design,omitempty"`     // → --chain_id_jsonl (fova generates the JSONL)
	FixedPositions  string   `json:"fixed_positions,omitempty"`      // workspace path to a JSONL
	OmitAAs         string   `json:"omit_AAs,omitempty"`             // inline letters
	BiasAA          string   `json:"bias_AA,omitempty"`              // workspace path to a JSONL — NOT inline (unlike LigandMPNN)
	BiasByResidue   string   `json:"bias_by_residue,omitempty"`      // workspace path to a JSONL
	TiedPositions   string   `json:"tied_positions,omitempty"`       // workspace path to a JSONL
	SaveScore      *bool     `json:"save_score,omitempty"`
}
```

### `BindCraftParams` (`internal/domain/bindcraft.go`)

```go
type BindCraftParams struct {
	BinderName            string `json:"binder_name,omitempty"`
	StartingPDB           string `json:"starting_pdb"`               // workspace path (required)
	Chains                string `json:"chains"`                     // target chain(s), required
	TargetHotspotResidues string `json:"target_hotspot_residues"`    // "A30,A33" (required)
	LengthMin             int    `json:"length_min"`                 // required, ≥1
	LengthMax             int    `json:"length_max"`                 // required, ≥LengthMin
	NumberOfFinalDesigns  int    `json:"number_of_final_designs,omitempty"`
	BinderChain           string `json:"binder_chain,omitempty"`     // default "B"
	DesignRuns            int    `json:"design_runs,omitempty"`
	ProtocolName          string `json:"protocol_name,omitempty"`
	TemplatePDB           string `json:"template_pdb,omitempty"`     // workspace path
	OmitAAs               string `json:"omit_aas,omitempty"`
}
```

### `MethodConfig` after Foundation (`internal/domain/types.go`)

```go
type MethodConfig struct {
	SpecPath    string             `json:"spec_path,omitempty"`
	BoltzGen    *BoltzGenParams    `json:"boltzgen,omitempty"`
	LigandMPNN  *LigandMPNNParams  `json:"ligandmpnn,omitempty"`
	RFantibody  *RFantibodyParams  `json:"rfantibody,omitempty"`
	RFdiffusion *RFdiffusionParams `json:"rfdiffusion,omitempty"`
	ProteinMPNN *ProteinMPNNParams `json:"proteinmpnn,omitempty"`
	BindCraft   *BindCraftParams   `json:"bindcraft,omitempty"`
}
```

(After B's `feat/rfdiffusion2` merges into `dev`, that branch will have added `RFdiffusion2 *RFdiffusion2Params` too. The rebase resolves by accepting both sides — additive.)

### Per-tool mapping summaries

**RFdiffusion — Hydra `key=value` overrides** (fova owns `inference.output_prefix` + ckpt auto-pick):

```
target            → inference.input_pdb=<path>
hotspots          → 'ppi.hotspot_res=[<csv>]'
contigs           → 'contigmap.contigs=[<contigs>]'
num_designs       → inference.num_designs=<n>
deterministic     → inference.deterministic=<bool>
symmetric         → inference.symmetric=<bool>
symmetry_kind     → symmetry.symmetry_kind=<kind>
n_chains          → symmetry.n_chains=<n>
partial_t         → diffuser.partial_T=<n>
noise_scale_ca    → diffuser.noise_scale_ca=<f>
noise_scale_frame → diffuser.noise_scale_frame=<f>
guiding_potentials→ 'potentials.guiding_potentials=[<csv>]'
guide_scale       → potentials.guide_scale=<f>
```

**ProteinMPNN — `protein_mpnn_run.py` flags** (fova owns `--jsonl_path` + `--out_folder`; defaults: `sampling_temp 0.1`, `seed 37`, `batch_size 1`, `num_seq_per_target 1` when unset):

```
pdb               → input PDB staged into inputs/ + parsed via parse_multiple_chains.py
num_designs       → --num_seq_per_target <n>
batch_size        → --batch_size <n>
sampling_temp     → --sampling_temp <f>
seed              → --seed <n>
chains_to_design  → --chain_id_jsonl <generated JSONL>
fixed_positions   → --fixed_positions_jsonl <staged path>
omit_AAs          → --omit_AAs "<letters>"
bias_AA           → --bias_AA_jsonl <staged path>           (NOTE: file, not inline)
bias_by_residue   → --bias_by_res_jsonl <staged path>
tied_positions    → --tied_positions_jsonl <staged path>
save_score        → --save_score 1                          (when true)
```

**BindCraft — typed JSON compilation.** fova writes `settings.json` to the workdir from `BindCraftParams`; zero/empty fields are omitted so BindCraft applies its own defaults. Exact JSON:

```json
{
  "binder_name": "<BinderName>",
  "starting_pdb": "<staged StartingPDB>",
  "chains": "<Chains>",
  "target_hotspot_residues": "<TargetHotspotResidues>",
  "lengths": [<LengthMin>, <LengthMax>],
  "number_of_final_designs": <N>,
  "binder_chain": "<BinderChain or \"B\">",
  "design_runs": <DesignRuns>,
  "protocol_name": "<ProtocolName>",
  "template_pdb": "<staged TemplatePDB>",
  "omit_AAs": "<OmitAAs>"
}
```

### Score keys

No new score parsers. Existing parsers preserved unchanged:
- BindCraft: `final_design_stats.csv` parser.
- ProteinMPNN: FASTA-header `score`/`global_score`/`seq_recovery`.
- RFdiffusion: no native scores — designs land with empty `Scores` (agent runs a refold for scoring).

---

# FOUNDATION (coordinator — two commits, before streams start)

### Commit 1 — domain (`internal/domain/`)

Create three new files. Each carries the typed struct from THE CONTRACT plus a `Validate() error` (value-shape only, no filesystem). Patterns mirror `internal/domain/ligandmpnn.go` and `internal/domain/rfantibody.go`.

**`internal/domain/rfdiffusion.go`:**
- The `RFdiffusionParams` struct.
- `validateRFdiffusionSymmetryKinds = map[string]bool{"cyclic": true, "dihedral": true, "tetrahedral": true, "octahedral": true, "icosahedral": true}`.
- `Validate()`: `Contigs` non-empty; if `Symmetric` is true, `SymmetryKind` is in the map AND `NChains > 0`; `NumDesigns ≥ 0`; `PartialT ≥ 0`; if `NoiseScaleCA` / `NoiseScaleFrame` / `GuideScale` set, `> 0`; each `GuidingPotentials` entry non-empty; if `Hotspots` non-empty, each comma-separated token matches `^[A-Za-z][0-9]+$`.

**`internal/domain/proteinmpnn.go`:**
- The `ProteinMPNNParams` struct.
- `Validate()`: `PDB` non-empty; `NumDesigns`/`BatchSize` non-negative; if `SamplingTemp` set, `> 0`; if `Seed` set, `≥ 0`; `OmitAAs` is one-letter codes only (regex `^[A-Za-z]*$`); the JSONL-path fields, when set, are non-empty strings (existence is checked in `Execute`).

**`internal/domain/bindcraft.go`:**
- The `BindCraftParams` struct.
- `Validate()`: `StartingPDB`, `Chains`, `TargetHotspotResidues` non-empty; each comma-separated hotspot token matches `^[A-Za-z][0-9]+$`; `LengthMin ≥ 1`; `LengthMax ≥ LengthMin`; `NumberOfFinalDesigns ≥ 0`; if `ProtocolName` is set, it's one of `{"beta_only", "ss_only", "fixed_seq"}` (and a small map declared next to it).

Plus three `_test.go` files — table tests for each `Validate` (one valid case + 4-6 invalid cases each, mirroring `internal/domain/ligandmpnn_test.go` exactly).

**`internal/domain/types.go`:** extend `MethodConfig` with the three new fields per THE CONTRACT.

**Verify + commit:**
```
go build ./... && go test ./internal/domain/
git add internal/domain/{types.go,rfdiffusion.go,rfdiffusion_test.go,proteinmpnn.go,proteinmpnn_test.go,bindcraft.go,bindcraft_test.go}
git commit -m "feat(domain): RFdiffusion/ProteinMPNN/BindCraft Params + Validate + MethodConfig"
```

### Commit 2 — `/plan` integration (`internal/tools/plan/` + `design_test.go`)

In `plan.go`: add three methods on `*CreateTool`, each mirroring `applyLigandMPNNMethodConfig`:

```go
func (t *CreateTool) applyRFdiffusionMethodConfig(input json.RawMessage, p *domain.DesignPlan) error {
    var envelope struct{ Params *domain.RFdiffusionParams `json:"method_params"` }
    if err := json.Unmarshal(input, &envelope); err != nil { return fmt.Errorf("plan.create: invalid method_params: %w", err) }
    if envelope.Params == nil { return fmt.Errorf("plan.create: method RFdiffusion requires method_params (at minimum a contigs string)") }
    if err := envelope.Params.Validate(); err != nil { return err }
    p.MethodConfig = &domain.MethodConfig{RFdiffusion: envelope.Params}
    return nil
}

func (t *CreateTool) applyProteinMPNNMethodConfig(input json.RawMessage, p *domain.DesignPlan) error { /* same shape, ProteinMPNN/pdb */ }
func (t *CreateTool) applyBindCraftMethodConfig(input json.RawMessage, p *domain.DesignPlan) error { /* same shape, BindCraft/starting_pdb+chains+target_hotspot_residues */ }
```

In `plan.create`, alongside the existing `MethodBoltzGen` / `MethodLigandMPNN` / `MethodRFantibody` blocks, add three more:

```go
if method == MethodRFdiffusion { if err := t.applyRFdiffusionMethodConfig(input, &p); err != nil { return tools.Result{}, err } }
if method == MethodProteinMPNN { if err := t.applyProteinMPNNMethodConfig(input, &p); err != nil { return tools.Result{}, err } }
if method == MethodBindCraft   { if err := t.applyBindCraftMethodConfig(input, &p);   err != nil { return tools.Result{}, err } }
```

In `render.go`, add three sections mirroring `renderLigandMPNNSection` (header line + `labelRow` lines for the key fields), and three new cases in the `MethodConfig` render dispatch (alongside the existing BoltzGen / LigandMPNN / RFantibody cases):

```go
case mc.RFdiffusion != nil: renderRFdiffusionSection(&b, mc.RFdiffusion)
case mc.ProteinMPNN != nil: renderProteinMPNNSection(&b, mc.ProteinMPNN)
case mc.BindCraft   != nil: renderBindCraftSection(&b, mc.BindCraft)
```

Each `renderXSection` shows the same shape of fields shown in `renderLigandMPNNSection` — what the agent proposed + the typed values. ~10-20 lines per section.

In `plan_test.go` and `render_test.go`: three table-style tests covering positive-case + missing-required-field for each method-config; one render test per tool checking the section header + key fields appear. The test `TestPlanCreateAcceptsCompatibleApplicationMethod` already special-cases BoltzGen/LigandMPNN/RFantibody via an `extra` switch — add the analogous `method_params` clauses for `MethodRFdiffusion` (minimal `{"contigs":"50-100"}`), `MethodProteinMPNN` (`{"pdb":"bb.pdb"}`), `MethodBindCraft` (`{"starting_pdb":"t.pdb","chains":"A","target_hotspot_residues":"A30","length_min":80,"length_max":120}`).

**`internal/tools/design/design_test.go`:** the existing `TestDesignToolSchemaAdvertisesSettings` asserts an opaque `settings` field on BindCraft's schema. After bespoke BindCraft, that field is gone. **Remove the test** — the new typed schema is covered by Stream C's `bindcraft_test.go`.

**Verify + commit:**
```
go build ./... && go test ./internal/tools/plan/ ./internal/tools/design/ ./internal/tui/
git add internal/tools/plan/{plan,plan_test,render,render_test}.go internal/tools/design/design_test.go
git commit -m "feat(plan): RFdiffusion/ProteinMPNN/BindCraft method-config + render dispatch"
```

---

# STREAM A — bespoke `rfdiffusionTool` + adapter rework

Worktree `trio/rfdiffusion`. Packages `internal/tools/design` + `internal/backends/local`. Read `internal/tools/design/ligandmpnn.go` (the bespoke-tool template) and `internal/backends/local/adapter_rfdiffusion.go` (the current Hydra-override adapter you're extending).

### Task A1: Bespoke tool — type, schema, interface methods

**Files:**
- Modify: `internal/tools/design/rfdiffusion.go` (replace entirely)
- Test: `internal/tools/design/rfdiffusion_test.go` (create)

- [ ] **Step 1: Write the failing test**

```go
package design

import (
	"encoding/json"
	"testing"
)

func TestRFdiffusionToolSchema(t *testing.T) {
	tool := NewRFdiffusionTool("/ws", nil, nil, nil)
	if tool.Name() != "design.rfdiffusion" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{
		"target", "hotspots", "contigs", "num_designs", "deterministic",
		"symmetric", "symmetry_kind", "n_chains", "partial_t",
		"noise_scale_ca", "guiding_potentials", "guide_scale",
	} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestRFdiffusionToolRequiresConfirmation(t *testing.T) {
	if !NewRFdiffusionTool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.rfdiffusion must require confirmation — GPU design job")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestRFdiffusionTool`
Expected: FAIL — `NewRFdiffusionTool` still returns the shared `*designTool`.

- [ ] **Step 3: Write the implementation**

Replace the whole of `internal/tools/design/rfdiffusion.go`, mirroring `internal/tools/design/ligandmpnn.go`. Define `type RFdiffusionParams = domain.RFdiffusionParams`; an `rfdiffusionTool` struct with `workspaceRoot`/`mgr`/`backend`/`store`; `NewRFdiffusionTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *rfdiffusionTool` (**signature unchanged**).

Interface methods:
- `Name()` → `"design.rfdiffusion"`.
- `Description()` → "Generate de novo protein backbones with RFdiffusion against a target or unconditionally; runs as an async GPU job."
- `InputSchema()` → `type: object`, `required: ["contigs"]`, `properties` for every `RFdiffusionParams` field; `symmetry_kind` an enum `[cyclic, dihedral, tetrahedral, octahedral, icosahedral]`; bounded numerics carry `minimum`; each property a `description`.
- `RequiresConfirmation` → `true`; `EstimatedCostUSD` → `2.0`; `EstimatedDuration` → `20 * time.Minute`.
- `Execute` — stub `return tools.Result{}, nil` (real body in A2).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/ -run TestRFdiffusionTool` and `go vet ./internal/tools/design/`
Expected: PASS; package compiles.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/rfdiffusion.go internal/tools/design/rfdiffusion_test.go
git commit -m "feat(design.rfdiffusion): bespoke tool type and typed schema"
```

### Task A2: Execute — validate, resolve paths, submit, persist

**Files:**
- Modify: `internal/tools/design/rfdiffusion.go`
- Test: `internal/tools/design/rfdiffusion_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRFdiffusionExecuteRejectsBadInput(t *testing.T) {
	tool := NewRFdiffusionTool(t.TempDir(), nil, nil, nil)
	// Missing contigs — Validate rejects before any job/store access.
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected a validation error when contigs is missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestRFdiffusionExecute`
Expected: FAIL — `Execute` is the A1 stub.

- [ ] **Step 3: Write the implementation**

Implement `Execute` mirroring `ligandMPNNTool.Execute`:
1. `json.Unmarshal` input into `RFdiffusionParams`; wrap parse errors.
2. `params.Validate()` — return its error directly on failure.
3. Resolve workspace paths when `t.workspaceRoot != ""`: rewrite `Target` (if non-empty) via `tools.ResolveWorkspacePath`; resolve errors return `fmt.Errorf("design.rfdiffusion: %w", err)`.
4. Re-marshal the resolved params.
5. Submit the job (`jobs.Spec{Kind: domain.JobCompute, Tool: "design.rfdiffusion", Backend: t.backend.Name(), Input: resolved, Run: …}`); `Run` calls `t.backend.Run`, ticks `progress(0.95)`, then `t.persist(out)`.
6. `persist` — copy `ligandMPNNTool.persist` but with `Origin: domain.OriginRFDiffMPNN`, `Application: domain.AppBinder`, tool name `"design.rfdiffusion"`.
7. Return `tools.Result{JobID, Display: "started design.rfdiffusion job " + string(jobID) + " — poll jobs.result for the backbones", Provenance: domain.NewToolCallRef("design.rfdiffusion", input)}`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/rfdiffusion.go internal/tools/design/rfdiffusion_test.go
git commit -m "feat(design.rfdiffusion): Execute with validation, path resolution, persist"
```

### Task A3: Adapter rework — typed request, extended Hydra overrides

**Files:**
- Modify: `internal/backends/local/adapter_rfdiffusion.go`
- Modify: `internal/backends/local/adapter_rfdiffusion_test.go`

The current adapter takes `rfdiffusionRequest{Contigs, Target, Hotspots, NumDesigns}` and builds 3-4 Hydra overrides. Rework: take `domain.RFdiffusionParams` and emit the full mapping table from THE CONTRACT. Preserve container-mode + venv-mode + the ckpt auto-pick.

- [ ] **Step 1: Write the failing test**

```go
func TestRFdiffusionArgs(t *testing.T) {
	det := true
	sym := true
	gs := 5.0
	got := strings.Join(rfdiffusionArgs(domain.RFdiffusionParams{
		Target: "/work/t.pdb", Hotspots: "A30,A33", Contigs: "50-100",
		NumDesigns: 8, Deterministic: &det,
		Symmetric: &sym, SymmetryKind: "cyclic", NChains: 4,
		PartialT: 12, GuidingPotentials: []string{"binder_ROG"}, GuideScale: &gs,
	}, "/models/Base_ckpt.pt"), " ")
	for _, want := range []string{
		"inference.input_pdb=/work/t.pdb",
		"inference.num_designs=8",
		"'ppi.hotspot_res=[A30,A33]'",
		"'contigmap.contigs=[50-100]'",
		"inference.ckpt_override_path=/models/Base_ckpt.pt",
		"inference.deterministic=true",
		"inference.symmetric=true",
		"symmetry.symmetry_kind=cyclic",
		"symmetry.n_chains=4",
		"diffuser.partial_T=12",
		"'potentials.guiding_potentials=[binder_ROG]'",
		"potentials.guide_scale=5",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args missing %q in %q", want, got)
		}
	}
	// Unset optionals omit their overrides.
	if strings.Contains(strings.Join(rfdiffusionArgs(domain.RFdiffusionParams{Contigs: "X"}, "/m/c.pt"), " "), "noise_scale") {
		t.Error("unset noise_scale must omit the override")
	}
}
```

Add `domain` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestRFdiffusionArgs`
Expected: FAIL — `rfdiffusionArgs` undefined (current adapter inlines the override list).

- [ ] **Step 3: Write the implementation**

Replace `rfdiffusionRequest` with `type rfdiffusionRequest = domain.RFdiffusionParams` (import `domain`). Extract a table-driven `rfdiffusionArgs(p domain.RFdiffusionParams, ckpt string) []string` that builds all Hydra overrides per the CONTRACT's mapping. Each field is emitted only when non-zero / non-nil (pointer bools emit `true`/`false`; floats use `strconv.FormatFloat(v, 'g', -1, 64)`; ints `strconv.Itoa`; string slices join with `,` and wrap in `'…=[…]'`).

Rework `Invoke` to:
1. Unmarshal the request into `domain.RFdiffusionParams`.
2. `Contigs` non-empty (defensive backstop); `Target` (when set) must end `.pdb` and `os.Stat`-exist; existing weights-cache + container-runtime guards kept.
3. Pick the ckpt path (`/models/Base_ckpt.pt` if `Target` is empty, `/models/Complex_base_ckpt.pt` otherwise) — same logic as today.
4. Stage `Target` into the workdir (when set) and rewrite the param to `/work/<base>`.
5. Build `args := append(rfdiffusionArgs(req, ckpt), "inference.output_prefix=/work/out")` and run the container (Cmd = args, image ENTRYPOINT is `python /opt/rfdiffusion/scripts/run_inference.py`).
6. Collect backbones via the existing `parseRFdiffusionOutput` (unchanged — still no scores).

The venv-mode fallback is updated in the same way: use `rfdiffusionArgs` to build the command-line, prepending the python+script paths.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/ -run 'TestRFdiffusion'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_rfdiffusion.go internal/backends/local/adapter_rfdiffusion_test.go
git commit -m "feat(rfdiffusion adapter): typed request + full Hydra-override mapping"
```

### Task A4: Grounding skill

**Files:**
- Create: `internal/assets/embed/skills/rfdiffusion-design.md`

- [ ] **Step 1: Write the skill**

~90–120 lines, house style, YAML frontmatter (`name: rfdiffusion-design`, one-line `description`). Concrete `design.rfdiffusion` JSON examples matching THE CONTRACT. Cover: the contigs syntax (chains/lengths/gaps, e.g. `"A1-100/0 60-80"` for binder-against-target, `"50-100"` for unconditional length range); hotspots; the unconditional vs target-conditioned distinction (and the implicit ckpt switch); when to use `partial_t` (re-diffuse from a starting motif); when to set `symmetric`/`symmetry_kind`/`n_chains`; potentials (`binder_ROG`, `monomer_ROG`, `interface_ncontacts`) and `guide_scale`; **the absence of native scores** — RFdiffusion emits backbones only, so the agent must run a fold (`fold.boltz2` / `fold.chai1`) on the backbones to rank them. One worked binder-against-target example, one unconditional symmetric example.

- [ ] **Step 2: Verify it loads**

Run: `go test ./internal/assets/` and `go build ./...`
Expected: build clean; the assets loader picks up the new skill (NOTE: an assets-test asserts the skill count — if it now fails, that is expected; do NOT edit any `_test.go` file — coordinator fixes at integration).

- [ ] **Step 3: Commit**

```bash
git add internal/assets/embed/skills/rfdiffusion-design.md
git commit -m "docs(design.rfdiffusion): rfdiffusion-design grounding skill"
```

---

# STREAM B — bespoke `proteinMPNNTool` + adapter rework

Worktree `trio/proteinmpnn`. Read `internal/tools/design/ligandmpnn.go` and `internal/backends/local/adapter_proteinmpnn.go`.

### Task B1: Bespoke tool — type, schema, interface methods

**Files:**
- Modify: `internal/tools/design/proteinmpnn.go` (replace entirely)
- Test: `internal/tools/design/proteinmpnn_test.go` (create)

- [ ] **Step 1: Write the failing test**

```go
package design

import (
	"encoding/json"
	"testing"
)

func TestProteinMPNNToolSchema(t *testing.T) {
	tool := NewProteinMPNNTool("/ws", nil, nil, nil)
	if tool.Name() != "design.proteinmpnn" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{
		"pdb", "num_designs", "batch_size", "sampling_temp", "seed",
		"chains_to_design", "fixed_positions", "omit_AAs",
		"bias_AA", "bias_by_residue", "tied_positions", "save_score",
	} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestProteinMPNNToolRequiresConfirmation(t *testing.T) {
	if !NewProteinMPNNTool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.proteinmpnn must require confirmation")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestProteinMPNNTool`
Expected: FAIL — `NewProteinMPNNTool` still returns the shared `*designTool`.

- [ ] **Step 3: Write the implementation**

Replace the whole of `internal/tools/design/proteinmpnn.go`, mirroring `internal/tools/design/ligandmpnn.go`. Define `type ProteinMPNNParams = domain.ProteinMPNNParams`; a `proteinMPNNTool` struct; `NewProteinMPNNTool(workspaceRoot, mgr, backend, st) *proteinMPNNTool` (signature unchanged).

Interface methods:
- `Name()` → `"design.proteinmpnn"`.
- `Description()` → "Design protein sequences for a fixed backbone with ProteinMPNN; runs as an async GPU job."
- `InputSchema()` → `type: object`, `required: ["pdb"]`, `properties` for every `ProteinMPNNParams` field; each carries a `description`; the bias_AA description must explicitly note "workspace path to a JSONL — file, not inline (unlike LigandMPNN's bias_AA)".
- `RequiresConfirmation` → `true`; `EstimatedCostUSD` → `1.0`; `EstimatedDuration` → `10 * time.Minute`.
- `Execute` — stub `return tools.Result{}, nil` (A2).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/ -run TestProteinMPNNTool` and `go vet ./internal/tools/design/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/proteinmpnn.go internal/tools/design/proteinmpnn_test.go
git commit -m "feat(design.proteinmpnn): bespoke tool type and typed schema"
```

### Task B2: Execute — validate, resolve paths, submit, persist

**Files:**
- Modify: `internal/tools/design/proteinmpnn.go`
- Test: `internal/tools/design/proteinmpnn_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestProteinMPNNExecuteRejectsBadInput(t *testing.T) {
	tool := NewProteinMPNNTool(t.TempDir(), nil, nil, nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected a validation error when pdb is missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestProteinMPNNExecute`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

Mirror `ligandMPNNTool.Execute`:
1. Unmarshal into `ProteinMPNNParams`; wrap parse errors.
2. `params.Validate()` — return error.
3. Resolve workspace paths when `t.workspaceRoot != ""`: rewrite `PDB`, `FixedPositions`, `BiasAA`, `BiasByResidue`, `TiedPositions` (each when non-empty).
4. Re-marshal, submit job, `persist` with `Origin: domain.OriginRFDiffMPNN`, `Application: domain.AppBinder`, tool name `"design.proteinmpnn"`.
5. Return `tools.Result{JobID, Display: "started design.proteinmpnn job " + string(jobID) + " — poll jobs.result for the designs", Provenance: domain.NewToolCallRef("design.proteinmpnn", input)}`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/` and `go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/proteinmpnn.go internal/tools/design/proteinmpnn_test.go
git commit -m "feat(design.proteinmpnn): Execute with validation, path resolution, persist"
```

### Task B3: Adapter rework — typed request, parameterised driver script

**Files:**
- Modify: `internal/backends/local/adapter_proteinmpnn.go`
- Modify: `internal/backends/local/adapter_proteinmpnn_test.go`

The current adapter writes a 2-line driver script with `sampling_temp 0.1 --seed 37 --batch_size 1` hardcoded. Rework: take `domain.ProteinMPNNParams`, generate the script from those (defaults applied when unset), stage the JSONL paths.

- [ ] **Step 1: Write the failing test**

```go
func TestProteinMPNNArgs(t *testing.T) {
	temp, seed := 0.2, 42
	got := strings.Join(proteinMPNNArgs(domain.ProteinMPNNParams{
		NumDesigns: 5, BatchSize: 2, SamplingTemp: &temp, Seed: &seed,
		OmitAAs: "CG",
	}, "/work/parsed.jsonl", "/work/out"), " ")
	for _, want := range []string{
		"--jsonl_path /work/parsed.jsonl", "--out_folder /work/out",
		"--num_seq_per_target 5", "--batch_size 2",
		"--sampling_temp 0.2", "--seed 42", `--omit_AAs CG`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args missing %q in %q", want, got)
		}
	}
	// Unset NumDesigns defaults to 1 (a usable single sequence per target).
	d := strings.Join(proteinMPNNArgs(domain.ProteinMPNNParams{}, "/p", "/o"), " ")
	if !strings.Contains(d, "--num_seq_per_target 1") {
		t.Errorf("default num_seq_per_target = 1, got %q", d)
	}
}
```

Add `domain` and `strings` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestProteinMPNNArgs`
Expected: FAIL — `proteinMPNNArgs` undefined.

- [ ] **Step 3: Write the implementation**

Replace `proteinMPNNRequest` with `type proteinMPNNRequest = domain.ProteinMPNNParams` (import `domain`). Extract `proteinMPNNArgs(p domain.ProteinMPNNParams, jsonlPath, outFolder string) []string` building all `run.py` flags per the CONTRACT mapping. Defaults when unset: `--num_seq_per_target 1`, `--batch_size 1`, `--sampling_temp 0.1`, `--seed 37`. Each JSONL-path flag (`--chain_id_jsonl`, `--fixed_positions_jsonl`, `--bias_AA_jsonl`, `--bias_by_res_jsonl`, `--tied_positions_jsonl`) is appended ONLY when the corresponding param field is non-empty, pointing at its **staged** workdir path. `--save_score 1` appended when `SaveScore != nil && *SaveScore`.

Rework `Invoke`:
1. Unmarshal request into `domain.ProteinMPNNParams`.
2. `req.PDB` non-empty, ends `.pdb`, `os.Stat`-exists; existing weights-cache + container-runtime guards kept.
3. Stage `req.PDB` into `<workdir>/inputs/<base>` (same as today).
4. Stage every set JSONL path: `copyFile(field, filepath.Join(env.WorkDir, filepath.Base(field)))`; rewrite the field to the staged base name in `/work/`.
5. When `ChainsToDesign != ""`, generate the chain-id JSONL: a one-line JSON file like `{"<pdb-stem>": [["<designed_chains>"], ["<fixed_chains>"]]}` — designed chains are the comma-split tokens; fixed chains are the chains present in the input PDB minus the designed ones (parse via `proteinio` — keep it simple: assume fixed = "" when not specified, i.e. designed-only). Write to `/work/chain_id.jsonl`, pass `--chain_id_jsonl /work/chain_id.jsonl`. *Implementation note: if computing "fixed chains" is non-trivial without a PDB parse, ship the simpler form `{"<stem>": [["<designed>"], []]}` — ProteinMPNN treats absent fixed-list as no constraint, and the grounding skill explains the trade-off.*
6. Write the driver script — `parse_multiple_chains.py` step unchanged; the `protein_mpnn_run.py` line uses `proteinMPNNArgs`. Run via the existing container-mode path.
7. Existing `parseProteinMPNNOutput` (FASTA-header parser) kept unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_proteinmpnn.go internal/backends/local/adapter_proteinmpnn_test.go
git commit -m "feat(proteinmpnn adapter): typed request, parameterised driver, JSONL staging"
```

### Task B4: Grounding skill

**Files:**
- Create: `internal/assets/embed/skills/proteinmpnn-design.md`

- [ ] **Step 1: Write the skill**

~90–120 lines, house style, YAML frontmatter (`name: proteinmpnn-design`). Concrete `design.proteinmpnn` JSON examples matching THE CONTRACT. Cover: input is a backbone PDB; `num_designs` × `batch_size` controls total sequences; `sampling_temp` ~0.1 default (lower = more conservative, higher = more diverse); `chains_to_design` for multi-chain backbones; how to author a `fixed_positions_jsonl` (one-line example of the JSONL format `{"<pdb_stem>": {"<chain>": [<1-indexed residue numbers to fix>]}}`); **`bias_AA` is a JSONL file path, not an inline string** — distinguish from LigandMPNN's `bias_AA`; how to read the FASTA-header scores (`score`/`global_score`/`seq_recovery`); a worked example designing sequences for an RFdiffusion backbone.

- [ ] **Step 2: Verify it loads**

Run: `go test ./internal/assets/` and `go build ./...`
Expected: build clean (skill-count test will fail — that's expected, coordinator fixes at integration).

- [ ] **Step 3: Commit**

```bash
git add internal/assets/embed/skills/proteinmpnn-design.md
git commit -m "docs(design.proteinmpnn): proteinmpnn-design grounding skill"
```

---

# STREAM C — bespoke `bindcraftTool` + adapter rework

Worktree `trio/bindcraft`. Read `internal/tools/design/ligandmpnn.go` and `internal/backends/local/adapter_bindcraft.go`.

### Task C1: Bespoke tool — type, schema, interface methods

**Files:**
- Modify: `internal/tools/design/bindcraft.go` (replace entirely)
- Test: `internal/tools/design/bindcraft_test.go` (create)

- [ ] **Step 1: Write the failing test**

```go
package design

import (
	"encoding/json"
	"testing"
)

func TestBindCraftToolSchema(t *testing.T) {
	tool := NewBindCraftTool("/ws", nil, nil, nil)
	if tool.Name() != "design.bindcraft" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{
		"binder_name", "starting_pdb", "chains", "target_hotspot_residues",
		"length_min", "length_max", "number_of_final_designs", "binder_chain",
		"design_runs", "protocol_name", "template_pdb", "omit_aas",
	} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
	// Opaque `settings` is GONE after bespoke.
	if _, present := props["settings"]; present {
		t.Error("typed BindCraft schema must not advertise an opaque settings field")
	}
}

func TestBindCraftToolRequiresConfirmation(t *testing.T) {
	if !NewBindCraftTool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.bindcraft must require confirmation")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestBindCraftTool`
Expected: FAIL — `NewBindCraftTool` still returns the shared `*designTool`.

- [ ] **Step 3: Write the implementation**

Replace the whole of `internal/tools/design/bindcraft.go`, mirroring `internal/tools/design/ligandmpnn.go`. Define `type BindCraftParams = domain.BindCraftParams`; a `bindcraftTool` struct; `NewBindCraftTool(workspaceRoot, mgr, backend, st) *bindcraftTool` (signature unchanged).

Interface methods:
- `Name()` → `"design.bindcraft"`.
- `Description()` → "Design de novo protein binders against a target with BindCraft (x86-only, PyRosetta-based); runs as an async GPU job."
- `InputSchema()` → `type: object`, `required: ["starting_pdb","chains","target_hotspot_residues","length_min","length_max"]`, `properties` for every `BindCraftParams` field. `protocol_name` an enum `[beta_only, ss_only, fixed_seq]`. Each property a `description`.
- `RequiresConfirmation` → `true`; `EstimatedCostUSD` → `5.0`; `EstimatedDuration` → `60 * time.Minute`.
- `Execute` — stub.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/ -run TestBindCraftTool` and `go vet ./internal/tools/design/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/bindcraft.go internal/tools/design/bindcraft_test.go
git commit -m "feat(design.bindcraft): bespoke tool type and typed schema"
```

### Task C2: Execute — validate, resolve paths, submit, persist

**Files:**
- Modify: `internal/tools/design/bindcraft.go`
- Test: `internal/tools/design/bindcraft_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBindCraftExecuteRejectsBadInput(t *testing.T) {
	tool := NewBindCraftTool(t.TempDir(), nil, nil, nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected a validation error when required fields are missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestBindCraftExecute`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

Mirror `ligandMPNNTool.Execute`:
1. Unmarshal into `BindCraftParams`.
2. `params.Validate()`.
3. Resolve workspace paths when `t.workspaceRoot != ""`: rewrite `StartingPDB` and (when non-empty) `TemplatePDB`.
4. Re-marshal, submit job, persist with `Origin: domain.OriginBindCraft`, `Application: domain.AppBinder`, tool name `"design.bindcraft"`.
5. Return the standard Result.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/` and `go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/bindcraft.go internal/tools/design/bindcraft_test.go
git commit -m "feat(design.bindcraft): Execute with validation, path resolution, persist"
```

### Task C3: Adapter rework — typed JSON compilation replaces the opaque pass-through

**Files:**
- Modify: `internal/backends/local/adapter_bindcraft.go`
- Modify: `internal/backends/local/adapter_bindcraft_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBuildBindCraftSettingsJSON(t *testing.T) {
	got := buildBindCraftSettingsJSON(domain.BindCraftParams{
		BinderName: "PDL1_binder",
		StartingPDB: "/work/target.pdb", Chains: "A",
		TargetHotspotResidues: "A30,A33",
		LengthMin: 80, LengthMax: 120,
		NumberOfFinalDesigns: 10, BinderChain: "B",
		ProtocolName: "beta_only",
	})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("settings JSON did not parse: %v\n%s", err, got)
	}
	for k, want := range map[string]any{
		"binder_name":             "PDL1_binder",
		"starting_pdb":            "/work/target.pdb",
		"chains":                  "A",
		"target_hotspot_residues": "A30,A33",
		"binder_chain":            "B",
		"protocol_name":           "beta_only",
		"number_of_final_designs": float64(10),
	} {
		if parsed[k] != want {
			t.Errorf("settings[%q] = %v, want %v", k, parsed[k], want)
		}
	}
	// lengths must be a 2-element list.
	lengths, ok := parsed["lengths"].([]any)
	if !ok || len(lengths) != 2 || lengths[0] != float64(80) || lengths[1] != float64(120) {
		t.Errorf("lengths = %v, want [80, 120]", parsed["lengths"])
	}
	// Unset fields are omitted (defaults applied by BindCraft).
	if _, present := parsed["design_runs"]; present {
		t.Error("unset design_runs must be omitted (zero, not in JSON)")
	}
	if _, present := parsed["template_pdb"]; present {
		t.Error("unset template_pdb must be omitted")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestBuildBindCraftSettingsJSON`
Expected: FAIL — `buildBindCraftSettingsJSON` undefined.

- [ ] **Step 3: Write the implementation**

Replace the current `bindCraftRequest` (and its embedded settings map) with `type bindCraftRequest = domain.BindCraftParams` (import `domain`). Add:

```go
// buildBindCraftSettingsJSON compiles a BindCraft target-settings JSON from
// the typed BindCraftParams. Zero-value fields are omitted so BindCraft
// applies its own defaults — fova never advertises an opaque settings blob.
func buildBindCraftSettingsJSON(p domain.BindCraftParams) string {
	m := map[string]any{
		"starting_pdb":            p.StartingPDB,
		"chains":                  p.Chains,
		"target_hotspot_residues": p.TargetHotspotResidues,
		"lengths":                 []int{p.LengthMin, p.LengthMax},
	}
	if p.BinderName != ""           { m["binder_name"] = p.BinderName }
	if p.NumberOfFinalDesigns > 0    { m["number_of_final_designs"] = p.NumberOfFinalDesigns }
	if p.BinderChain != ""           { m["binder_chain"] = p.BinderChain }
	if p.DesignRuns > 0              { m["design_runs"] = p.DesignRuns }
	if p.ProtocolName != ""          { m["protocol_name"] = p.ProtocolName }
	if p.TemplatePDB != ""           { m["template_pdb"] = p.TemplatePDB }
	if p.OmitAAs != ""               { m["omit_AAs"] = p.OmitAAs }
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}
```

Rework `Invoke` to:
1. Unmarshal request into `domain.BindCraftParams`.
2. Defensive backstop: required fields non-empty.
3. Stage `StartingPDB` and (when set) `TemplatePDB` into the workdir; rewrite the param fields to the staged `/work/<base>` paths before compiling the JSON.
4. Write `buildBindCraftSettingsJSON(req)` to `<workdir>/settings.json`.
5. Existing container-runtime + weights-cache guards kept.
6. Run the container with the existing entrypoint pointing at the settings file.
7. Existing `final_design_stats.csv` parser kept unchanged.

The pre-existing tests that fed an opaque `settings` map are **deleted** (they tested the old pass-through); the new `TestBuildBindCraftSettingsJSON` and an `Invoke` happy-path test cover the new shape.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/`
Expected: PASS (whole package).

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_bindcraft.go internal/backends/local/adapter_bindcraft_test.go
git commit -m "feat(bindcraft adapter): typed settings JSON compilation (no more opaque blob)"
```

### Task C4: Grounding skill

**Files:**
- Create: `internal/assets/embed/skills/bindcraft-design.md`

- [ ] **Step 1: Write the skill**

~90–120 lines, house style, YAML frontmatter (`name: bindcraft-design`). Concrete `design.bindcraft` JSON examples matching THE CONTRACT. Cover: BindCraft is **x86-only** (PyRosetta); the typed `starting_pdb` + `chains` + `target_hotspot_residues` + `length_min`/`length_max` requirements; hotspot selection heuristics (>~3 hydrophobic residues, leave ~10 Å of target context, avoid glycosylated/charged surfaces); `binder_chain` defaults to `"B"`; when to set `design_runs`; the available `protocol_name` choices; reading `final_design_stats.csv` scores. One worked binder-against-PDL1 example.

- [ ] **Step 2: Verify it loads**

Run: `go test ./internal/assets/` and `go build ./...`
Expected: build clean (skill-count test fails as expected — coordinator fixes at integration).

- [ ] **Step 3: Commit**

```bash
git add internal/assets/embed/skills/bindcraft-design.md
git commit -m "docs(design.bindcraft): bindcraft-design grounding skill"
```

---

# INTEGRATION (sequential — after Streams A, B, C all complete)

Run by the coordinator in the `feat/design-trio` worktree.

### Task INT1: Merge the three streams

- [ ] **Step 1: Merge each stream branch**

```bash
git merge --no-ff trio/rfdiffusion -m "merge: design.rfdiffusion bespoke rework"
git merge --no-ff trio/proteinmpnn -m "merge: design.proteinmpnn bespoke rework"
git merge --no-ff trio/bindcraft   -m "merge: design.bindcraft bespoke rework"
```

Expected: three clean merges — the streams touch disjoint files.

- [ ] **Step 2: Build, test, gofmt**

Run: `go build ./...`, then `go test ./...`, then `gofmt -l internal/ cmd/`
Expected: build OK; every package PASS; gofmt prints nothing.

### Task INT2: Asset skill-count + final commit

- [ ] **Step 1: Bump the embedded-skill count**

`internal/assets/assets_test.go` asserts the built-in skill count (currently `12` after the rfantibody trio in `dev`). The three new skills bring it to `15`. Two assertions (in `TestLoadMaterializesAndParses` and `TestLoadReportsBadSkillButKeepsGoing`) plus a comment — bump every `12` → `15`. Re-run `go test ./internal/assets/`; expect green.

- [ ] **Step 2: Final commit**

```bash
git add -A
git commit -m "feat(design-trio): rfdiffusion/proteinmpnn/bindcraft bespoke rework — full schema"
```

### Task INT3: Rebase onto post-B `dev` and merge

- [ ] **Step 1: Rebase**

Once `feat/rfdiffusion2` has merged into `dev`:

```bash
git fetch
git rebase dev
```

Conflicts expected on `internal/domain/types.go` (`MethodConfig` — both branches added fields; resolve by accepting both sides), `internal/tools/plan/plan.go` (B added one method-config branch; we added three — accept both), `internal/tools/plan/render.go` (same — accept both), `internal/assets/assets_test.go` (B bumped 12→13; we bumped 12→15 — resolve to `16` so the count is `dev`-baseline + 4 total).

All conflicts are additive. Resolve, rerun build + tests, continue the rebase.

- [ ] **Step 2: Merge into `dev`**

In the `dev` worktree:

```bash
git -C <dev-worktree> merge --no-ff feat/design-trio -m "Merge feat/design-trio into dev: schema expansion for rfdiffusion/proteinmpnn/bindcraft"
```

Verify build + full suite + gofmt clean on `dev` after the merge.

---

## Self-Review

- **Spec coverage:** Component A per tool → Stream A/B/C task 1+2. Component B per tool → Stream A/B/C task 3. Component C `/plan` → Foundation Commit 2. Component D `Validate` → Foundation Commit 1. Component E score ingestion → existing parsers preserved (verified by each adapter rework's tests). Component F skills → Stream A/B/C task 4. Testing → tests in every task + INT1.
- **Placeholder scan:** none. The ProteinMPNN chain_id JSONL has a small caveat in Task B3 step 3 about the "fixed chains" simplification — that's an explicit, bounded design choice, not a TBD.
- **Type consistency:** `domain.RFdiffusionParams`/`ProteinMPNNParams`/`BindCraftParams` defined once in the Foundation, imported (not copied) everywhere. `rfdiffusionArgs`, `proteinMPNNArgs`, `buildBindCraftSettingsJSON` each keep one signature throughout.
