# `design.rfdiffusion2` Integration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fully integrate `design.rfdiffusion2` end-to-end — typed schema, a new adapter driving RFdiffusion2's Hydra-driven pipeline (`pipeline.py`), `/plan` method-config, metrics-CSV score ingestion, grounding skill.

**Architecture:** A coordinator **Foundation** (the `domain.RFdiffusion2Params` type + `MethodConfig.RFdiffusion2` + `Validate`) is committed first. Then four file-disjoint streams build in parallel worktrees and merge: A the bespoke tool, B the local adapter, C the `/plan` integration, D the grounding skill. Every stream imports the one `domain.RFdiffusion2Params` type.

**Tech Stack:** Go 1.22+, `tools.Tool`, `jobs.Manager`, the local container backend, RFdiffusion2 (`pipeline.py` — backbone diffusion + idealization + inline LigandMPNN + inline Chai-1) in a tool image.

**Spec:** `docs/superpowers/specs/2026-05-23-design-rfdiffusion2-design.md`

---

## Parallel execution model

The **Foundation** is committed on `feat/rfdiffusion2` by the coordinator before any stream starts. Each stream then runs in its own worktree off the foundation commit:

| Stream | Branch | Files (exclusive) |
|---|---|---|
| A — tool | `rfdiffusion2/tool` | `internal/tools/design/rfdiffusion2.go`, `internal/tools/design/rfdiffusion2_test.go`, `internal/tools/design/design_test.go` |
| B — adapter | `rfdiffusion2/adapter` | `internal/backends/local/adapter_rfdiffusion2.go`, `internal/backends/local/adapter_rfdiffusion2_test.go` |
| C — /plan | `rfdiffusion2/plan` | `internal/tools/plan/plan.go`, `internal/tools/plan/plan_test.go`, `internal/tools/plan/render.go`, `internal/tools/plan/render_test.go` |
| D — skill | `rfdiffusion2/skill` | `internal/assets/embed/skills/rfdiffusion2-design.md` |

No file appears twice. The Foundation is committed first, so every stream's package compiles and tests standalone. `compat.go`, `cmd/fova/main.go`, `tools.toml`, and `runtime_exec.go` are **already complete** for rfdiffusion2 — no stream touches them.

Each stream agent runs `go build ./...` and `go test ./<its package>` green before its final commit.

---

## THE CONTRACT — `domain.RFdiffusion2Params`

Defined once in the Foundation; imported (not copied) by every stream.

```go
// RFdiffusion2Params is the agent-facing RFdiffusion2 run configuration. It
// drives RFdiffusion2's Hydra-config pipeline (rf_diffusion/benchmark/pipeline.py)
// — backbone diffusion + idealization, then (when StopStep="end") inline
// LigandMPNN sequence fitting + inline Chai-1 fold + metrics emission. Pointer
// fields distinguish "unset" (omit the override, use the upstream default)
// from a real zero value. It lives in internal/domain so a DesignPlan's
// MethodConfig can carry it without an import cycle; internal/tools/design
// references it under a package-local alias.
type RFdiffusion2Params struct {
	Benchmark                string `json:"benchmark,omitempty"`         // "" / "active_site_demo" (default) / "enzyme_bench_n41"
	MotifPDB                 string `json:"motif_pdb,omitempty"`         // workspace path to a user catalytic motif PDB; overrides the benchmark's bundled motif
	Contigs                  string `json:"contigs,omitempty"`           // Hydra-style contig string; required when MotifPDB is set
	NumDesigns               int    `json:"num_designs,omitempty"`
	Seed                     *int   `json:"seed,omitempty"`
	GuidepostXYZAsDesignBB   *bool  `json:"guidepost_xyz_as_design_bb,omitempty"`
	IdealizeSidechainOutputs *bool  `json:"idealize_sidechain_outputs,omitempty"`
	StopStep                 string `json:"stop_step,omitempty"`         // "" / "design" / "end" (default "end" — full pipeline)
}
```

### Benchmark resolution (Stream B)

The `Benchmark` enum maps to the upstream `--config-name` + bundled `sweep.benchmarks=`:

| Benchmark value          | Hydra overrides emitted                                                                |
|--------------------------|----------------------------------------------------------------------------------------|
| `""` or `active_site_demo` | `--config-name=open_source_demo` `sweep.benchmarks=active_site_unindexed_atomic_partial_ligand` |
| `enzyme_bench_n41`         | `--config-name=enzyme_bench_n41_fixedligand` `in_proc=True`                              |

### Motif-override resolution (Stream B)

When `MotifPDB != ""`, the adapter:

1. stages the PDB into the workdir as `/work/<base>.pdb`;
2. **adds** these Hydra overrides on top of the benchmark's `--config-name`:
   `+inference.input_pdb=/work/<base>.pdb` `contigmap.contigs=[<Contigs>]`.

`Contigs` is required when `MotifPDB` is set (the override is meaningless without a contig string).

### Inference-toggle override mapping (Stream B)

A Hydra override is appended only when the corresponding field is set:

| Field                     | Hydra override                                            |
|---------------------------|-----------------------------------------------------------|
| `NumDesigns > 0`          | `inference.num_designs=<N>`                                |
| `Seed != nil`              | `seed=<N>`                                                  |
| `GuidepostXYZAsDesignBB != nil` | `inference.guidepost_xyz_as_design_bb=<true|false>`     |
| `IdealizeSidechainOutputs != nil` | `inference.idealize_sidechain_outputs=<true|false>`   |
| `StopStep != ""`             | `stop_step='<design|end>'` (single-quoted to survive bash) |

The adapter always appends `outdir=/work/out` `hydra.run.dir=/work/out` so the output landing tree is deterministic regardless of the chosen `--config-name`.

### Score keys (Stream B)

`pipeline.py` writes a metrics CSV under `/work/out/.../*.csv` (exact filename pinned during B implementation by glob-search). The header drives parsing — every numeric column becomes a score, with these canonical-key foldings:

- `metrics.IdealizedResidueRMSD.rmsd_constellation` → `idealized_residue_rmsd`
- `motif_ideality_diff` → `motif_ideality_diff`
- `contig_rmsd_des_ref_motif_atom` → `motif_rmsd` (the headline design-vs-reference motif-atom RMSD)
- every other numeric column → carried through under its header name, lower-cased

The design tag column (first column, by convention `name` or `design`; the parser tolerates either) links each row to its Chai-1 prediction PDB (or, for `stop_step='design'`, its diffusion-stage backbone PDB) in the same output tree. A row with no matching PDB is dropped; a PDB with no row gets an empty `Scores`. An empty PDB set is an error.

---

# FOUNDATION (coordinator — one commit before streams start)

### Commit — domain foundation

**Files:**
- Modify: `internal/domain/types.go`
- Create: `internal/domain/rfdiffusion2.go`
- Create: `internal/domain/rfdiffusion2_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/domain/rfdiffusion2_test.go`:

