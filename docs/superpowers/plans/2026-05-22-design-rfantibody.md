# design.rfantibody Integration + retire design.chai2 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Retire the unbuildable `design.chai2` phantom, and fully integrate `design.rfantibody` — typed schema, a new adapter driving RFantibody's 3-stage pipeline, `/plan` method-config, Quiver score ingestion, grounding skill.

**Architecture:** A coordinator **Foundation** (chai2 retirement + the `domain.RFantibodyParams` type) is committed first. Then four file-disjoint streams build in parallel worktrees and merge: A the bespoke tool, B the local adapter, C the `/plan` integration, D the grounding skill. Every stream imports the one `domain.RFantibodyParams` type.

**Tech Stack:** Go 1.22+, `tools.Tool`, `jobs.Manager`, the local container backend, RFantibody (`rfdiffusion`/`proteinmpnn`/`rf2` + Quiver) in a tool image.

**Spec:** `docs/superpowers/specs/2026-05-22-design-rfantibody-design.md`

---

## Parallel execution model

The **Foundation** is committed on `feat/rfantibody` by the coordinator before any stream starts. Each stream then runs in its own worktree off the foundation commit:

| Stream | Branch | Files (exclusive) |
|---|---|---|
| A — tool | `rfantibody/tool` | `internal/tools/design/rfantibody.go`, `internal/tools/design/rfantibody_test.go`, `internal/tools/design/design_test.go` |
| B — adapter | `rfantibody/adapter` | `internal/backends/local/adapter_rfantibody.go`, `internal/backends/local/adapter_rfantibody_test.go`, `internal/backends/local/runtime_exec.go`, `internal/backends/local/runtime_exec_test.go` |
| C — /plan | `rfantibody/plan` | `internal/tools/plan/plan.go`, `internal/tools/plan/plan_test.go`, `internal/tools/plan/render.go`, `internal/tools/plan/render_test.go`, `internal/tui/plan.go` |
| D — skill | `rfantibody/skill` | `internal/assets/embed/skills/rfantibody-design.md` |

No file appears twice. The Foundation is committed first, so every stream's package compiles and tests standalone. `cmd/fova/main.go` is touched only by the Foundation (the chai2-line removal).

Each stream agent runs `go build ./...` and `go test ./<its package>` green before its final commit.

---

## THE CONTRACT — `domain.RFantibodyParams`

Defined once in the Foundation; imported (not copied) by every stream.

```go
// RFantibodyParams is the agent-facing RFantibody run configuration. Pointer
// fields distinguish "unset" (omit the flag, use the stage's default) from a
// real zero value.
type RFantibodyParams struct {
	Target          string   `json:"target"`
	Hotspots        string   `json:"hotspots"`
	Framework       string   `json:"framework,omitempty"`     // "nanobody" (default) | "scfv"
	FrameworkPDB    string   `json:"framework_pdb,omitempty"` // workspace path; overrides Framework
	DesignLoops     string   `json:"design_loops,omitempty"`  // e.g. "H1:7,H3:5-13,L3:9-11"
	NumDesigns      int      `json:"num_designs,omitempty"`
	Deterministic   *bool    `json:"deterministic,omitempty"`
	SeqsPerStruct   int      `json:"seqs_per_struct,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	NumRecycles     *int     `json:"num_recycles,omitempty"`
	Seed            *int     `json:"seed,omitempty"`
	HotspotShowProp *float64 `json:"hotspot_show_prop,omitempty"`
}
```

### Framework resolution (Stream B)

- `Framework` `""`/`nanobody` → `/opt/rfantibody/scripts/examples/example_inputs/h-NbBCII10.pdb`
- `Framework` `scfv` → `/opt/rfantibody/scripts/examples/example_inputs/hu-4D5-8_Fv.pdb`
- `FrameworkPDB` non-empty → overrides; the file is staged into the workdir and referenced at `/work/<base>`.

### 3-stage flag mapping (Stream B)

All three stages run `uv run --project /opt/rfantibody <stage>`. A flag is omitted when its pointer is nil / its string is empty / its int is ≤ 0.

- **rfdiffusion:** `-t <target>` `-f <framework>` `-q /work/designs.qv` `-n <NumDesigns>` `-h <Hotspots>`; `-l <DesignLoops>` when set; `--deterministic` when `Deterministic` true.
- **proteinmpnn:** `-q /work/designs.qv` `--output-quiver /work/sequences.qv`; `-n <SeqsPerStruct>` when > 0; `-t <Temperature>` when set; `--deterministic` when true.
- **rf2:** `-q /work/sequences.qv` `--output-quiver /work/predictions.qv`; `-r <NumRecycles>` when set; `-s <Seed>` when set; `--hotspot-show-prop <HotspotShowProp>` when set.
- Then `qvextract /work/predictions.qv -o /work/out` and `qvscorefile /work/predictions.qv` (stdout → `/work/out/scores.tsv`).

### Score keys (Stream B)

`qvscorefile` emits a TSV — a header row then one row per design. Numeric
columns → `designOut.Scores`; `plddt`/`pLDDT` → `plddt`, `pae`/`pAE` → `pae`,
other numeric columns carried through under their column name. The design's
tag column links a score row to its extracted `out/<tag>.pdb`. A prediction
with no score row → empty `Scores`, never an error.

---

# FOUNDATION (coordinator — two commits before streams start)

### Commit 1 — retire `design.chai2`

- **Delete** `internal/tools/design/chai2.go`.
- `internal/tools/plan/compat.go` — remove: the `MethodChai2` const (line ~30); `MethodChai2` from the antibody-application slice (~59) and from the `parseMethod`/all-methods slice (~146); the two alias-map entries `"chai2"` / `"design.chai2"` (~127-128) and their comment (~126); the `case MethodChai2` block in `toolForMethod` (~177-178) and in `installProbeKey` (~209-210); the stale "chai1 weights + design head" comments (~178, ~189-190).
- `internal/tools/plan/plan.go` — remove `, Chai2` (and the trailing parenthetical if it names chai2) from the two `method` schema-description strings at lines ~112 and ~264.
- `cmd/fova/main.go` — delete the `registry.Register(designtools.NewChai2Tool(workspace, mgr, backend, st))` line (~221).
- Test references:
  - `cmd/fova/main_test.go:80` — drop `"design.chai2"` from the expected-tools list.
  - `internal/tools/design/design_test.go` — remove `var _ tools.Tool = NewChai2Tool(...)` (~129) and the `NewChai2Tool` row in `TestAntibodyEnzymeToolMetadata`'s table (~147-148).
  - `internal/tools/plan/compat_test.go:81` — remove the `{"Chai2", MethodChai2}` table entry.
  - `internal/tools/plan/plan_test.go:552` — remove `MethodChai2` from the `[]Method` slice.
  - `internal/safety/guard_test.go:42` — remove the `"design.chai2"` entry.
- **Keep** `domain.OriginChai2` (back-compat for historical design records).
- Verify `go build ./...` + `go test ./...` green. Commit: `refactor(design): retire design.chai2 — Chai-2 is proprietary, not installable`.

### Commit 2 — domain foundation

**File:** `internal/domain/types.go` — add `RFantibodyParams` (THE CONTRACT) and an `RFantibody *RFantibodyParams` field to `MethodConfig`:

```go
type MethodConfig struct {
	SpecPath   string            `json:"spec_path,omitempty"`
	BoltzGen   *BoltzGenParams   `json:"boltzgen,omitempty"`
	LigandMPNN *LigandMPNNParams `json:"ligandmpnn,omitempty"`
	RFantibody *RFantibodyParams `json:"rfantibody,omitempty"`
}
```

**File:** `internal/domain/rfantibody.go` (new) — `func (p RFantibodyParams) Validate() error`, value-shape only (no filesystem):
- `Target` non-empty; `Hotspots` non-empty and every comma-separated token matches `^[A-Za-z][0-9]+$` (chain + residue number);
- `Framework` is `""`/`nanobody`/`scfv`, **or** `FrameworkPDB` is non-empty;
- `DesignLoops`, when set, parses — each comma-separated token is `<CDR>:<spec>` where `<CDR>` ∈ `{H1,H2,H3,L1,L2,L3}` and `<spec>` is an int or `<min>-<max>` with `min ≤ max`;
- `NumDesigns`, `SeqsPerStruct`, if non-zero, are `> 0`; `NumRecycles`, `Seed`, if set, are `> 0`; `Temperature`, if set, `> 0`; `HotspotShowProp`, if set, in `[0,1]`.

Add an `internal/domain/rfantibody_test.go` table test for `Validate` (valid + each invalid case). Verify `go build ./...` + `go test ./internal/domain/`. Commit: `feat(domain): RFantibodyParams, MethodConfig.RFantibody, Validate`.

---

# STREAM A — bespoke `rfantibodyTool`

Worktree `rfantibody/tool`. Package `internal/tools/design`. Read
`internal/tools/design/ligandmpnn.go` first — `rfantibodyTool` mirrors it
exactly (the bespoke design-tool pattern).

### Task A1: Bespoke tool — type, schema, interface methods

**Files:**
- Modify: `internal/tools/design/rfantibody.go` (replace entirely)
- Modify: `internal/tools/design/design_test.go` (drop the rfantibody designTool row)
- Test: `internal/tools/design/rfantibody_test.go` (create)

> **Package-compiles note:** `design_test.go`'s `TestAntibodyEnzymeToolMetadata` has a `func(...) *designTool` table with a `NewRFAntibodyTool(...)` row; once `NewRFAntibodyTool` returns `*rfantibodyTool` that row stops compiling — Step 3 removes it. The `var _ tools.Tool = NewRFAntibodyTool(...)` line keeps compiling (the bespoke type satisfies `tools.Tool`) — leave it.

- [ ] **Step 1: Write the failing test**

```go
package design