```go
package domain

import (
	"strings"
	"testing"
)

func TestRFdiffusion2ParamsValidateValid(t *testing.T) {
	cases := []struct {
		name string
		p    RFdiffusion2Params
	}{
		{"defaults", RFdiffusion2Params{}},
		{"explicit_active_site", RFdiffusion2Params{Benchmark: "active_site_demo"}},
		{"enzyme_bench", RFdiffusion2Params{Benchmark: "enzyme_bench_n41"}},
		{"motif_with_contigs", RFdiffusion2Params{
			MotifPDB: "inputs/triad.pdb", Contigs: "5-15,A10-30,5-15",
		}},
		{"all_toggles", RFdiffusion2Params{
			NumDesigns: 8, Seed: intPtr(7), StopStep: "design",
			GuidepostXYZAsDesignBB:   boolPtr(true),
			IdealizeSidechainOutputs: boolPtr(false),
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.p.Validate(); err != nil {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}

func TestRFdiffusion2ParamsValidateInvalid(t *testing.T) {
	cases := []struct {
		name    string
		p       RFdiffusion2Params
		wantSub string
	}{
		{"unknown_benchmark", RFdiffusion2Params{Benchmark: "made_up"}, "benchmark"},
		{"motif_without_contigs", RFdiffusion2Params{MotifPDB: "x.pdb"}, "contigs"},
		{"motif_bad_extension", RFdiffusion2Params{MotifPDB: "x.txt", Contigs: "1-1"}, "motif_pdb"},
		{"negative_num_designs", RFdiffusion2Params{NumDesigns: -1}, "num_designs"},
		{"negative_seed", RFdiffusion2Params{Seed: intPtr(-3)}, "seed"},
		{"bad_stop_step", RFdiffusion2Params{StopStep: "halfway"}, "stop_step"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.p.Validate()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q must mention %q", err.Error(), c.wantSub)
			}
		})
	}
}

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestRFdiffusion2Params`
Expected: FAIL — `RFdiffusion2Params` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/domain/types.go`, immediately after the existing `RFantibodyParams` block (around line 251), add:

```go
// RFdiffusion2Params is the agent-facing RFdiffusion2 run configuration. It
// drives RFdiffusion2's Hydra-config pipeline (rf_diffusion/benchmark/pipeline.py)
// — backbone diffusion + idealization, then (when StopStep="end") inline
// LigandMPNN sequence fitting + inline Chai-1 fold + metrics emission. Pointer
// fields distinguish "unset" (omit the override, use the upstream default)
// from a real zero value. It lives in internal/domain so a DesignPlan's
// MethodConfig can carry it without an import cycle; internal/tools/design
// references it under a package-local alias.
type RFdiffusion2Params struct {
	Benchmark                string `json:"benchmark,omitempty"`
	MotifPDB                 string `json:"motif_pdb,omitempty"`
	Contigs                  string `json:"contigs,omitempty"`
	NumDesigns               int    `json:"num_designs,omitempty"`
	Seed                     *int   `json:"seed,omitempty"`
	GuidepostXYZAsDesignBB   *bool  `json:"guidepost_xyz_as_design_bb,omitempty"`
	IdealizeSidechainOutputs *bool  `json:"idealize_sidechain_outputs,omitempty"`
	StopStep                 string `json:"stop_step,omitempty"`
}
```

Also in `internal/domain/types.go`, extend `MethodConfig` (around line 225-230) with the new pointer — **purely additive**, alongside `RFantibody`:

```go
type MethodConfig struct {
	SpecPath     string              `json:"spec_path,omitempty"`
	BoltzGen     *BoltzGenParams     `json:"boltzgen,omitempty"`
	LigandMPNN   *LigandMPNNParams   `json:"ligandmpnn,omitempty"`
	RFantibody   *RFantibodyParams   `json:"rfantibody,omitempty"`
	RFdiffusion2 *RFdiffusion2Params `json:"rfdiffusion2,omitempty"`
}
```

Create `internal/domain/rfdiffusion2.go`:

```go
package domain

import (
	"fmt"
	"strings"
)

// rfdiffusion2Benchmarks is the closed set of bundled active-site sweeps.
var rfdiffusion2Benchmarks = map[string]bool{
	"active_site_demo": true,
	"enzyme_bench_n41": true,
}

// rfdiffusion2StopSteps is the closed set of pipeline stop points.
var rfdiffusion2StopSteps = map[string]bool{
	"design": true, // backbone diffusion + idealization only
	"end":    true, // full pipeline (default) — design + LigandMPNN + Chai-1
}