import (
	"encoding/json"
	"testing"
)

func TestRFantibodyToolSchema(t *testing.T) {
	tool := NewRFAntibodyTool("/ws", nil, nil, nil)
	if tool.Name() != "design.rfantibody" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{"target", "hotspots", "framework",
		"framework_pdb", "design_loops", "num_designs", "seqs_per_struct",
		"temperature", "num_recycles", "seed", "hotspot_show_prop"} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestRFantibodyToolRequiresConfirmation(t *testing.T) {
	if !NewRFAntibodyTool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.rfantibody must require confirmation — GPU design job")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestRFantibodyTool`
Expected: FAIL — `NewRFAntibodyTool` still returns the shared `*designTool`.

- [ ] **Step 3: Write the implementation**

Replace the whole of `internal/tools/design/rfantibody.go`, mirroring
`ligandmpnn.go`. Define `type RFantibodyParams = domain.RFantibodyParams`; an
`rfantibodyTool` struct with `workspaceRoot`/`mgr`/`backend`/`store`;
`NewRFAntibodyTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *rfantibodyTool` (**signature unchanged**).

Interface methods:
- `Name()` → `"design.rfantibody"`.
- `Description()` → designs de novo antibodies / nanobodies against a target with RFantibody; runs as an async GPU job.
- `InputSchema()` → `type: object`, `required: ["target","hotspots"]`,
  `properties` for every field in THE CONTRACT — `framework` an enum
  `[nanobody,scfv]` with `default: "nanobody"`; bounded numerics carry
  `minimum`; each property a `description`.
- `RequiresConfirmation` → `true`; `EstimatedCostUSD` → `5.0`;
  `EstimatedDuration` → `60 * time.Minute`.
- `Execute` — stub `return tools.Result{}, nil` (real body in A2).

Then remove the `NewRFAntibodyTool` row from `design_test.go`'s
`TestAntibodyEnzymeToolMetadata` `cases` table.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/ -run TestRFantibodyTool` and `go vet ./internal/tools/design/`
Expected: PASS; package compiles.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/rfantibody.go internal/tools/design/rfantibody_test.go internal/tools/design/design_test.go
git commit -m "feat(design.rfantibody): bespoke tool type and typed schema"
```

### Task A2: Execute — validate, resolve paths, submit, persist

**Files:**
- Modify: `internal/tools/design/rfantibody.go`
- Test: `internal/tools/design/rfantibody_test.go`

Read `ligandmpnn.go`'s `Execute` + `persist` — `rfantibodyTool` is the same shape.

- [ ] **Step 1: Write the failing test**

```go
func TestRFantibodyExecuteRejectsBadInput(t *testing.T) {
	tool := NewRFAntibodyTool(t.TempDir(), nil, nil, nil)
	// No target/hotspots — Validate rejects before any job/store access.
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected a validation error when target/hotspots are missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestRFantibodyExecute`
Expected: FAIL — `Execute` is the A1 stub.

- [ ] **Step 3: Write the implementation**

Implement `Execute`, mirroring `ligandMPNNTool.Execute`:
1. `json.Unmarshal` the input into `RFantibodyParams`; wrap a parse error as `fmt.Errorf("invalid design.rfantibody request: %w", err)`.
2. `params.Validate()` — return its error directly on failure.
3. Resolve workspace paths when `t.workspaceRoot != ""`: rewrite `Target` and (when non-empty) `FrameworkPDB` via `tools.ResolveWorkspacePath`; a resolve error returns `fmt.Errorf("design.rfantibody: %w", err)`.
4. Re-marshal the resolved params.
5. Submit the job (`jobs.Spec{Kind: domain.JobCompute, Tool: "design.rfantibody", Backend: t.backend.Name(), Input: resolved, Run: ...}`); `Run` calls `t.backend.Run`, ticks `progress(0.95)`, then `t.persist(out)`.
6. `persist` — copy `ligandMPNNTool.persist` but with `Origin: domain.OriginRFAntibody`, `Application: domain.AppAntibody`, tool name `"design.rfantibody"`.
7. Return `tools.Result{JobID: jobID, Display: "started design.rfantibody job " + string(jobID) + " — poll jobs.result for the designs", Provenance: domain.NewToolCallRef("design.rfantibody", input)}`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/rfantibody.go internal/tools/design/rfantibody_test.go
git commit -m "feat(design.rfantibody): Execute with validation, path resolution, persist"
```

---

# STREAM B — local adapter

Worktree `rfantibody/adapter`. Package `internal/backends/local`. Read
`adapter_ligandmpnn.go` (the staging + weights-guard pattern),
`adapter_proteinmpnn.go` (the driver-script pattern), and `runtime_exec.go`.

### Task B1: `Entrypoint` field on `ContainerRunArgs`

The RFantibody image's ENTRYPOINT is `uv run … rfdiffusion` (stage 1 only).
The 3-stage driver needs the entrypoint overridden to `bash`.

**Files:**
- Modify: `internal/backends/local/runtime_exec.go`
- Test: `internal/backends/local/runtime_exec_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRunContainerEntrypointOverride(t *testing.T) {
	calls := stubContainerRuntime(t, nil)
	rt := Detect()
	_, _ = rt.RunContainer(context.Background(), ContainerRunArgs{
		Image: "fova/x:1", Entrypoint: "bash", Cmd: []string{"/work/run.sh"},
	})
	joined := strings.Join((*calls)[0], " ")
	if !strings.Contains(joined, "--entrypoint bash") {
		t.Errorf("argv missing --entrypoint bash: %s", joined)
	}
	if strings.Index(joined, "--entrypoint") > strings.Index(joined, "fova/x:1") {
		t.Error("--entrypoint must precede the image")
	}
}
```

(Use the existing `stubContainerRuntime` helper in the package; if its file is
elsewhere, the test still compiles — it is package-level.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestRunContainerEntrypointOverride`
Expected: FAIL — `ContainerRunArgs` has no `Entrypoint` field.

- [ ] **Step 3: Write the implementation**

Add `Entrypoint string` to `ContainerRunArgs` (doc comment: "override the image ENTRYPOINT; empty = keep the image default"). In `RunContainer`, after the workdir args and **before** `args = append(args, a.Image)`, add:

```go
	if a.Entrypoint != "" {
		args = append(args, "--entrypoint", a.Entrypoint)
	}
```

Existing adapters pass no `Entrypoint`, so their argv is unchanged.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/ -run TestRunContainerEntrypointOverride`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/runtime_exec.go internal/backends/local/runtime_exec_test.go
git commit -m "feat(local): ContainerRunArgs.Entrypoint for image-entrypoint override"
```

### Task B2: `qvscorefile`-TSV score parsing

**Files:**
- Create: `internal/backends/local/adapter_rfantibody.go`
- Test: `internal/backends/local/adapter_rfantibody_test.go` (create)

- [ ] **Step 1: Write the failing test**

```go
package local

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRFantibodyOutput(t *testing.T) {
	outDir := t.TempDir()
	for _, name := range []string{"ab_0.pdb", "ab_1.pdb"} {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte("ATOM\nEND\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsv := "tag\tplddt\tpae\n" +
		"ab_0\t82.5\t7.1\n" +
		"ab_1\t74.0\t11.8\n"
	if err := os.WriteFile(filepath.Join(outDir, "scores.tsv"), []byte(tsv), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseRFantibodyOutput(outDir)
	if err != nil {
		t.Fatalf("parseRFantibodyOutput: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(designs))
	}
	// designs are sorted by tag; ab_0 first.
	if designs[0].Scores["plddt"] != 82.5 || designs[0].Scores["pae"] != 7.1 {
		t.Errorf("ab_0 scores wrong: %v", designs[0].Scores)
	}
	if designs[0].StructureFile == "" {
		t.Error("ab_0 structure_file must be set")
	}
}

func TestParseRFantibodyOutputEmptyErrors(t *testing.T) {
	if _, err := parseRFantibodyOutput(t.TempDir()); err == nil {
		t.Fatal("expected an error when no prediction PDBs are present")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestParseRFantibodyOutput`
Expected: FAIL — `parseRFantibodyOutput` undefined.

- [ ] **Step 3: Write the implementation**

Create `adapter_rfantibody.go`. Add `parseRFantibodyOutput(outDir string) ([]designOut, error)`:
- read `outDir/scores.tsv` if present: first row is the header; for each data
  row, the `tag` column keys the row, every other column that parses as a
  `float64` becomes a score (lower-cased `plddt`/`pae` mapped to `plddt`/`pae`,
  others carried under their header name);
- glob `outDir/*.pdb`; sort; for each, the tag is the file stem; build
  `designOut{Sequence: map[string]string{}, StructureFile: <path>, Scores: <scores for that tag, or empty>}`;
- empty PDB set ⇒ `fmt.Errorf("design.rfantibody: no prediction PDBs found in %s", outDir)`.

A missing/unreadable `scores.tsv` is not an error — designs get empty `Scores`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/ -run TestParseRFantibodyOutput`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_rfantibody.go internal/backends/local/adapter_rfantibody_test.go
git commit -m "feat(rfantibody adapter): qvscorefile TSV score ingestion"
```

### Task B3: 3-stage driver-script builder

**Files:**
- Modify: `internal/backends/local/adapter_rfantibody.go`
- Test: `internal/backends/local/adapter_rfantibody_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBuildRFantibodyDriver(t *testing.T) {
	tmp := 0.2
	script := buildRFantibodyDriver(domain.RFantibodyParams{
		NumDesigns: 20, Hotspots: "T305,T456", DesignLoops: "H3:5-13",
		SeqsPerStruct: 4, Temperature: &tmp,
	}, "/work/target.pdb", "/work/framework.pdb")
	for _, want := range []string{
		"uv run --project /opt/rfantibody rfdiffusion",
		"-t /work/target.pdb", "-f /work/framework.pdb",
		"-h T305,T456", "-n 20", "-l H3:5-13",
		"uv run --project /opt/rfantibody proteinmpnn",
		"-n 4", "-t 0.2",
		"uv run --project /opt/rfantibody rf2",
		"qvextract /work/predictions.qv", "qvscorefile /work/predictions.qv",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("driver missing %q in:\n%s", want, script)
		}
	}
}
```

Add `domain` and `strings` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestBuildRFantibodyDriver`
Expected: FAIL — `buildRFantibodyDriver` undefined.

- [ ] **Step 3: Write the implementation**

Add `buildRFantibodyDriver(p domain.RFantibodyParams, targetPath, frameworkPath string) string` — returns a bash script:
- starts `#!/bin/bash` + `set -euo pipefail`;
- stage 1: `uv run --project /opt/rfantibody rfdiffusion` with `-t <targetPath> -f <frameworkPath> -q /work/designs.qv -h <Hotspots>`, `-n <NumDesigns>` when > 0, `-l <DesignLoops>` when non-empty, `--deterministic` when `Deterministic` is true;
- stage 2: `uv run --project /opt/rfantibody proteinmpnn -q /work/designs.qv --output-quiver /work/sequences.qv`, `-n <SeqsPerStruct>` when > 0, `-t <Temperature>` when set, `--deterministic` when true;
- stage 3: `uv run --project /opt/rfantibody rf2 -q /work/sequences.qv --output-quiver /work/predictions.qv`, `-r <NumRecycles>` when set, `-s <Seed>` when set, `--hotspot-show-prop <HotspotShowProp>` when set;
- then `mkdir -p /work/out`, `uv run --project /opt/rfantibody qvextract /work/predictions.qv -o /work/out`, and `uv run --project /opt/rfantibody qvscorefile /work/predictions.qv > /work/out/scores.tsv`.

Float flags format with `strconv.FormatFloat(v, 'g', -1, 64)`; ints with `strconv.Itoa`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/ -run TestBuildRFantibodyDriver`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_rfantibody.go internal/backends/local/adapter_rfantibody_test.go
git commit -m "feat(rfantibody adapter): 3-stage pipeline driver-script builder"
```

### Task B4: Invoke

**Files:**
- Modify: `internal/backends/local/adapter_rfantibody.go`
- Test: `internal/backends/local/adapter_rfantibody_test.go`

Read `adapter_ligandmpnn.go`'s `Invoke` for the runtime/image/weights-guard structure.

- [ ] **Step 1: Write the failing test**

```go
func TestRFantibodyAdapterInvoke(t *testing.T) {
	env := rfantibodyTestEnv(t) // helper modelled on boltz2TestEnv
	target := filepath.Join(t.TempDir(), "ag.pdb")
	if err := os.WriteFile(target, []byte("ATOM\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubContainerRuntime(t, func(args []string) error {
		if len(args) < 2 || args[1] != "run" {
			return nil
		}
		out := filepath.Join(env.WorkDir, "out")
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(out, "scores.tsv"),
			[]byte("tag\tplddt\tpae\nab_0\t80.0\t8.0\n"), 0o644)
		return os.WriteFile(filepath.Join(out, "ab_0.pdb"), []byte("ATOM\nEND\n"), 0o644)
	})
	body := []byte(`{"target":"` + target + `","hotspots":"T10","framework":"nanobody"}`)
	out, err := rfantibodyAdapter{}.Invoke(context.Background(), env, body)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var resp designsEnvelope
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("not a designs envelope: %v", err)
	}
	if len(resp.Designs) != 1 || resp.Designs[0].Scores["plddt"] != 80.0 {
		t.Fatalf("want 1 scored design, got %+v", resp.Designs)
	}
}

func TestRFantibodyAdapterInvokeRejectsMissingTarget(t *testing.T) {
	env := rfantibodyTestEnv(t)
	if _, err := (rfantibodyAdapter{}).Invoke(context.Background(), env, []byte(`{"hotspots":"T10"}`)); err == nil {
		t.Fatal("expected an error when target is missing")
	}
}
```

Add a `rfantibodyTestEnv(t)` helper modelled on `boltz2TestEnv` in
`adapter_boltz2_test.go`: `LoadRegistry(t.TempDir())`,
`os.MkdirAll(ModelsRoot(home, "rfantibody"), 0o755)`, the `rfantibody` recipe,
an `AdapterEnv` with `WorkDir: t.TempDir()`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestRFantibodyAdapterInvoke`
Expected: FAIL — `rfantibodyAdapter` undefined.

- [ ] **Step 3: Write the implementation**

Add the adapter: `init()` → `registerAdapter(rfantibodyAdapter{})`;
`rfantibodyAdapter` with `AgentTool() "design.rfantibody"`, `Recipe() "rfantibody"`. `Invoke`:
1. `json.Unmarshal` the request into `domain.RFantibodyParams`; wrap parse errors.
2. `req.Target` non-empty, ends `.pdb`, exists (`os.Stat`) — else a clear error.
3. `env.Registry` non-nil; `env.Recipe.ImageTag` non-empty (`run /install rfantibody`).
4. Stage the target into `env.WorkDir` (`copyFile` → `/work/<base>`).
5. Resolve the framework: if `req.FrameworkPDB != ""`, stage it into `env.WorkDir` (→ `/work/<base>`); else map `req.Framework` (`""`/`nanobody` → the bundled nanobody path, `scfv` → the bundled scFv path — both `/opt/rfantibody/scripts/examples/example_inputs/...`, in-image, used as-is).
6. `os.MkdirAll(filepath.Join(env.WorkDir, "out"))`. `env.Tick(0.05)`.
7. `rt := Detect()`; require `rt.Available()` and `rt.ImageExists`.
8. `modelsCache := ModelsRoot(env.Registry.Home(), "rfantibody")` — `os.Stat`; absent ⇒ `fmt.Errorf("design.rfantibody: weights cache %s missing — run /install rfantibody", modelsCache)`.
9. Write `buildRFantibodyDriver(req, "/work/<target base>", <framework /work or /opt path>)` to `filepath.Join(env.WorkDir, "run.sh")` (mode `0o755`).
10. `rt.RunContainer` with `Entrypoint: "bash"`, `Cmd: []string{"/work/run.sh"}`, mounts `{env.WorkDir:/work}` + `{modelsCache:/models}`, `GPU: env.Recipe.GPU`, `Workdir: "/work"`, `Log: env.LogWriter()`. `env.Tick(0.95)`.
11. `designs, err := parseRFantibodyOutput(filepath.Join(env.WorkDir, "out"))`.
12. `return json.Marshal(designsEnvelope{Designs: designs})`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/`
Expected: PASS (whole package).

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_rfantibody.go internal/backends/local/adapter_rfantibody_test.go
git commit -m "feat(rfantibody adapter): Invoke — staging, 3-stage driver, weights guard"
```

---

# STREAM C — `/plan` integration

Worktree `rfantibody/plan`. Packages `internal/tools/plan` and `internal/tui`.
Read `plan.go`'s `applyLigandMPNNMethodConfig` and `render.go`'s
`renderLigandMPNNSection` — the RFantibody equivalents mirror them.

### Task C1: `plan.create` method-config

**Files:**
- Modify: `internal/tools/plan/plan.go`
- Test: `internal/tools/plan/plan_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPlanCreateRFantibodyMethodConfig(t *testing.T) {
	cfg := applyRFantibodyParams(t, `{"method_params":{"target":"ag.pdb","hotspots":"T10"}}`)
	if cfg == nil || cfg.RFantibody == nil {
		t.Fatal("MethodConfig.RFantibody must be populated")
	}
	if cfg.RFantibody.Target != "ag.pdb" {
		t.Errorf("target = %q", cfg.RFantibody.Target)
	}
	if _, err := applyRFantibodyParamsErr(`{"method_params":{"hotspots":"T10"}}`); err == nil {
		t.Error("an RFantibody plan with no target must be rejected")
	}
}
```

Implement the helpers `applyRFantibodyParams` / `applyRFantibodyParamsErr` in
`plan_test.go` calling the new `applyRFantibodyMethodConfig` against a
`*domain.DesignPlan` — mirror the existing `applyLigandMPNNMethodConfig`
tests.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/plan/ -run TestPlanCreateRFantibody`
Expected: FAIL — `applyRFantibodyMethodConfig` undefined.

- [ ] **Step 3: Write the implementation**

Add `applyRFantibodyMethodConfig(input json.RawMessage, p *domain.DesignPlan) error` mirroring `applyLigandMPNNMethodConfig`:
- unmarshal `{ "method_params": *domain.RFantibodyParams }`;
- nil `method_params` ⇒ `fmt.Errorf("plan.create: method RFantibody requires method_params — the RFantibody run configuration (at minimum target and hotspots)")`;
- `params.Validate()` — return its error;
- `p.MethodConfig = &domain.MethodConfig{RFantibody: params}`.

In `plan.create`, alongside the `MethodBoltzGen`/`MethodLigandMPNN` blocks, add `if method == MethodRFantibody { if err := t.applyRFantibodyMethodConfig(input, &p); err != nil { return tools.Result{}, err } }`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/plan/ -run TestPlanCreateRFantibody`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/plan/plan.go internal/tools/plan/plan_test.go
git commit -m "feat(plan): RFantibody method-config in plan.create"
```

### Task C2: `/plan` rendering

**Files:**
- Modify: `internal/tools/plan/render.go`, `internal/tui/plan.go`
- Test: `internal/tools/plan/render_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRenderRFantibodySection(t *testing.T) {
	p := &domain.DesignPlan{
		ID: "p_x", Method: "RFantibody",
		MethodConfig: &domain.MethodConfig{RFantibody: &domain.RFantibodyParams{
			Framework: "nanobody", Target: "ag.pdb", Hotspots: "T10,T12", NumDesigns: 20,
		}},
	}
	out := RenderPlan(*p)
	for _, want := range []string{"RFantibody", "nanobody", "ag.pdb", "T10,T12"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered plan missing %q:\n%s", want, out)
		}
	}
}
```

(Match `RenderPlan`'s real signature — value or pointer — as the existing
`render_test.go` LigandMPNN test does.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/plan/ -run TestRenderRFantibodySection`
Expected: FAIL — no RFantibody section is rendered.

- [ ] **Step 3: Write the implementation**

In `render.go`, the `MethodConfig` dispatch (currently BoltzGen / LigandMPNN) gains a `case mc.RFantibody != nil: renderRFantibodySection(&b, mc.RFantibody)`. Add `renderRFantibodySection(b *strings.Builder, ra *domain.RFantibodyParams)` mirroring `renderLigandMPNNSection`'s `labelRow` style: a `"\n  RFantibody design configuration\n"` header, then `labelRow` lines for framework (blank ⇒ `"nanobody (default)"`, or the `framework_pdb` path when set), target, hotspots, num designs, and design loops (when set).

`internal/tui/plan.go` — if it renders plans generically through `RenderPlan`/`RenderPlanWithOpts`, no change is needed beyond confirming it compiles; an RFantibody plan has no spec file or check result.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/plan/ ./internal/tui/` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/plan/render.go internal/tools/plan/render_test.go internal/tui/plan.go
git commit -m "feat(plan): render the RFantibody method-config section"
```

---

# STREAM D — grounding skill

Worktree `rfantibody/skill`. Read `internal/assets/embed/skills/ligandmpnn-design.md`
for house style; every embedded skill needs `name` + `description` frontmatter.

### Task D1: Write `rfantibody-design.md`

**Files:**
- Create: `internal/assets/embed/skills/rfantibody-design.md`

- [ ] **Step 1: Write the skill**

Create `internal/assets/embed/skills/rfantibody-design.md`, starting with YAML
frontmatter (`name: rfantibody-design`, a one-line `description`), then
~90–120 lines, house style, with concrete `design.rfantibody` tool-call JSON
examples matching THE CONTRACT. Cover: the nanobody-vs-scFv `framework` choice
(and the `framework_pdb` HLT-override option); epitope/`hotspots` selection
(RFantibody is *sensitive* to hotspot choice — >~3 hydrophobic residues, leave
~10 Å of target context, avoid glycosylated/charged patches); `design_loops`
per-CDR length specs (`H1:7` fixed, `H3:5-13` range; widen H3 for deep
pockets); `num_designs` campaign sizing; the 3-stage pipeline; and reading the
result scores (`plddt` 0–100 higher-better, `pae` Å lower-better; RF2 pae < 10
as a useful filter). One worked example designing a nanobody against a target
epitope.

- [ ] **Step 2: Verify it loads**

Run: `go test ./internal/assets/` and `go build ./...`
Expected: build clean; the assets loader discovers the skill. NOTE: if an
assets test asserts the built-in skill count, it will fail (count went up by
one) — that is expected and fixed by the coordinator at integration; do **not**
edit any `_test.go` file.

- [ ] **Step 3: Commit**

```bash
git add internal/assets/embed/skills/rfantibody-design.md
git commit -m "docs(design.rfantibody): rfantibody-design grounding skill"
```

---

# INTEGRATION (sequential — after Streams A, B, C, D complete)

Run by the coordinator in the `feat/rfantibody` worktree.

### Task INT1: Merge the four streams

- [ ] **Step 1: Merge each stream branch**

```bash
git merge --no-ff rfantibody/tool    -m "merge: design.rfantibody bespoke tool"
git merge --no-ff rfantibody/adapter -m "merge: design.rfantibody adapter"
git merge --no-ff rfantibody/plan    -m "merge: design.rfantibody /plan integration"
git merge --no-ff rfantibody/skill   -m "merge: rfantibody-design skill"
```

Expected: four clean merges — the streams touch disjoint files.

- [ ] **Step 2: Build, test, gofmt**

Run: `go build ./...`, then `go test ./...`, then `gofmt -l internal/ cmd/`
Expected: build OK; every package PASS; gofmt prints nothing.

### Task INT2: Asset skill-count + final commit

- [ ] **Step 1: Update the embedded-skill count if asserted**

`internal/assets/assets_test.go` asserts the built-in skill count (11 after
ligandmpnn). Adding `rfantibody-design.md` makes it 12 — run `go test
./internal/assets/`; if it fails on the count, bump the assertions from 11 to
12 and re-run.

- [ ] **Step 2: Final commit**

```bash
git add -A
git commit -m "feat(design.rfantibody): full RFantibody integration; design.chai2 retired"
```

The GPU end-to-end run is **x86-only** — RFantibody cannot run on the GB10
(no aarch64 DGL wheel). It is validated on an x86 GPU box when one is
available.

---

## Self-Review

- **Spec coverage:** Part 1 (chai2 retirement) → Foundation Commit 1. Component A (tool) → Stream A. Component B (`/plan`) → Foundation Commit 2 (`MethodConfig.RFantibody`) + Stream C. Component C (adapter) → Stream B. Component D (preflight) → `RFantibodyParams.Validate` (Foundation) called by A2 and C1. Component E (score ingestion) → Stream B (B2). Component F (skill) → Stream D. Testing → tests in every task + INT1.
- **Placeholder scan:** none — every step has concrete code, commands, and expected output. The `qvscorefile` TSV column names are read by header at runtime (the parser keys by name, lower-cases `plddt`/`pae`) — robust to the exact casing.
- **Type consistency:** `domain.RFantibodyParams` defined once in the Foundation, imported everywhere. `parseRFantibodyOutput`, `buildRFantibodyDriver`, `rfantibodyAdapter`, `applyRFantibodyMethodConfig`, `renderRFantibodySection`, `RFantibodyParams.Validate` each keep one signature throughout.