// Validate checks the value shape of an RFdiffusion2Params. It performs no
// filesystem access — workspace-path existence (motif_pdb) is the caller's
// job (the design tool's Execute, the adapter's Invoke). It returns the first
// violation as a design.rfdiffusion2-prefixed error, or nil when valid.
func (p RFdiffusion2Params) Validate() error {
	if b := strings.TrimSpace(p.Benchmark); b != "" && !rfdiffusion2Benchmarks[b] {
		return fmt.Errorf("design.rfdiffusion2: benchmark %q is invalid — "+
			"use active_site_demo (default) or enzyme_bench_n41", p.Benchmark)
	}
	if motif := strings.TrimSpace(p.MotifPDB); motif != "" {
		if !strings.HasSuffix(motif, ".pdb") {
			return fmt.Errorf("design.rfdiffusion2: motif_pdb %q must be a .pdb file", p.MotifPDB)
		}
		if strings.TrimSpace(p.Contigs) == "" {
			return fmt.Errorf("design.rfdiffusion2: contigs is required when motif_pdb is set " +
				"(give the Hydra contigmap.contigs string, e.g. 5-15,A10-30,5-15)")
		}
	}
	if p.NumDesigns < 0 {
		return fmt.Errorf("design.rfdiffusion2: num_designs must not be negative (got %d)", p.NumDesigns)
	}
	if p.Seed != nil && *p.Seed < 0 {
		return fmt.Errorf("design.rfdiffusion2: seed must not be negative (got %d)", *p.Seed)
	}
	if s := strings.TrimSpace(p.StopStep); s != "" && !rfdiffusion2StopSteps[s] {
		return fmt.Errorf("design.rfdiffusion2: stop_step %q is invalid — "+
			"use design (backbone only) or end (full pipeline, default)", p.StopStep)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/ -run TestRFdiffusion2Params` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/types.go internal/domain/rfdiffusion2.go internal/domain/rfdiffusion2_test.go
git commit -m "feat(domain): RFdiffusion2Params, MethodConfig.RFdiffusion2, Validate"
```

---

# STREAM A — bespoke `rfdiffusion2Tool`

Worktree `rfdiffusion2/tool`. Package `internal/tools/design`. Read
`internal/tools/design/rfantibody.go` first — `rfdiffusion2Tool` mirrors it
exactly (the bespoke design-tool pattern). The currently-checked-in
`rfdiffusion2.go` is the shared `designTool` wrapper this stream replaces.

### Task A1: Bespoke tool — type, schema, interface methods

**Files:**
- Modify: `internal/tools/design/rfdiffusion2.go` (replace entirely)
- Modify: `internal/tools/design/design_test.go` (drop the rfdiffusion2 designTool rows)
- Test: `internal/tools/design/rfdiffusion2_test.go` (create)

> **Package-compiles note:** `design_test.go`'s `TestAntibodyEnzymeToolMetadata` has two `func(...) *designTool` tables with `NewRFdiffusion2Tool` rows (lines ~142-145 and ~161-164); once `NewRFdiffusion2Tool` returns `*rfdiffusion2Tool` those rows stop compiling — Step 3 removes them. The `var _ tools.Tool = NewRFdiffusion2Tool(...)` line at ~129 keeps compiling (the bespoke type satisfies `tools.Tool`) — leave it.

- [ ] **Step 1: Write the failing test**

Create `internal/tools/design/rfdiffusion2_test.go`:

```go
package design

import (
	"encoding/json"
	"testing"
)

func TestRFdiffusion2ToolSchema(t *testing.T) {
	tool := NewRFdiffusion2Tool("/ws", nil, nil, nil)
	if tool.Name() != "design.rfdiffusion2" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{
		"benchmark", "motif_pdb", "contigs", "num_designs", "seed",
		"guidepost_xyz_as_design_bb", "idealize_sidechain_outputs", "stop_step",
	} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestRFdiffusion2ToolRequiresConfirmation(t *testing.T) {
	if !NewRFdiffusion2Tool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.rfdiffusion2 must require confirmation — GPU design job")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestRFdiffusion2Tool`
Expected: FAIL — `NewRFdiffusion2Tool` still returns the shared `*designTool`, which has no typed `benchmark`/`motif_pdb`/etc. properties.

- [ ] **Step 3: Write the implementation**

Replace the whole of `internal/tools/design/rfdiffusion2.go`:

```go
package design

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// (The "io" import is added in A2 when Execute's job-Run callback needs io.Writer.)

// RFdiffusion2Params is the agent-facing RFdiffusion2 run configuration. It is
// an alias of domain.RFdiffusion2Params — the type lives in internal/domain so
// a DesignPlan can carry it without an import cycle, and design tools
// reference it here under the friendlier package-local name.
type RFdiffusion2Params = domain.RFdiffusion2Params

// rfdiffusion2Benchmarks is the closed set of bundled active-site sweeps,
// advertised as the benchmark enum.
var rfdiffusion2Benchmarks = []string{"active_site_demo", "enzyme_bench_n41"}

// rfdiffusion2StopSteps is the closed set of pipeline stop points, advertised
// as the stop_step enum.
var rfdiffusion2StopSteps = []string{"design", "end"}

// rfdiffusion2Tool is the bespoke design.rfdiffusion2 tool. Unlike the shared
// designTool wrapper, it advertises RFdiffusion2's Hydra-driven pipeline
// surface — the benchmark choice (or a user motif PDB + contig string
// override), the inference toggles, and the stop-step switch.
type rfdiffusion2Tool struct {
	mgr           *jobs.Manager
	backend       backends.Backend
	store         *store.Store
	workspaceRoot string
}

// NewRFdiffusion2Tool builds the design.rfdiffusion2 tool — atom-level enzyme
// active-site scaffolding with RFdiffusion2. workspaceRoot scopes the relative
// path inputs (motif_pdb).
//
// The signature is held stable so cmd/fova/main.go's registration line is
// unchanged across the bespoke-tool rework.
func NewRFdiffusion2Tool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *rfdiffusion2Tool {
	return &rfdiffusion2Tool{
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}

func (*rfdiffusion2Tool) Name() string { return "design.rfdiffusion2" }

func (*rfdiffusion2Tool) Description() string {
	return "Scaffold enzyme backbones around a catalytic motif with " +
		"RFdiffusion2 — atom-level flow-matching active-site scaffolding " +
		"that runs the full Hydra-driven pipeline (backbone diffusion → " +
		"idealization → inline LigandMPNN sequence design → inline Chai-1 " +
		"fold → metrics). Runs as an async GPU job. Supports the bundled " +
		"benchmark presets (active_site_demo, enzyme_bench_n41), a user " +
		"catalytic motif PDB + contig string override, and the documented " +
		"inference toggles."
}

// InputSchema advertises every RFdiffusion2Params field, with the benchmark
// and stop_step enums and minimums on the bounded numerics. No required keys:
// every field has a default or is a conditional override; Validate enforces
// the conditional shape (motif_pdb ⇒ contigs).
func (*rfdiffusion2Tool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"benchmark": map[string]any{
				"type":        "string",
				"description": "Bundled in-image active-site sweep — active_site_demo (the open-source active-site demo, default) or enzyme_bench_n41 (the AME-41 enzyme benchmark)",
				"enum":        rfdiffusion2Benchmarks,
				"default":     "active_site_demo",
			},
			"motif_pdb": map[string]any{
				"type":        "string",
				"description": "Workspace path to a user catalytic motif .pdb; when set, overrides the benchmark's bundled motif via Hydra +inference.input_pdb and requires contigs",
			},
			"contigs": map[string]any{
				"type":        "string",
				"description": "Hydra-style contig string, e.g. '5-15,A10-30,5-15'; required when motif_pdb is set, ignored otherwise",
			},
			"num_designs": map[string]any{
				"type":        "integer",
				"description": "Number of backbones to generate (inference.num_designs)",
				"minimum":     1,
			},
			"seed": map[string]any{
				"type":        "integer",
				"description": "Random seed for reproducible runs",
				"minimum":     0,
			},
			"guidepost_xyz_as_design_bb": map[string]any{
				"type":        "boolean",
				"description": "Whether unindexed motif XYZ coordinates overwrite the matched backbone (inference.guidepost_xyz_as_design_bb)",
			},
			"idealize_sidechain_outputs": map[string]any{
				"type":        "boolean",
				"description": "Run the PyRosetta idealization pass on atomized sidechains post-diffusion (inference.idealize_sidechain_outputs)",
			},
			"stop_step": map[string]any{
				"type":        "string",
				"description": "Where to stop the pipeline — design (backbone + motif only) or end (full pipeline through Chai-1 fold; default)",
				"enum":        rfdiffusion2StopSteps,
				"default":     "end",
			},
		},
	}
}

// Design jobs are long and GPU-bound — always require user approval.
func (*rfdiffusion2Tool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*rfdiffusion2Tool) EstimatedCostUSD(json.RawMessage) float64        { return 5.0 }
func (*rfdiffusion2Tool) EstimatedDuration(json.RawMessage) time.Duration { return 60 * time.Minute }

// Execute is the A2 implementation — stubbed in A1 so the tool compiles
// against tools.Tool. A2 adds validation, path resolution, job submission, and
// the persist callback.
func (t *rfdiffusion2Tool) Execute(_ context.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{}, nil
}

// persist parses the backend's design-list output and writes each design to
// the store. A response with no "designs" array persists nothing. (Wired up by
// A2.)
func (t *rfdiffusion2Tool) persist(out []byte) (int, error) {
	var bo backendOutput
	if err := json.Unmarshal(out, &bo); err != nil {
		return 0, fmt.Errorf("design.rfdiffusion2 output is not valid JSON: %w", err)
	}
	for _, d := range bo.Designs {
		design := domain.Design{
			ID:            domain.DesignID("d_" + uuid.NewString()),
			ProjectID:     store.DefaultProjectID,
			Created:       time.Now().UTC(),
			Origin:        domain.OriginRFDiff2MPNN,
			Application:   domain.AppEnzyme,
			Sequence:      domain.Sequence{Chains: d.Sequence},
			StructureFile: d.StructureFile,
			Scores:        d.Scores,
			Provenance:    []domain.ToolCallRef{domain.NewToolCallRef("design.rfdiffusion2", nil)},
		}
		if err := t.store.InsertDesign(design); err != nil {
			return 0, err
		}
	}
	return len(bo.Designs), nil
}

```

In `internal/tools/design/design_test.go`, remove the two `NewRFdiffusion2Tool` rows from `TestAntibodyEnzymeToolMetadata`'s tables:

```diff
-		{func(ws string, m *jobs.Manager, s *store.Store, b *stubBackend) *designTool {
-			return NewRFdiffusion2Tool(ws, m, b, s)
-		}, "design.rfdiffusion2"},
```

and

```diff
-		{func(ws string, m *jobs.Manager, s *store.Store, b *stubBackend) *designTool {
-			return NewRFdiffusion2Tool(ws, m, b, s)
-		}, domain.OriginRFDiff2MPNN, domain.AppEnzyme},
```

Both tables now have zero rows, so `TestAntibodyEnzymeToolMetadata` no longer tests anything. **Delete the entire function** (lines ~133-187). Both the rfantibody and rfdiffusion2 enzyme/antibody tools now have their own bespoke-tool test coverage in `rfantibody_test.go` and `rfdiffusion2_test.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/ -run TestRFdiffusion2Tool` and `go vet ./internal/tools/design/`
Expected: PASS; package compiles.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/rfdiffusion2.go internal/tools/design/rfdiffusion2_test.go internal/tools/design/design_test.go
git commit -m "feat(design.rfdiffusion2): bespoke tool type and typed schema"
```

### Task A2: Execute — validate, resolve paths, submit, persist

**Files:**
- Modify: `internal/tools/design/rfdiffusion2.go`
- Test: `internal/tools/design/rfdiffusion2_test.go`

Read `rfantibody.go`'s `Execute` (lines ~143-196) and `persist` (lines ~200-222) — `rfdiffusion2Tool` is the same shape; the only differences are the workspace-path field (`MotifPDB` instead of `Target` and `FrameworkPDB`) and the tool name.

- [ ] **Step 1: Write the failing test**

Append to `internal/tools/design/rfdiffusion2_test.go`:

```go
import (
	"context"

	// ...existing imports
)

func TestRFdiffusion2ExecuteRejectsBadInput(t *testing.T) {
	tool := NewRFdiffusion2Tool(t.TempDir(), nil, nil, nil)
	// motif_pdb without contigs — Validate rejects before any job/store access.
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"motif_pdb":"x.pdb"}`)); err == nil {
		t.Fatal("expected a validation error when motif_pdb has no contigs")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestRFdiffusion2Execute`
Expected: FAIL — `Execute` is the A1 stub that returns nil.

- [ ] **Step 3: Write the implementation**

Replace the `Execute` stub in `internal/tools/design/rfdiffusion2.go` with the full implementation. Drop the `var _ = io.Discard` placeholder; `io` is now used by the job's `Run` closure.

```go
// Execute validates the request, resolves the workspace path input, submits
// a background job, and returns its ID immediately. The job runs the backend,
// parses the designs, and persists them.
func (t *rfdiffusion2Tool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var params RFdiffusion2Params
	if err := json.Unmarshal(input, &params); err != nil {
		return tools.Result{}, fmt.Errorf("invalid design.rfdiffusion2 request: %w", err)
	}
	if err := params.Validate(); err != nil {
		return tools.Result{}, err
	}
	// Resolve the one workspace-relative path input against the workspace root.
	if t.workspaceRoot != "" && params.MotifPDB != "" {
		resolved, err := tools.ResolveWorkspacePath(t.workspaceRoot, params.MotifPDB)
		if err != nil {
			return tools.Result{}, fmt.Errorf("design.rfdiffusion2: %w", err)
		}
		if resolved != "" {
			params.MotifPDB = resolved
		}
	}
	resolved, err := json.Marshal(params)
	if err != nil {
		return tools.Result{}, fmt.Errorf("design.rfdiffusion2: %w", err)
	}
	jobID, err := t.mgr.Submit(jobs.Spec{
		Kind:    domain.JobCompute,
		Tool:    "design.rfdiffusion2",
		Backend: t.backend.Name(),
		Input:   resolved,
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			out, err := t.backend.Run(ctx, "design.rfdiffusion2", resolved, log, progress)
			if err != nil {
				return nil, err
			}
			progress(0.95)
			if _, perr := t.persist(out); perr != nil {
				return out, perr
			}
			return out, nil
		},
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		JobID: jobID,
		Display: fmt.Sprintf("started design.rfdiffusion2 job %s — poll jobs.result for the designs",
			jobID),
		Provenance: domain.NewToolCallRef("design.rfdiffusion2", input),
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/rfdiffusion2.go internal/tools/design/rfdiffusion2_test.go
git commit -m "feat(design.rfdiffusion2): Execute with validation, path resolution, persist"
```

---

# STREAM B — local adapter

Worktree `rfdiffusion2/adapter`. Package `internal/backends/local`. Read
`adapter_rfantibody.go` (the staging + driver-script + weights-guard +
entrypoint-override pattern), `adapter_ligandmpnn.go` (workspace path staging
and `os.Stat` weights guard), and `runtime_exec.go` (the `Entrypoint` field on
`ContainerRunArgs` — already present from rfantibody, no runtime change needed
this stream).

### Task B1: Hydra-override builder

**Files:**
- Create: `internal/backends/local/adapter_rfdiffusion2.go`
- Create: `internal/backends/local/adapter_rfdiffusion2_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/backends/local/adapter_rfdiffusion2_test.go`:

```go
package local

import (
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/domain"
)

func TestRFdiffusion2HydraOverridesActiveSiteDemo(t *testing.T) {
	args := rfdiffusion2HydraOverrides(domain.RFdiffusion2Params{
		Benchmark: "active_site_demo",
	}, "")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--config-name=open_source_demo",
		"sweep.benchmarks=active_site_unindexed_atomic_partial_ligand",
		"outdir=/work/out",
		"hydra.run.dir=/work/out",
		"stop_step='end'", // default
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("overrides missing %q in:\n%s", want, joined)
		}
	}
}

func TestRFdiffusion2HydraOverridesEnzymeBench(t *testing.T) {
	args := rfdiffusion2HydraOverrides(domain.RFdiffusion2Params{
		Benchmark: "enzyme_bench_n41",
	}, "")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--config-name=enzyme_bench_n41_fixedligand",
		"in_proc=True",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("overrides missing %q in:\n%s", want, joined)
		}
	}
}

func TestRFdiffusion2HydraOverridesMotifOverride(t *testing.T) {
	args := rfdiffusion2HydraOverrides(domain.RFdiffusion2Params{
		Benchmark: "active_site_demo",
		MotifPDB:  "/host/triad.pdb",
		Contigs:   "5-15,A10-30,5-15",
	}, "/work/triad.pdb")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"+inference.input_pdb=/work/triad.pdb",
		"contigmap.contigs=[5-15,A10-30,5-15]",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("overrides missing %q in:\n%s", want, joined)
		}
	}
}

func TestRFdiffusion2HydraOverridesToggles(t *testing.T) {
	tru := true
	fls := false
	seed := 7
	args := rfdiffusion2HydraOverrides(domain.RFdiffusion2Params{
		NumDesigns:               8,
		Seed:                     &seed,
		GuidepostXYZAsDesignBB:   &tru,
		IdealizeSidechainOutputs: &fls,
		StopStep:                 "design",
	}, "")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"inference.num_designs=8",
		"seed=7",
		"inference.guidepost_xyz_as_design_bb=True",
		"inference.idealize_sidechain_outputs=False",
		"stop_step='design'",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("overrides missing %q in:\n%s", want, joined)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestRFdiffusion2HydraOverrides`
Expected: FAIL — `rfdiffusion2HydraOverrides` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/backends/local/adapter_rfdiffusion2.go`:

```go
package local

import (
	"strconv"

	"github.com/alvarogonjim/fova/internal/domain"
)

// rfdiffusion2HydraOverrides returns the positional Hydra overrides for one
// pipeline.py invocation. motifContainerPath is the /work-rooted path of the
// motif PDB once staged by Invoke; an empty string means no motif override.
//
// Always-on overrides:
//   --config-name=... + sweep selection (per Benchmark, or the default)
//   outdir=/work/out + hydra.run.dir=/work/out (so the output tree is deterministic)
//   stop_step='end' (the default, full pipeline; can be overridden)
//
// Conditional overrides — emitted only when the corresponding field is set:
//   +inference.input_pdb=<motifContainerPath> contigmap.contigs=[<Contigs>]  (when MotifPDB set)
//   inference.num_designs=<N>
//   seed=<N>
//   inference.guidepost_xyz_as_design_bb=True|False
//   inference.idealize_sidechain_outputs=True|False
//   stop_step='<design|end>' (replaces the default when explicit)
func rfdiffusion2HydraOverrides(p domain.RFdiffusion2Params, motifContainerPath string) []string {
	var args []string

	// --config-name + bundled sweep selection.
	switch p.Benchmark {
	case "enzyme_bench_n41":
		args = append(args,
			"--config-name=enzyme_bench_n41_fixedligand",
			"in_proc=True",
		)
	default: // "" or "active_site_demo"
		args = append(args,
			"--config-name=open_source_demo",
			"sweep.benchmarks=active_site_unindexed_atomic_partial_ligand",
		)
	}

	// Deterministic output landing.
	args = append(args, "outdir=/work/out", "hydra.run.dir=/work/out")

	// Motif override.
	if motifContainerPath != "" {
		args = append(args,
			"+inference.input_pdb="+motifContainerPath,
			"contigmap.contigs=["+p.Contigs+"]",
		)
	}

	// Inference toggles.
	if p.NumDesigns > 0 {
		args = append(args, "inference.num_designs="+strconv.Itoa(p.NumDesigns))
	}
	if p.Seed != nil {
		args = append(args, "seed="+strconv.Itoa(*p.Seed))
	}
	if p.GuidepostXYZAsDesignBB != nil {
		args = append(args, "inference.guidepost_xyz_as_design_bb="+pyBool(*p.GuidepostXYZAsDesignBB))
	}
	if p.IdealizeSidechainOutputs != nil {
		args = append(args, "inference.idealize_sidechain_outputs="+pyBool(*p.IdealizeSidechainOutputs))
	}

	// stop_step — default 'end' (full pipeline) unless explicit.
	stop := p.StopStep
	if stop == "" {
		stop = "end"
	}
	args = append(args, "stop_step='"+stop+"'")

	return args
}

// pyBool returns "True"/"False" — what Hydra/OmegaConf expect for a bool
// override. Lower-case "true"/"false" works in newer OmegaConf releases but
// the Python-style capitalised form is the safe, upstream-documented choice.
func pyBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/ -run TestRFdiffusion2HydraOverrides`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_rfdiffusion2.go internal/backends/local/adapter_rfdiffusion2_test.go
git commit -m "feat(rfdiffusion2 adapter): Hydra-override builder for pipeline.py"
```

### Task B2: Driver-script builder

**Files:**
- Modify: `internal/backends/local/adapter_rfdiffusion2.go`
- Modify: `internal/backends/local/adapter_rfdiffusion2_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/backends/local/adapter_rfdiffusion2_test.go`:

```go
func TestBuildRFdiffusion2Driver(t *testing.T) {
	script := buildRFdiffusion2Driver([]string{
		"--config-name=open_source_demo",
		"sweep.benchmarks=active_site_unindexed_atomic_partial_ligand",
		"outdir=/work/out",
		"stop_step='end'",
	})
	for _, want := range []string{
		"#!/bin/bash",
		"set -euo pipefail",
		"mkdir -p /work/out",
		"python /opt/rfdiffusion2/rf_diffusion/benchmark/pipeline.py",
		"--config-name=open_source_demo",
		"sweep.benchmarks=active_site_unindexed_atomic_partial_ligand",
		"outdir=/work/out",
		"stop_step='end'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("driver missing %q in:\n%s", want, script)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestBuildRFdiffusion2Driver`
Expected: FAIL — `buildRFdiffusion2Driver` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/backends/local/adapter_rfdiffusion2.go`, add:

```go
import (
	"strings"   // add to existing imports
)

// buildRFdiffusion2Driver renders the bash script that drives one pipeline.py
// invocation inside the tool image. The script mkdirs the deterministic
// /work/out landing dir then execs python pipeline.py with the assembled
// Hydra overrides. The container is run with Entrypoint=bash because the
// image ENTRYPOINT is `python /opt/rfdiffusion2/rf_diffusion/benchmark/pipeline.py`
// — we override it so the script can prepare /work/out before exec and so the
// argv shape stays uniform across benchmark/motif variants.
func buildRFdiffusion2Driver(hydraOverrides []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("set -euo pipefail\n")
	b.WriteString("mkdir -p /work/out\n")
	b.WriteString("python /opt/rfdiffusion2/rf_diffusion/benchmark/pipeline.py")
	for _, arg := range hydraOverrides {
		b.WriteString(" ")
		b.WriteString(arg)
	}
	b.WriteString("\n")
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/ -run TestBuildRFdiffusion2Driver`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_rfdiffusion2.go internal/backends/local/adapter_rfdiffusion2_test.go
git commit -m "feat(rfdiffusion2 adapter): pipeline.py driver-script builder"
```

### Task B3: Metrics-CSV parsing

**Files:**
- Modify: `internal/backends/local/adapter_rfdiffusion2.go`
- Modify: `internal/backends/local/adapter_rfdiffusion2_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/backends/local/adapter_rfdiffusion2_test.go`:

```go
import (
	"os"
	"path/filepath"
	// ...existing imports
)

func TestParseRFdiffusion2Output(t *testing.T) {
	outDir := t.TempDir()

	// A run directory like pipeline_outputs/<timestamp>_<config>/ with PDBs
	// and a metrics CSV. The parser glob-searches; we exercise that here.
	runDir := filepath.Join(outDir, "pipeline_outputs", "2026-05-23_demo")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"design_0.pdb", "design_1.pdb"} {
		if err := os.WriteFile(filepath.Join(runDir, name), []byte("ATOM\nEND\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	csv := "name,metrics.IdealizedResidueRMSD.rmsd_constellation,motif_ideality_diff,contig_rmsd_des_ref_motif_atom,extra_score\n" +
		"design_0,0.42,0.11,0.38,0.91\n" +
		"design_1,0.55,0.18,0.61,0.82\n"
	if err := os.WriteFile(filepath.Join(runDir, "metrics.csv"), []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}

	designs, err := parseRFdiffusion2Output(outDir)
	if err != nil {
		t.Fatalf("parseRFdiffusion2Output: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(designs))
	}

	// design_0 — sorted first.
	d0 := designs[0]
	if d0.StructureFile == "" || !strings.HasSuffix(d0.StructureFile, "design_0.pdb") {
		t.Errorf("design_0 structure_file = %q", d0.StructureFile)
	}
	for k, want := range map[string]float64{
		"idealized_residue_rmsd": 0.42,
		"motif_ideality_diff":    0.11,
		"motif_rmsd":             0.38,
		"extra_score":            0.91,
	} {
		if got := d0.Scores[k]; got != want {
			t.Errorf("design_0 score %q = %v, want %v", k, got, want)
		}
	}
}

func TestParseRFdiffusion2OutputEmptyErrors(t *testing.T) {
	if _, err := parseRFdiffusion2Output(t.TempDir()); err == nil {
		t.Fatal("expected an error when no prediction PDBs are present")
	}
}

func TestParseRFdiffusion2OutputMissingCSVIsNotFatal(t *testing.T) {
	outDir := t.TempDir()
	runDir := filepath.Join(outDir, "pipeline_outputs", "2026-05-23_demo")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "design_0.pdb"), []byte("ATOM\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseRFdiffusion2Output(outDir)
	if err != nil {
		t.Fatalf("parseRFdiffusion2Output: %v", err)
	}
	if len(designs) != 1 || len(designs[0].Scores) != 0 {
		t.Errorf("missing CSV ⇒ designs with empty Scores, got %+v", designs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestParseRFdiffusion2Output`
Expected: FAIL — `parseRFdiffusion2Output` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/backends/local/adapter_rfdiffusion2.go`, add the imports `encoding/csv`, `fmt`, `os`, `path/filepath`, `sort`, then:

```go
// rfdiffusion2ScoreKey folds a CSV header column to a canonical Scores key.
// Unknown columns are returned lower-cased and as-is, so any numeric column
// pipeline.py emits is carried through under its header name.
func rfdiffusion2ScoreKey(col string) string {
	switch strings.TrimSpace(col) {
	case "metrics.IdealizedResidueRMSD.rmsd_constellation":
		return "idealized_residue_rmsd"
	case "motif_ideality_diff":
		return "motif_ideality_diff"
	case "contig_rmsd_des_ref_motif_atom":
		return "motif_rmsd"
	default:
		return strings.ToLower(strings.TrimSpace(col))
	}
}

// readRFdiffusion2Scores parses the metrics CSV emitted by pipeline.py into
// tag -> score map. The first row is the header; the first column ("name" or
// "design", case-insensitive) keys each data row. Numeric columns become
// scores, with the canonical-key folding in rfdiffusion2ScoreKey + everything
// else carried through. A missing or unreadable file yields an empty map —
// never an error, because a dropped score must not fail an otherwise-
// successful design run.
func readRFdiffusion2Scores(csvPath string) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	f, err := os.Open(csvPath)
	if err != nil {
		return out
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // tolerate ragged rows; we key by column index
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		return out
	}
	header := rows[0]
	tagCol := -1
	for i, col := range header {
		c := strings.ToLower(strings.TrimSpace(col))
		if c == "name" || c == "design" || c == "tag" {
			tagCol = i
			break
		}
	}
	if tagCol < 0 {
		// Convention violation; nothing useful to extract.
		return out
	}
	for _, row := range rows[1:] {
		if tagCol >= len(row) {
			continue
		}
		tag := strings.TrimSpace(row[tagCol])
		if tag == "" {
			continue
		}
		scores := map[string]float64{}
		for i, col := range header {
			if i == tagCol || i >= len(row) {
				continue
			}
			v, err := strconv.ParseFloat(strings.TrimSpace(row[i]), 64)
			if err != nil {
				continue
			}
			scores[rfdiffusion2ScoreKey(col)] = v
		}
		out[tag] = scores
	}
	return out
}

// parseRFdiffusion2Output walks the pipeline.py output tree under outDir and
// returns one designOut per prediction PDB. The pipeline writes a metrics CSV
// somewhere under outDir/pipeline_outputs/<timestamp>_<config>/; the parser
// glob-searches for the first *.csv it finds and uses its tag column to
// associate scores with PDBs. A missing CSV yields scoreless designs (not an
// error). An empty PDB set is an error.
func parseRFdiffusion2Output(outDir string) ([]designOut, error) {
	pdbs, err := filepath.Glob(filepath.Join(outDir, "**", "*.pdb"))
	if err != nil {
		return nil, err
	}
	// Go's filepath.Glob does not recurse; walk for "**" semantics.
	if len(pdbs) == 0 {
		pdbs, err = walkGlob(outDir, ".pdb")
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(pdbs)
	if len(pdbs) == 0 {
		return nil, fmt.Errorf("design.rfdiffusion2: no prediction PDBs found under %s", outDir)
	}

	csvs, err := walkGlob(outDir, ".csv")
	if err != nil {
		return nil, err
	}
	sort.Strings(csvs)
	scores := map[string]map[string]float64{}
	if len(csvs) > 0 {
		scores = readRFdiffusion2Scores(csvs[0])
	}

	designs := make([]designOut, 0, len(pdbs))
	for _, pdb := range pdbs {
		tag := strings.TrimSuffix(filepath.Base(pdb), filepath.Ext(pdb))
		row := scores[tag]
		if row == nil {
			row = map[string]float64{}
		}
		designs = append(designs, designOut{
			Sequence:      map[string]string{},
			StructureFile: pdb,
			Scores:        row,
		})
	}
	return designs, nil
}

// walkGlob walks root and returns every file whose name ends in suffix.
// Used in lieu of `**` globbing, which Go's filepath.Glob doesn't support.
func walkGlob(root, suffix string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, suffix) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/ -run TestParseRFdiffusion2Output`
Expected: PASS (all three sub-tests).

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_rfdiffusion2.go internal/backends/local/adapter_rfdiffusion2_test.go
git commit -m "feat(rfdiffusion2 adapter): metrics-CSV score ingestion"
```

### Task B4: Invoke

**Files:**
- Modify: `internal/backends/local/adapter_rfdiffusion2.go`
- Modify: `internal/backends/local/adapter_rfdiffusion2_test.go`

Read `adapter_rfantibody.go`'s `Invoke` (lines ~217-323) for the runtime / image / weights-guard / Entrypoint-override structure.

- [ ] **Step 1: Write the failing test**

Append to `internal/backends/local/adapter_rfdiffusion2_test.go`:

```go
import (
	"context"
	"encoding/json"
	// ...existing imports
)

// rfdiffusion2TestEnv builds a stub AdapterEnv with a fova home, a registered
// recipe, the weights cache directory present, and a workdir. Modelled on
// boltz2TestEnv / the rfantibody helper.
func rfdiffusion2TestEnv(t *testing.T) AdapterEnv {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ModelsRoot(home, "rfdiffusion2"), 0o755); err != nil {
		t.Fatal(err)
	}
	return AdapterEnv{
		Registry: reg,
		Recipe:   reg.Recipes["rfdiffusion2"],
		WorkDir:  t.TempDir(),
		Tick:     func(float64) {},
		LogWriter: func() io.Writer {
			return io.Discard
		},
	}
}

func TestRFdiffusion2AdapterInvoke(t *testing.T) {
	env := rfdiffusion2TestEnv(t)
	motif := filepath.Join(t.TempDir(), "triad.pdb")
	if err := os.WriteFile(motif, []byte("ATOM\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubContainerRuntime(t, func(args []string) error {
		if len(args) < 2 || args[1] != "run" {
			return nil
		}
		runDir := filepath.Join(env.WorkDir, "out", "pipeline_outputs", "2026-05-23_demo")
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(runDir, "metrics.csv"),
			[]byte("name,motif_ideality_diff\ndesign_0,0.07\n"), 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(runDir, "design_0.pdb"), []byte("ATOM\nEND\n"), 0o644)
	})

	body, _ := json.Marshal(domain.RFdiffusion2Params{
		Benchmark: "active_site_demo",
		MotifPDB:  motif,
		Contigs:   "5-15,A10-30,5-15",
	})
	out, err := rfdiffusion2Adapter{}.Invoke(context.Background(), env, body)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var resp designsEnvelope
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("not a designs envelope: %v", err)
	}
	if len(resp.Designs) != 1 || resp.Designs[0].Scores["motif_ideality_diff"] != 0.07 {
		t.Fatalf("want 1 scored design with motif_ideality_diff=0.07, got %+v", resp.Designs)
	}
}

func TestRFdiffusion2AdapterInvokeRejectsMissingMotif(t *testing.T) {
	env := rfdiffusion2TestEnv(t)
	body := []byte(`{"motif_pdb":"/nonexistent/triad.pdb","contigs":"1-1"}`)
	if _, err := (rfdiffusion2Adapter{}).Invoke(context.Background(), env, body); err == nil {
		t.Fatal("expected an error when motif_pdb does not exist")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestRFdiffusion2Adapter`
Expected: FAIL — `rfdiffusion2Adapter` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/backends/local/adapter_rfdiffusion2.go`, add the imports `context`, `encoding/json` then:

```go
// init registers the RFdiffusion2 design adapter with the local backend.
func init() { registerAdapter(rfdiffusion2Adapter{}) }

// rfdiffusion2Adapter wires design.rfdiffusion2 to the container-mode
// RFdiffusion2 image. The image ENTRYPOINT is `python pipeline.py`, but we
// override it to bash so a small driver script can mkdir /work/out and then
// exec pipeline.py with the assembled Hydra overrides — letting the output
// landing tree stay deterministic across benchmark/motif variants. The
// metrics CSV emitted under /work/out/pipeline_outputs/<timestamp>_<config>/
// is parsed back into the {"designs":[...]} envelope.
type rfdiffusion2Adapter struct{}

func (rfdiffusion2Adapter) AgentTool() string { return "design.rfdiffusion2" }
func (rfdiffusion2Adapter) Recipe() string    { return "rfdiffusion2" }

// Invoke stages the motif PDB (when set), assembles the Hydra overrides,
// writes the driver script, runs the RFdiffusion2 image with the entrypoint
// overridden to bash, and parses the metrics CSV + prediction PDBs into the
// {"designs":[...]} envelope.
func (rfdiffusion2Adapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req domain.RFdiffusion2Params
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion2: invalid request: %w", err)
	}

	if env.Registry == nil {
		return nil, fmt.Errorf("design.rfdiffusion2: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		return nil, fmt.Errorf("design.rfdiffusion2: container image is not configured (run /install rfdiffusion2)")
	}

	// Stage the motif PDB into the workdir when set; remember the /work path
	// for the Hydra override.
	motifContainerPath := ""
	if motif := strings.TrimSpace(req.MotifPDB); motif != "" {
		if !strings.HasSuffix(motif, ".pdb") {
			return nil, fmt.Errorf("design.rfdiffusion2: motif_pdb %q must be a .pdb file", motif)
		}
		if info, err := os.Stat(motif); err != nil || info.IsDir() {
			return nil, fmt.Errorf("design.rfdiffusion2: motif_pdb %q not found", motif)
		}
		base := filepath.Base(motif)
		if err := copyFile(motif, filepath.Join(env.WorkDir, base)); err != nil {
			return nil, fmt.Errorf("design.rfdiffusion2: stage motif_pdb: %w", err)
		}
		motifContainerPath = "/work/" + base
	}

	outDir := filepath.Join(env.WorkDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	env.Tick(0.05) // input staged

	rt := Detect()
	if !rt.Available() {
		return nil, fmt.Errorf("design.rfdiffusion2: no container runtime — install podman or docker")
	}
	if ok, _ := rt.ImageExists(env.Recipe.ImageTag); !ok {
		return nil, fmt.Errorf(
			"design.rfdiffusion2: image %s is missing — run /install rfdiffusion2",
			env.Recipe.ImageTag)
	}

	// Weights cache (RFD checkpoints + bundled LigandMPNN tied weights) is
	// fetched at /install time. A missing cache means install did not
	// complete, so validate it exists rather than creating it.
	modelsCache := ModelsRoot(env.Registry.Home(), "rfdiffusion2")
	if info, err := os.Stat(modelsCache); err != nil || !info.IsDir() {
		return nil, fmt.Errorf(
			"design.rfdiffusion2: weights cache %s missing — run /install rfdiffusion2",
			modelsCache)
	}

	// Write the driver script with the assembled Hydra overrides.
	overrides := rfdiffusion2HydraOverrides(req, motifContainerPath)
	driver := filepath.Join(env.WorkDir, "run.sh")
	if err := os.WriteFile(driver, []byte(buildRFdiffusion2Driver(overrides)), 0o755); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion2: write driver: %w", err)
	}

	// The recipe declares weights_paths = ["/models/rfdiffusion2"], so the
	// host cache mounts to /models/rfdiffusion2 (not /models).
	mounts := []Mount{
		{HostPath: env.WorkDir, ContainerPath: "/work"},
		{HostPath: modelsCache, ContainerPath: "/models/rfdiffusion2"},
	}
	if _, err := rt.RunContainer(ctx, ContainerRunArgs{
		Image:      env.Recipe.ImageTag,
		Entrypoint: "bash",
		Cmd:        []string{"/work/run.sh"},
		GPU:        env.Recipe.GPU,
		Mounts:     mounts,
		Workdir:    "/work",
		Log:        env.LogWriter(),
	}); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion2: container run failed: %w", err)
	}
	env.Tick(0.95) // pipeline.py done

	designs, err := parseRFdiffusion2Output(outDir)
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/` and `go build ./...`
Expected: PASS (whole package); build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_rfdiffusion2.go internal/backends/local/adapter_rfdiffusion2_test.go
git commit -m "feat(rfdiffusion2 adapter): Invoke — staging, driver, weights guard"
```

---

# STREAM C — `/plan` integration

Worktree `rfdiffusion2/plan`. Package `internal/tools/plan`. Read
`plan.go`'s `applyRFantibodyMethodConfig` (lines ~603-626) and `render.go`'s
`renderRFantibodySection` (lines ~181-204) — the RFdiffusion2 equivalents
mirror them.

### Task C1: `plan.create` method-config

**Files:**
- Modify: `internal/tools/plan/plan.go`
- Modify: `internal/tools/plan/plan_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tools/plan/plan_test.go` (mirror the rfantibody helpers
around line 660):

```go
// applyRFdiffusion2ParamsErr runs applyRFdiffusion2MethodConfig against a fresh
// RFdiffusion2 plan and returns the resulting MethodConfig (nil on error) plus
// the error. Helper.
func applyRFdiffusion2ParamsErr(input string) (*domain.MethodConfig, error) {
	ct := &CreateTool{}
	p := domain.DesignPlan{Method: string(MethodRFdiffusion2)}
	if err := ct.applyRFdiffusion2MethodConfig(json.RawMessage(input), &p); err != nil {
		return nil, err
	}
	return p.MethodConfig, nil
}

// applyRFdiffusion2Params is applyRFdiffusion2ParamsErr for the happy path: it
// fails the test on any error and returns the resulting MethodConfig.
func applyRFdiffusion2Params(t *testing.T, input string) *domain.MethodConfig {
	t.Helper()
	cfg, err := applyRFdiffusion2ParamsErr(input)
	if err != nil {
		t.Fatalf("applyRFdiffusion2MethodConfig: %v", err)
	}
	return cfg
}

// TestPlanCreateRFdiffusion2MethodConfig: an RFdiffusion2 plan with
// method_params must land MethodConfig.RFdiffusion2, and an invalid params
// object is rejected.
func TestPlanCreateRFdiffusion2MethodConfig(t *testing.T) {
	cfg := applyRFdiffusion2Params(t, `{"method_params":{"benchmark":"active_site_demo"}}`)
	if cfg == nil || cfg.RFdiffusion2 == nil {
		t.Fatal("MethodConfig.RFdiffusion2 must be populated")
	}
	if cfg.RFdiffusion2.Benchmark != "active_site_demo" {
		t.Errorf("benchmark = %q", cfg.RFdiffusion2.Benchmark)
	}
	if _, err := applyRFdiffusion2ParamsErr(
		`{"method_params":{"motif_pdb":"triad.pdb"}}`); err == nil {
		t.Error("an RFdiffusion2 plan with motif_pdb and no contigs must be rejected")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/plan/ -run TestPlanCreateRFdiffusion2`
Expected: FAIL — `applyRFdiffusion2MethodConfig` undefined.

- [ ] **Step 3: Write the implementation**

In `internal/tools/plan/plan.go`, **immediately after** `applyRFantibodyMethodConfig` (at the end of the file, around line 626), add the parallel helper:

```go
// applyRFdiffusion2MethodConfig parses the method_params input into an
// RFdiffusion2Params and folds it into DesignPlan.MethodConfig. method_params
// is required for an RFdiffusion2 plan — it carries the pipeline-run
// configuration (at minimum the benchmark choice; motif_pdb + contigs
// optional). The params are value-shape validated via
// domain.RFdiffusion2Params.Validate; there is no external check tool.
func (t *CreateTool) applyRFdiffusion2MethodConfig(input json.RawMessage, p *domain.DesignPlan) error {
	var envelope struct {
		Params *domain.RFdiffusion2Params `json:"method_params"`
	}
	if err := json.Unmarshal(input, &envelope); err != nil {
		return fmt.Errorf("plan.create: invalid method_params: %w", err)
	}
	if envelope.Params == nil {
		return fmt.Errorf(
			"plan.create: method RFdiffusion2 requires method_params — the " +
				"RFdiffusion2 run configuration (at minimum a benchmark choice)")
	}
	if err := envelope.Params.Validate(); err != nil {
		return err
	}
	p.MethodConfig = &domain.MethodConfig{RFdiffusion2: envelope.Params}
	return nil
}
```

In `plan.create.Execute`, immediately after the `MethodRFantibody` dispatch block (around lines ~309-316), add the parallel block — **purely additive**, alongside the existing three:

```go
	// RFdiffusion2 folds its pipeline-run configuration (the RFdiffusion2Params)
	// into the plan so /plan can render it and /plan approve can run it. Like
	// LigandMPNN and RFantibody there is no spec file or external check —
	// method_params alone carries the configuration.
	if method == MethodRFdiffusion2 {
		if err := t.applyRFdiffusion2MethodConfig(input, &p); err != nil {
			return tools.Result{}, err
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/plan/ -run TestPlanCreateRFdiffusion2` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/plan/plan.go internal/tools/plan/plan_test.go
git commit -m "feat(plan): RFdiffusion2 method-config in plan.create"
```

### Task C2: `/plan` rendering

**Files:**
- Modify: `internal/tools/plan/render.go`
- Modify: `internal/tools/plan/render_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tools/plan/render_test.go` (mirror `TestRenderRFantibodySection` at line ~213):

```go
// TestRenderRFdiffusion2Section: a plan carrying an RFdiffusion2
// method-config renders the RFdiffusion2 block — the benchmark, the motif
// PDB (when set), the contigs (when set), the num designs, the stop step.
func TestRenderRFdiffusion2Section(t *testing.T) {
	p := &domain.DesignPlan{
		ID: "p_x", Method: "RFdiffusion2",
		MethodConfig: &domain.MethodConfig{RFdiffusion2: &domain.RFdiffusion2Params{
			Benchmark:  "active_site_demo",
			MotifPDB:   "inputs/triad.pdb",
			Contigs:    "5-15,A10-30,5-15",
			NumDesigns: 8,
			StopStep:   "end",
		}},
	}
	out := RenderPlan(*p)
	for _, want := range []string{
		"RFdiffusion2",
		"active_site_demo",
		"inputs/triad.pdb",
		"5-15,A10-30,5-15",
		"end",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered plan missing %q:\n%s", want, out)
		}
	}
}

// TestRenderRFdiffusion2SectionDefaultStopStep: an RFdiffusion2 plan with an
// empty stop_step falls back to "end (default)" in the render — the user sees
// what will actually happen.
func TestRenderRFdiffusion2SectionDefaultStopStep(t *testing.T) {
	p := &domain.DesignPlan{
		ID: "p_x", Method: "RFdiffusion2",
		MethodConfig: &domain.MethodConfig{RFdiffusion2: &domain.RFdiffusion2Params{
			Benchmark: "active_site_demo",
		}},
	}
	out := RenderPlan(*p)
	if !strings.Contains(out, "end (default)") {
		t.Errorf("rendered plan must show 'end (default)' when stop_step is unset:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/plan/ -run TestRenderRFdiffusion2`
Expected: FAIL — no RFdiffusion2 section is rendered.

- [ ] **Step 3: Write the implementation**

In `internal/tools/plan/render.go`, **extend** the `MethodConfig` switch in `RenderPlanWithOpts` (around lines ~95-104) with the new `case`:

```go
	if mc := p.MethodConfig; mc != nil {
		switch {
		case mc.BoltzGen != nil || mc.SpecPath != "":
			renderBoltzGenSection(&b, mc, opts)
		case mc.LigandMPNN != nil:
			renderLigandMPNNSection(&b, mc.LigandMPNN)
		case mc.RFantibody != nil:
			renderRFantibodySection(&b, mc.RFantibody)
		case mc.RFdiffusion2 != nil:
			renderRFdiffusion2Section(&b, mc.RFdiffusion2)
		}
	}
```

Then add the section function, mirroring `renderRFantibodySection`:

```go
// renderRFdiffusion2Section appends the RFdiffusion2 method-config block to b:
// the benchmark choice, the motif PDB and contigs when a user motif is set,
// the design count, and the stop step (with the "end (default)" fallback when
// unset, so the user sees what will actually happen). It is emitted for a
// plan whose MethodConfig carries RFdiffusion2 params.
func renderRFdiffusion2Section(b *strings.Builder, rd *domain.RFdiffusion2Params) {
	b.WriteString("\n  RFdiffusion2 design configuration\n")

	benchmark := rd.Benchmark
	if benchmark == "" {
		benchmark = "active_site_demo (default)"
	}
	labelRow(b, "Benchmark", benchmark)

	if rd.MotifPDB != "" {
		labelRow(b, "Motif PDB", rd.MotifPDB)
		labelRow(b, "Contigs", rd.Contigs)
	}

	labelRow(b, "Num designs", fmt.Sprintf("%d", rd.NumDesigns))

	stop := rd.StopStep
	if stop == "" {
		stop = "end (default)"
	}
	labelRow(b, "Stop step", stop)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/plan/` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/plan/render.go internal/tools/plan/render_test.go
git commit -m "feat(plan): render the RFdiffusion2 method-config section"
```

---

# STREAM D — grounding skill

Worktree `rfdiffusion2/skill`. Read `internal/assets/embed/skills/rfantibody-design.md`
and `ligandmpnn-design.md` for house style; every embedded skill needs `name`
+ `description` frontmatter.

### Task D1: Write `rfdiffusion2-design.md`

**Files:**
- Create: `internal/assets/embed/skills/rfdiffusion2-design.md`

- [ ] **Step 1: Write the skill**

Create `internal/assets/embed/skills/rfdiffusion2-design.md`, starting with
YAML frontmatter, then ~90-150 lines, house style, with concrete
`design.rfdiffusion2` tool-call JSON examples matching THE CONTRACT.

Required sections:

1. **What it does.** Atom-level flow-matching scaffolding for catalytic motifs;
   runs the full Hydra-driven pipeline (`pipeline.py`): backbone diffusion →
   PyRosetta idealization → inline LigandMPNN sequence fitting → inline
   Chai-1 fold → metrics emission. Versus `design.rfdiffusion` (which is
   per-residue, not per-atom, and has no sequence/fold step).

2. **The `benchmark` choice.** `active_site_demo` (the open-source active-site
   demo, broad coverage of unindexed atomic partial-ligand motifs — the
   default, right starting point) vs `enzyme_bench_n41` (the AME-41 curated
   enzyme reference set — narrower, validated against the reference enzymes).
   Pick `enzyme_bench_n41` for benchmarking against AME-41; pick
   `active_site_demo` for exploratory active-site scaffolding.

3. **Supplying a user catalytic motif** (`motif_pdb` + `contigs`).
   - PDB shape: a single chain containing the catalytic residues you want
     scaffolded, ideally idealized (PyRosetta-clean).
   - Contigs syntax: comma-separated tokens
     `<flanker_length>,<chain><start>-<end>,<flanker_length>`; e.g.
     `5-15,A10-30,5-15` = 5-15 residues of flanker, A10-A30 as the fixed
     motif region, 5-15 more residues of flanker. The motif region is the
     part copied from the PDB; the flankers are diffused around it.
   - Worked tool-call JSON for a 3-residue catalytic triad.

4. **`guidepost_xyz_as_design_bb` and `idealize_sidechain_outputs`** — when to
   flip them.
   - `guidepost_xyz_as_design_bb`: leave off (default) for indexed motifs (the
     standard case); turn on when supplying unindexed motif XYZ coordinates
     (rare, advanced).
   - `idealize_sidechain_outputs`: leave on (default) for production runs —
     PyRosetta-clean motifs help downstream LigandMPNN scoring. Turn off only
     for speed in exploratory sweeps.

5. **`stop_step` choice.**
   - `end` (default) — full pipeline, scored designs out the other side.
     Right choice 95% of the time.
   - `design` — backbone + idealization only; useful when you want to chain
     `design.ligandmpnn` + a fold tool yourself, or when iterating on the
     motif and you don't need sequences yet.

6. **Reading the scores.** Headline metric is `motif_rmsd` (lower better; <1 Å
   for a tight scaffold). `idealized_residue_rmsd` — idealization gap; high
   values mean PyRosetta couldn't clean the motif. `motif_ideality_diff` —
   motif-atom ideality. Plus Chai-1 confidence columns when present.

7. **First-run cost.** When `stop_step='end'` (the default), the inline
   Chai-1 fold step downloads `chai_lab`'s ~1.3 GB weights from
   `chaiassets.com` **on first invocation inside the container**. Subsequent
   runs reuse the in-container cache. (Pre-staging is a planned Phase-2
   improvement.)

8. **One worked example.** A complete `design.rfdiffusion2` tool-call JSON
   scaffolding a catalytic triad, with the full pipeline run and a
   `motif_rmsd < 0.5 Å` filter on the output.

9. **Platform note.** x86-only — won't run on the GB10. Fall back to
   `design.rfdiffusion` + `design.proteinmpnn` for aarch64 work.

- [ ] **Step 2: Verify it loads**

Run: `go test ./internal/assets/` and `go build ./...`
Expected: build clean; the assets loader discovers the skill. NOTE: the
`internal/assets/assets_test.go` skill-count assertion (currently 12) will
fail — that's expected and fixed by the coordinator at integration; do **not**
edit any `_test.go` file.

- [ ] **Step 3: Commit**

```bash
git add internal/assets/embed/skills/rfdiffusion2-design.md
git commit -m "docs(design.rfdiffusion2): rfdiffusion2-design grounding skill"
```

---

# INTEGRATION (sequential — after Streams A, B, C, D complete)

Run by the coordinator in the `feat/rfdiffusion2` worktree.

### Task INT1: Merge the four streams

- [ ] **Step 1: Merge each stream branch**

```bash
git merge --no-ff rfdiffusion2/tool    -m "merge: design.rfdiffusion2 bespoke tool"
git merge --no-ff rfdiffusion2/adapter -m "merge: design.rfdiffusion2 adapter"
git merge --no-ff rfdiffusion2/plan    -m "merge: design.rfdiffusion2 /plan integration"
git merge --no-ff rfdiffusion2/skill   -m "merge: rfdiffusion2-design skill"
```

Expected: four clean merges — the streams touch disjoint files.

- [ ] **Step 2: Build, test, gofmt**

Run:
```bash
go build ./...
go test ./...
gofmt -l internal/ cmd/
```
Expected: build OK; every package PASS; gofmt prints nothing.

### Task INT2: Asset skill-count + final commit

- [ ] **Step 1: Update the embedded-skill count**

`internal/assets/assets_test.go` asserts the built-in skill count (12 after
rfantibody). Adding `rfdiffusion2-design.md` makes it 13 — run `go test
./internal/assets/`; if it fails on the count, bump **both** assertions
(currently at lines ~20 and ~50, both with the literal `12`) to `13` and
re-run.

**If a sibling session has landed a built-in skill on `dev` first** (the
knowledge-pdb-search or editable-review tracks per the brief): rebase the
bump against the count `dev` actually shows + 1 (so 13 → 14 if dev has 13,
etc.).

```bash
git add internal/assets/assets_test.go
git commit -m "test(assets): bump built-in skill count for rfdiffusion2-design"
```

- [ ] **Step 2: Final verify**

```bash
go build ./...
go test ./...
gofmt -l internal/ cmd/
```
Expected: build OK; every package PASS; gofmt prints nothing.

The GPU end-to-end run is **x86-only** — RFdiffusion2 cannot run on the GB10
(no Python-3.12 aarch64 PyRosetta wheel). It is validated on an x86 GPU box
when one is available.

---

## Self-Review

- **Spec coverage:**
  - Component A (bespoke tool + schema, spec §3) → Stream A (A1 + A2).
  - Component B (`/plan` integration, spec §4) → Foundation (`MethodConfig.RFdiffusion2`) + Stream C (C1 + C2).
  - Component C (adapter, spec §5) → Stream B (B1 + B2 + B3 + B4). `ContainerRunArgs.Entrypoint` is already present from rfantibody, no runtime change.
  - Component D (preflight, spec §6) → `RFdiffusion2Params.Validate` (Foundation) called by A2 and C1; `Invoke` path-existence (B4).
  - Component E (score ingestion, spec §7) → Stream B (B3 — `parseRFdiffusion2Output` + `readRFdiffusion2Scores` + `rfdiffusion2ScoreKey`).
  - Component F (skill, spec §8) → Stream D (D1).
  - Testing (spec §9) → tests in every task + INT1.
- **Placeholder scan:** none — every step has concrete code, commands, and expected output. The metrics-CSV exact filename is glob-discovered at runtime (`walkGlob(outDir, ".csv")`) so it survives upstream renames; per the spec §13 risk this is pinned during Stream B with a real run, and a wrong filename surfaces as a clear "no prediction PDBs found" or "missing CSV ⇒ scoreless designs" outcome that the parser handles.
- **Type consistency:** `domain.RFdiffusion2Params` defined once in the Foundation, imported everywhere. `rfdiffusion2HydraOverrides`, `buildRFdiffusion2Driver`, `parseRFdiffusion2Output`, `readRFdiffusion2Scores`, `rfdiffusion2ScoreKey`, `rfdiffusion2Adapter`, `applyRFdiffusion2MethodConfig`, `renderRFdiffusion2Section`, `RFdiffusion2Params.Validate` each keep one signature throughout.
- **Additivity (per brief, for the schema-expansion trio rebase):** every `MethodConfig` / plan-dispatch (`plan.go`) / render-dispatch (`render.go`) edit is a pure addition to the existing struct field list / `if method == ...` chain / switch-case list. No renames; no reorderings; the trio (`design.rfdiffusion` / `proteinmpnn` / `bindcraft`) rebases cleanly.
- **TUI test hygiene (per brief):** this plan adds no `internal/tui/` tests; existing `/plan` TUI tests render via `RenderPlan`/`RenderPlanWithOpts`, which already dispatch on the new `mc.RFdiffusion2 != nil` case without TUI-side changes. If a future test exercises `/plan approve` on an RFdiffusion2 plan, the existing `drainTurn(t, m)` helper in `app_test.go` is the documented fix.
