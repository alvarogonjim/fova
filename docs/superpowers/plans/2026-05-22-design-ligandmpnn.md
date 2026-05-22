# design.ligandmpnn Full Integration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the thin `design.ligandmpnn` tool with a real LigandMPNN integration — typed full-surface schema, a new local adapter, `/plan` method-config wiring, FASTA-header score ingestion, and a grounding skill.

**Architecture:** A small **domain foundation** (the `LigandMPNNParams` type, its `Validate`, and the `MethodConfig` field) is committed first by the coordinator. Then four file-disjoint streams build in parallel worktrees and merge: A the bespoke agent tool, B the local adapter, C the `/plan` integration, D the grounding skill. Every stream imports the one `domain.LigandMPNNParams` type — there are no copied structs.

**Tech Stack:** Go 1.22+, `tools.Tool`, `jobs.Manager`, the local container backend, LigandMPNN (`run.py`) in a tool image.

**Spec:** `docs/superpowers/specs/2026-05-22-design-ligandmpnn-design.md`
**Umbrella:** `docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md`

---

## Parallel execution model

The **Foundation** (below) is committed on `feat/ligandmpnn` by the coordinator *before* any stream starts. Each stream then runs in its own worktree off the foundation commit:

| Stream | Branch | Files (exclusive) |
|---|---|---|
| A — tool | `ligandmpnn/tool` | `internal/tools/design/ligandmpnn.go`, `internal/tools/design/ligandmpnn_test.go`, `internal/tools/design/design_test.go` |
| B — adapter | `ligandmpnn/adapter` | `internal/backends/local/adapter_ligandmpnn.go`, `internal/backends/local/adapter_ligandmpnn_test.go` |
| C — /plan | `ligandmpnn/plan` | `internal/tools/plan/plan.go`, `internal/tools/plan/plan_test.go`, `internal/tools/plan/render.go`, `internal/tools/plan/render_test.go`, `internal/tui/plan.go` |
| D — skill | `ligandmpnn/skill` | `internal/assets/embed/skills/ligandmpnn-design.md` |

No file appears twice — a four-way merge into `feat/ligandmpnn` cannot conflict. Because the foundation is already committed, every stream's package compiles and tests standalone. `cmd/fova/main.go` is **not** touched — `NewLigandMPNNTool`'s signature is unchanged.

Each stream agent runs `go build ./...` and `go test ./<its package>` green before its final commit.

---

## THE CONTRACT — `domain.LigandMPNNParams`

Defined once in the Foundation; imported (not copied) by every stream.

```go
// LigandMPNNParams is the agent-facing LigandMPNN run configuration. Every
// field maps to a `run.py` flag; fova owns the infra flags separately.
// Pointer fields distinguish "unset" (omit the flag, use run.py's default)
// from a real zero value.
type LigandMPNNParams struct {
	ModelType                 string   `json:"model_type,omitempty"`
	PDB                       string   `json:"pdb"`
	NumDesigns                int      `json:"num_designs,omitempty"`
	BatchSize                 int      `json:"batch_size,omitempty"`
	Temperature               *float64 `json:"temperature,omitempty"`
	Seed                      *int     `json:"seed,omitempty"`
	RedesignedResidues        string   `json:"redesigned_residues,omitempty"`
	FixedResidues             string   `json:"fixed_residues,omitempty"`
	ChainsToDesign            string   `json:"chains_to_design,omitempty"`
	BiasAA                    string   `json:"bias_AA,omitempty"`
	OmitAA                    string   `json:"omit_AA,omitempty"`
	BiasAAPerResidue          string   `json:"bias_AA_per_residue,omitempty"`
	OmitAAPerResidue          string   `json:"omit_AA_per_residue,omitempty"`
	LigandUseAtomContext      *bool    `json:"ligand_use_atom_context,omitempty"`
	LigandUseSideChainContext *bool    `json:"ligand_use_side_chain_context,omitempty"`
	LigandCutoff              *float64 `json:"ligand_cutoff,omitempty"`
	SymmetryResidues          string   `json:"symmetry_residues,omitempty"`
	SymmetryWeights           string   `json:"symmetry_weights,omitempty"`
	HomoOligomer              *bool    `json:"homo_oligomer,omitempty"`
	GlobalTransmembraneLabel  *int     `json:"global_transmembrane_label,omitempty"`
	TransmembraneBuried       string   `json:"transmembrane_buried,omitempty"`
	TransmembraneInterface    string   `json:"transmembrane_interface,omitempty"`
	PackSideChains            *bool    `json:"pack_side_chains,omitempty"`
	NumberOfPacksPerDesign    int      `json:"number_of_packs_per_design,omitempty"`
	PackWithLigandContext     *bool    `json:"pack_with_ligand_context,omitempty"`
	RepackEverything          *bool    `json:"repack_everything,omitempty"`
}
```

### Model types (the `model_type` enum)

`ligand_mpnn` (default), `protein_mpnn`, `soluble_mpnn`,
`global_label_membrane_mpnn`, `per_residue_label_membrane_mpnn`.

### `run.py` flag mapping (Stream B)

fova-owned, always passed: `--pdb_path <staged>`, `--out_folder /work/out`,
`--checkpoint_<model_type> /models/<ckpt>`, `--save_stats 0`; plus
`--checkpoint_path_sc /models/ligandmpnn_sc_v_32_002_16.pt` when
`pack_side_chains` is set.

Param → flag (a flag is omitted when its pointer is nil / its string is empty
/ its int is ≤ 0):

| field | flag | value form |
|---|---|---|
| `ModelType` | `--model_type` | string |
| `NumDesigns` | `--number_of_batches` | int |
| `BatchSize` | `--batch_size` | int |
| `Temperature` | `--temperature` | float |
| `Seed` | `--seed` | int |
| `RedesignedResidues` | `--redesigned_residues` | string |
| `FixedResidues` | `--fixed_residues` | string |
| `ChainsToDesign` | `--chains_to_design` | string |
| `BiasAA` | `--bias_AA` | string |
| `OmitAA` | `--omit_AA` | string |
| `BiasAAPerResidue` | `--bias_AA_per_residue` | staged path |
| `OmitAAPerResidue` | `--omit_AA_per_residue` | staged path |
| `LigandUseAtomContext` | `--ligand_mpnn_use_atom_context` | 1/0 |
| `LigandUseSideChainContext` | `--ligand_mpnn_use_side_chain_context` | 1/0 |
| `LigandCutoff` | `--ligand_mpnn_cutoff_for_score` | float |
| `SymmetryResidues` | `--symmetry_residues` | string |
| `SymmetryWeights` | `--symmetry_weights` | string |
| `HomoOligomer` | `--homo_oligomer` | 1/0 |
| `GlobalTransmembraneLabel` | `--global_transmembrane_label` | int |
| `TransmembraneBuried` | `--transmembrane_buried` | string |
| `TransmembraneInterface` | `--transmembrane_interface` | string |
| `PackSideChains` | `--pack_side_chains` | 1/0 |
| `NumberOfPacksPerDesign` | `--number_of_packs_per_design` | int |
| `PackWithLigandContext` | `--pack_with_ligand_context` | 1/0 |
| `RepackEverything` | `--repack_everything` | 1/0 |

`*bool` renders as `1` (true) / `0` (false). String values that contain
spaces (residue lists) are passed as one argv element.

### Checkpoint map (Stream B — `checkpointForModelType`)

```
protein_mpnn                    -> proteinmpnn_v_48_020.pt
ligand_mpnn                     -> ligandmpnn_v_32_010_25.pt
soluble_mpnn                    -> solublempnn_v_48_020.pt
global_label_membrane_mpnn      -> global_label_membrane_mpnn_v_48_020.pt
per_residue_label_membrane_mpnn -> per_residue_label_membrane_mpnn_v_48_020.pt
```

Stream B must confirm each filename against the `[[tools.ligandmpnn.weights]]`
entries in `internal/backends/local/tools.toml` (the four ProteinMPNN,
LigandMPNN, and membrane filenames are verified present there; confirm the
`soluble_mpnn` and side-chain `_sc_` filenames and adjust the map to the
actual `path =` values if they differ).

### FASTA score keys (Stream B)

LigandMPNN writes `out/seqs/<stem>.fa`. Record 0 is the native input
(skipped). Each design record's header is comma-separated `key=value` tokens;
read `overall_confidence`, `ligand_confidence`, `sequence_recovery` into
`designOut.Scores`. A missing key is simply absent — never an error.

---

# FOUNDATION (coordinator — committed before streams start)

**File:** `internal/domain/types.go`

1. Add the `LigandMPNNParams` struct from THE CONTRACT.
2. Add a `LigandMPNN *LigandMPNNParams` field to `MethodConfig`:

```go
type MethodConfig struct {
	SpecPath  string            `json:"spec_path,omitempty"`
	BoltzGen  *BoltzGenParams   `json:"boltzgen,omitempty"`
	LigandMPNN *LigandMPNNParams `json:"ligandmpnn,omitempty"`
}
```

3. Add `func (p LigandMPNNParams) Validate() error` — **value-shape**
   validation only (no filesystem; the workspace-path existence checks live in
   Stream A's `Execute` and Stream B's `Invoke`). It returns the first
   violation as a `design.ligandmpnn`-prefixed error, `nil` when valid:
   - `PDB` is non-empty;
   - `ModelType` is `""` or one of the five enum values (`""` ⇒ `ligand_mpnn`);
   - `FixedResidues`, `RedesignedResidues`, `TransmembraneBuried`,
     `TransmembraneInterface`, `SymmetryResidues` — every space-separated token
     matches `^[A-Za-z][0-9]+[A-Za-z]?$` (chain + number + optional icode);
   - `ChainsToDesign` — every comma-separated token is a non-empty chain id;
   - `BiasAA` — every comma-separated token is `<AA-letter>:<float>`;
   - `OmitAA` — only letters;
   - `GlobalTransmembraneLabel`, if set, is `0` or `1`;
   - `NumDesigns`, `BatchSize`, `NumberOfPacksPerDesign`, if non-zero, are `> 0`;
   - `Temperature`, if set, is `> 0`; `LigandCutoff`, if set, is `> 0`.

Commit: `feat(domain): LigandMPNNParams, MethodConfig.LigandMPNN, Validate`.
`go build ./...` and `go test ./internal/domain/` green before dispatch.

---

# STREAM A — bespoke `ligandMPNNTool`

Worktree `ligandmpnn/tool`. Package `internal/tools/design`. Read
`internal/tools/design/boltzgen.go` first — `ligandMPNNTool` mirrors
`boltzGenTool` exactly (the bespoke design-tool pattern).

### Task A1: Bespoke tool — type, schema, interface methods

**Files:**
- Modify: `internal/tools/design/ligandmpnn.go` (replace entirely)
- Modify: `internal/tools/design/design_test.go` (drop the ligandmpnn table row)
- Test: `internal/tools/design/ligandmpnn_test.go` (create)

> **Package-compiles note:** `design_test.go`'s `TestAntibodyEnzymeToolMetadata` has a `cases` table typed `func(...) *designTool` with a `NewLigandMPNNTool(...)` row. Once `NewLigandMPNNTool` returns `*ligandMPNNTool`, that row stops compiling — Step 3 removes it. `design_test.go:131` (`var _ tools.Tool = NewLigandMPNNTool(...)`) keeps compiling — `*ligandMPNNTool` satisfies `tools.Tool` — leave it.

- [ ] **Step 1: Write the failing test**

```go
package design

import (
	"encoding/json"
	"testing"
)

func TestLigandMPNNToolSchema(t *testing.T) {
	tool := NewLigandMPNNTool("/ws", nil, nil, nil)
	if tool.Name() != "design.ligandmpnn" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{"model_type", "pdb", "num_designs",
		"temperature", "redesigned_residues", "bias_AA", "pack_side_chains",
		"symmetry_residues", "global_transmembrane_label"} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestLigandMPNNToolRequiresConfirmation(t *testing.T) {
	if !NewLigandMPNNTool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.ligandmpnn must require confirmation — GPU design job")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestLigandMPNNTool`
Expected: FAIL — `NewLigandMPNNTool` still returns the shared `*designTool`.

- [ ] **Step 3: Write the implementation**

Replace the whole of `internal/tools/design/ligandmpnn.go`, mirroring
`boltzgen.go`. Define: `type LigandMPNNParams = domain.LigandMPNNParams` (the
package-local alias); a `ligandMPNNTool` struct with `workspaceRoot string`,
`mgr *jobs.Manager`, `backend backends.Backend`, `store *store.Store`;
`NewLigandMPNNTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *ligandMPNNTool` (**signature unchanged**).

Interface methods:
- `Name()` → `"design.ligandmpnn"`.
- `Description()` → designs protein sequences for a fixed backbone with
  LigandMPNN, ligand-aware; runs as an async GPU job.
- `InputSchema()` → `map[string]any`, `type: object`, `required: ["pdb"]`,
  `properties` for every `LigandMPNNParams` field (THE CONTRACT) — `model_type`
  an enum with the five values and `default: "ligand_mpnn"`; bounded numerics
  carry `minimum`; each property a `description`.
- `RequiresConfirmation(json.RawMessage) bool` → `true`.
- `EstimatedCostUSD(json.RawMessage) float64` → `2.0`.
- `EstimatedDuration(json.RawMessage) time.Duration` → `15 * time.Minute`.
- `Execute` — stub `return tools.Result{}, nil` (real body in A2).

Then remove the ligandmpnn row from `design_test.go`'s
`TestAntibodyEnzymeToolMetadata` `cases` table (the `func(...) *designTool { return NewLigandMPNNTool(...) }` entry and its trailing `"design.ligandmpnn"` label).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/ -run TestLigandMPNNTool` and `go vet ./internal/tools/design/`
Expected: PASS; package compiles.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/ligandmpnn.go internal/tools/design/ligandmpnn_test.go internal/tools/design/design_test.go
git commit -m "feat(design.ligandmpnn): bespoke tool type and typed schema"
```

### Task A2: Execute — validate, resolve paths, submit, persist

**Files:**
- Modify: `internal/tools/design/ligandmpnn.go`
- Test: `internal/tools/design/ligandmpnn_test.go`

Read `boltzgen.go`'s `Execute` + `persist` — `ligandMPNNTool` is the same shape.

- [ ] **Step 1: Write the failing test**

```go
func TestLigandMPNNExecuteRejectsBadInput(t *testing.T) {
	tool := NewLigandMPNNTool(t.TempDir(), nil, nil, nil)
	// No pdb — Validate rejects before any job/store access.
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected a validation error when pdb is missing")
	}
}
```

(Reuse the design package's existing test deps — `design_test.go` builds tools
with `(ws, mgr, backend, st)`; for the bad-input test a nil mgr/backend/store
is fine because `Validate` fails first. A job-submitting test is unnecessary
here — `persist`/submit mirror `boltzGenTool`, covered by the design package's
existing job tests for the shared tools and by integration.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestLigandMPNNExecute`
Expected: FAIL — `Execute` is the A1 stub (returns nil error).

- [ ] **Step 3: Write the implementation**

Implement `Execute`, mirroring `boltzGenTool.Execute`:
1. `json.Unmarshal` the input into `LigandMPNNParams`; wrap a parse error as
   `fmt.Errorf("invalid design.ligandmpnn request: %w", err)`.
2. `params.Validate()` — return its error directly on failure.
3. Resolve workspace paths when `t.workspaceRoot != ""`: rewrite `PDB`,
   `BiasAAPerResidue`, `OmitAAPerResidue` (each, when non-empty) via
   `tools.ResolveWorkspacePath`; a resolve error returns
   `fmt.Errorf("design.ligandmpnn: %w", err)`.
4. Re-marshal the resolved params.
5. Submit the job (`jobs.Spec{Kind: domain.JobCompute, Tool: "design.ligandmpnn", Backend: t.backend.Name(), Input: resolved, Run: ...}`); `Run` calls `t.backend.Run`, ticks `progress(0.95)`, then `t.persist(out)`.
6. `persist` — copy `boltzGenTool.persist` verbatim but with `Origin: domain.OriginRFDiff2MPNN`, `Application: domain.AppEnzyme`, tool name `"design.ligandmpnn"`.
7. Return `tools.Result{JobID: jobID, Display: "started design.ligandmpnn job " + string(jobID) + " — poll jobs.result for the designs", Provenance: domain.NewToolCallRef("design.ligandmpnn", input)}`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/design/` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/ligandmpnn.go internal/tools/design/ligandmpnn_test.go
git commit -m "feat(design.ligandmpnn): Execute with validation, path resolution, persist"
```

---

# STREAM B — local adapter

Worktree `ligandmpnn/adapter`. Package `internal/backends/local`. Read
`internal/backends/local/adapter_proteinmpnn.go` (the closest sibling — same
`run.py` family, FASTA output) and `adapter_boltz2.go` (the `os.Stat` weights
guard and the table-driven args pattern).

### Task B1: FASTA score parsing

**Files:**
- Create: `internal/backends/local/adapter_ligandmpnn.go`
- Test: `internal/backends/local/adapter_ligandmpnn_test.go` (create)

- [ ] **Step 1: Write the failing test**

```go
package local

import (
	"path/filepath"
	"os"
	"testing"
)

func TestParseLigandMPNNOutput(t *testing.T) {
	out := t.TempDir()
	seqs := filepath.Join(out, "seqs")
	if err := os.MkdirAll(seqs, 0o755); err != nil {
		t.Fatal(err)
	}
	// Record 0 = native (skipped); records 1-2 = designs.
	fa := ">1BC8, native\nMKQTAA\n" +
		">1BC8, id=1, overall_confidence=0.62, ligand_confidence=0.55, sequence_recovery=0.38\nMKDTAA\n" +
		">1BC8, id=2, overall_confidence=0.71, ligand_confidence=0.0, sequence_recovery=0.41\nMRDTAA\n"
	if err := os.WriteFile(filepath.Join(seqs, "1BC8.fa"), []byte(fa), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseLigandMPNNOutput(out)
	if err != nil {
		t.Fatalf("parseLigandMPNNOutput: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs (native skipped), got %d", len(designs))
	}
	if designs[0].Scores["overall_confidence"] != 0.62 ||
		designs[0].Scores["sequence_recovery"] != 0.38 {
		t.Errorf("design 0 scores wrong: %v", designs[0].Scores)
	}
	if designs[0].Sequence["A"] != "MKDTAA" {
		t.Errorf("design 0 sequence wrong: %v", designs[0].Sequence)
	}
}

func TestParseLigandMPNNOutputEmptyErrors(t *testing.T) {
	if _, err := parseLigandMPNNOutput(t.TempDir()); err == nil {
		t.Fatal("expected an error when no seqs/*.fa are present")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestParseLigandMPNNOutput`
Expected: FAIL — `parseLigandMPNNOutput` undefined.

- [ ] **Step 3: Write the implementation**

Create `adapter_ligandmpnn.go`. Add `parseLigandMPNNOutput(outDir string) ([]designOut, error)`:
- glob `outDir/seqs/*.fa`; for each, parse with `proteinio.ParseFASTA`
  (`pkg/proteinio`, as `adapter_proteinmpnn.go` does);
- skip record 0 (native); skip records with an empty sequence body;
- for each design record: `Sequence` is `splitChains(rec.Sequence)` (the
  existing helper in `adapter_proteinmpnn.go` — chains joined by `/` → A,B,…);
  `Scores` is built from the header's comma-separated `key=value` tokens for
  `overall_confidence`, `ligand_confidence`, `sequence_recovery`;
  `StructureFile` is the matching `outDir/backbones/<stem>_<i>.pdb` if it
  exists, else `outDir/packed/<stem>_<i>_1.pdb` if it exists, else `""`.
- empty result ⇒ `fmt.Errorf("design.ligandmpnn: no designed sequences found in %s", outDir)`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/ -run TestParseLigandMPNNOutput`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_ligandmpnn.go internal/backends/local/adapter_ligandmpnn_test.go
git commit -m "feat(ligandmpnn adapter): FASTA-header score ingestion"
```

### Task B2: Argument mapping

**Files:**
- Modify: `internal/backends/local/adapter_ligandmpnn.go` (add `ligandMPNNArgs`, `checkpointForModelType`)
- Test: `internal/backends/local/adapter_ligandmpnn_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestLigandMPNNArgs(t *testing.T) {
	temp := 0.2
	got := ligandMPNNArgs(domain.LigandMPNNParams{
		ModelType: "ligand_mpnn", NumDesigns: 8, Temperature: &temp,
		RedesignedResidues: "A23 A24",
	})
	joined := strings.Join(got, " ")
	for _, want := range []string{
		"--model_type ligand_mpnn", "--number_of_batches 8",
		"--temperature 0.2", "--redesigned_residues A23 A24",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q in %q", want, joined)
		}
	}
	// Unset optionals omit their flags.
	if strings.Contains(strings.Join(ligandMPNNArgs(domain.LigandMPNNParams{}), " "), "--seed") {
		t.Error("an unset seed must omit the flag")
	}
}

func TestCheckpointForModelType(t *testing.T) {
	if got := checkpointForModelType("ligand_mpnn"); got == "" {
		t.Error("ligand_mpnn must map to a checkpoint filename")
	}
	if got := checkpointForModelType(""); got != checkpointForModelType("ligand_mpnn") {
		t.Error("empty model_type must default to the ligand_mpnn checkpoint")
	}
}
```

Add `domain` and `strings` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run 'TestLigandMPNNArgs|TestCheckpointForModelType'`
Expected: FAIL — `ligandMPNNArgs` / `checkpointForModelType` undefined.

- [ ] **Step 3: Write the implementation**

Add `checkpointForModelType(modelType string) string` — the map from THE
CONTRACT's "Checkpoint map" (`""` defaults to the `ligand_mpnn` entry).
**Confirm each filename against `internal/backends/local/tools.toml`'s
`[[tools.ligandmpnn.weights]]` `path =` values** and correct any mismatch.

Add `ligandMPNNArgs(p domain.LigandMPNNParams) []string` — table-driven,
mirroring `boltz2Args`/`boltzGenArgs`. Append each agent-facing flag from THE
CONTRACT's mapping table when its field is set (pointer non-nil / string
non-empty / int > 0). `*bool` → `"1"`/`"0"`. A residue-list string is one
argv element. The fova-owned flags (`--pdb_path`, `--out_folder`,
`--checkpoint_*`, `--save_stats`, `--checkpoint_path_sc`) are added in `Invoke`
(Task B3), not here.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/ -run 'TestLigandMPNNArgs|TestCheckpointForModelType'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_ligandmpnn.go internal/backends/local/adapter_ligandmpnn_test.go
git commit -m "feat(ligandmpnn adapter): run.py argument and checkpoint mapping"
```

### Task B3: Invoke

**Files:**
- Modify: `internal/backends/local/adapter_ligandmpnn.go`
- Test: `internal/backends/local/adapter_ligandmpnn_test.go`

Read `adapter_boltz2.go`'s `Invoke` for the runtime/image/weights-guard
structure and `adapter_proteinmpnn.go` for input staging.

- [ ] **Step 1: Write the failing test**

```go
func TestLigandMPNNAdapterInvoke(t *testing.T) {
	env := ligandMPNNTestEnv(t) // see Step 3 note on the env helper
	pdb := filepath.Join(t.TempDir(), "bb.pdb")
	if err := os.WriteFile(pdb, []byte("ATOM\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubContainerRuntime(t, func(args []string) error {
		if len(args) < 2 || args[1] != "run" {
			return nil
		}
		seqs := filepath.Join(env.WorkDir, "out", "seqs")
		if err := os.MkdirAll(seqs, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(seqs, "bb.fa"),
			[]byte(">bb, native\nMKQ\n>bb, id=1, overall_confidence=0.7\nMRD\n"), 0o644)
	})
	body := []byte(`{"pdb":"` + pdb + `","model_type":"ligand_mpnn"}`)
	out, err := ligandMPNNAdapter{}.Invoke(context.Background(), env, body)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var resp designsEnvelope
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("not a designs envelope: %v", err)
	}
	if len(resp.Designs) != 1 || resp.Designs[0].Scores["overall_confidence"] != 0.7 {
		t.Fatalf("want 1 scored design, got %+v", resp.Designs)
	}
}

func TestLigandMPNNAdapterInvokeRejectsMissingPDB(t *testing.T) {
	env := ligandMPNNTestEnv(t)
	if _, err := (ligandMPNNAdapter{}).Invoke(context.Background(), env, []byte(`{}`)); err == nil {
		t.Fatal("expected an error when pdb is missing")
	}
}
```

Add a `ligandMPNNTestEnv(t)` helper to the test file modelled on
`boltz2TestEnv` in `adapter_boltz2_test.go`: `LoadRegistry(t.TempDir())`,
`os.MkdirAll(ModelsRoot(home, "ligandmpnn"), 0o755)`, the `ligandmpnn` recipe,
an `AdapterEnv` with `WorkDir: t.TempDir()`. `stubContainerRuntime` is the
shared helper already in the package.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestLigandMPNNAdapterInvoke`
Expected: FAIL — `ligandMPNNAdapter` undefined.

- [ ] **Step 3: Write the implementation**

Add the adapter: `init()` calls `registerAdapter(ligandMPNNAdapter{})`;
`ligandMPNNAdapter` with `AgentTool() "design.ligandmpnn"` and `Recipe() "ligandmpnn"`. `Invoke`:
1. `json.Unmarshal` the request into `domain.LigandMPNNParams`; wrap parse errors.
2. `req.PDB` non-empty, ends `.pdb`, exists (`os.Stat`) — else a clear error.
3. `env.Registry` non-nil; `env.Recipe.ImageTag` non-empty (`run /install ligandmpnn`).
4. Stage the PDB into `env.WorkDir` (`copyFile`); stage `BiasAAPerResidue` /
   `OmitAAPerResidue` JSON files if set; remember the staged `/work/...` paths.
5. `os.MkdirAll(filepath.Join(env.WorkDir, "out"))`. `env.Tick(0.05)`.
6. `rt := Detect()`; require `rt.Available()` and `rt.ImageExists`.
7. `modelsCache := ModelsRoot(env.Registry.Home(), "ligandmpnn")` — `os.Stat`;
   absent ⇒ `fmt.Errorf("design.ligandmpnn: weights cache %s missing — run /install ligandmpnn", modelsCache)`.
8. Build `cmd`: `--pdb_path /work/<staged.pdb>`, `--out_folder /work/out`,
   `--checkpoint_<model_type> /models/<checkpointForModelType(...)>`,
   `--save_stats 0`; `--checkpoint_path_sc /models/ligandmpnn_sc_v_32_002_16.pt`
   when `PackSideChains` is set true; then append `ligandMPNNArgs(req)`; rewrite
   the staged `--bias_AA_per_residue`/`--omit_AA_per_residue` values to their
   `/work/...` paths.
9. `rt.RunContainer` with mounts `{env.WorkDir:/work}` and `{modelsCache:/models}`,
   `GPU: env.Recipe.GPU`, `Workdir: "/work"`, `Log: env.LogWriter()`. `env.Tick(0.95)`.
10. `designs, err := parseLigandMPNNOutput(filepath.Join(env.WorkDir, "out"))`.
11. `return json.Marshal(designsEnvelope{Designs: designs})`.

The recipe's ENTRYPOINT is `python /opt/ligandmpnn/run.py`, so `Cmd` is the
flags only (no `run.py`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backends/local/`
Expected: PASS (whole package).

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_ligandmpnn.go internal/backends/local/adapter_ligandmpnn_test.go
git commit -m "feat(ligandmpnn adapter): Invoke — staging, run.py invocation, weights guard"
```

---

# STREAM C — `/plan` integration

Worktree `ligandmpnn/plan`. Packages `internal/tools/plan` and `internal/tui`.
Read `plan.go`'s `applyBoltzGenMethodConfig` and `render.go`'s
`renderBoltzGenSection` — the LigandMPNN equivalents mirror them.

### Task C1: `plan.create` method-config

**Files:**
- Modify: `internal/tools/plan/plan.go`
- Test: `internal/tools/plan/plan_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPlanCreateLigandMPNNMethodConfig(t *testing.T) {
	// A LigandMPNN plan with method_params must land MethodConfig.LigandMPNN.
	cfg := applyLigandMPNNParams(t, `{"method_params":{"pdb":"bb.pdb","model_type":"ligand_mpnn"}}`)
	if cfg == nil || cfg.LigandMPNN == nil {
		t.Fatal("MethodConfig.LigandMPNN must be populated")
	}
	if cfg.LigandMPNN.PDB != "bb.pdb" {
		t.Errorf("pdb = %q", cfg.LigandMPNN.PDB)
	}
	// An invalid params object (no pdb) must be rejected.
	if _, err := applyLigandMPNNParamsErr(`{"method_params":{"model_type":"ligand_mpnn"}}`); err == nil {
		t.Error("a LigandMPNN plan with no pdb must be rejected")
	}
}
```

Implement the two tiny test helpers `applyLigandMPNNParams` /
`applyLigandMPNNParamsErr` in `plan_test.go` by calling the new
`applyLigandMPNNMethodConfig` against a `*domain.DesignPlan` with
`Method` set to the LigandMPNN canonical name — follow how the existing
`plan_test.go` BoltzGen tests exercise `applyBoltzGenMethodConfig`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/plan/ -run TestPlanCreateLigandMPNN`
Expected: FAIL — `applyLigandMPNNMethodConfig` undefined.

- [ ] **Step 3: Write the implementation**

Add `applyLigandMPNNMethodConfig(input json.RawMessage, p *domain.DesignPlan) error` mirroring `applyBoltzGenMethodConfig`:
- unmarshal `{ "method_params": *domain.LigandMPNNParams }` from `input`;
- if `method_params` is nil ⇒ `fmt.Errorf("plan.create: method LigandMPNN requires method_params — the LigandMPNN run configuration (at minimum a pdb backbone path)")`;
- `params.Validate()` — return its error on failure;
- set `p.MethodConfig = &domain.MethodConfig{LigandMPNN: params}`.

In `plan.create`, alongside the `if method == MethodBoltzGen` block, add
`if method == MethodLigandMPNN { if err := t.applyLigandMPNNMethodConfig(input, &p); err != nil { return tools.Result{}, err } }`.

Extend the `method_params` schema description in the `plan.create`
`InputSchema()` to note it also carries LigandMPNN params for a LigandMPNN
method (a one-line addition to the existing description string).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/plan/ -run TestPlanCreateLigandMPNN`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/plan/plan.go internal/tools/plan/plan_test.go
git commit -m "feat(plan): LigandMPNN method-config in plan.create"
```

### Task C2: `/plan` rendering

**Files:**
- Modify: `internal/tools/plan/render.go`
- Test: `internal/tools/plan/render_test.go`
- Modify: `internal/tui/plan.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRenderLigandMPNNSection(t *testing.T) {
	p := &domain.DesignPlan{
		ID: "p_x", Method: "LigandMPNN",
		MethodConfig: &domain.MethodConfig{LigandMPNN: &domain.LigandMPNNParams{
			ModelType: "ligand_mpnn", PDB: "bb.pdb", NumDesigns: 8,
		}},
	}
	out := RenderPlan(p)
	for _, want := range []string{"LigandMPNN", "ligand_mpnn", "bb.pdb"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered plan missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/plan/ -run TestRenderLigandMPNNSection`
Expected: FAIL — no LigandMPNN section is rendered.

- [ ] **Step 3: Write the implementation**

In `render.go`, the `if p.MethodConfig != nil { renderBoltzGenSection(...) }`
call site becomes a dispatch on which config is populated:

```go
if mc := p.MethodConfig; mc != nil {
	switch {
	case mc.BoltzGen != nil || mc.SpecPath != "":
		renderBoltzGenSection(&b, mc, opts)
	case mc.LigandMPNN != nil:
		renderLigandMPNNSection(&b, mc.LigandMPNN)
	}
}
```

Add `renderLigandMPNNSection(b *strings.Builder, lm *domain.LigandMPNNParams)`
mirroring `renderBoltzGenSection`'s `labelRow` style: a
`"\n  LigandMPNN design configuration\n"` header, then `labelRow` lines for
model type (blank ⇒ `"ligand_mpnn (default)"`), input PDB, num designs,
temperature (when set), redesigned/fixed residues (when set), and
side-chain packing (when set).

In `internal/tui/plan.go`, if it special-cases BoltzGen plan rendering (e.g.
to pass `RenderPlanOpts`), make the LigandMPNN plan render through the same
path — a LigandMPNN plan has no spec file or check result, so a plain
`RenderPlan` (or `RenderPlanWithOpts` with a zero `RenderPlanOpts`) is correct.
If `tui/plan.go` already calls `RenderPlan`/`RenderPlanWithOpts` generically,
no change is needed there beyond confirming it compiles.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/plan/ ./internal/tui/` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/plan/render.go internal/tools/plan/render_test.go internal/tui/plan.go
git commit -m "feat(plan): render the LigandMPNN method-config section"
```

---

# STREAM D — grounding skill

Worktree `ligandmpnn/skill`. Read `internal/assets/embed/skills/design-binder.md`
and `boltzgen-spec.md` for the house style; every embedded skill needs YAML
frontmatter (`name`, `description`).

### Task D1: Write `ligandmpnn-design.md`

**Files:**
- Create: `internal/assets/embed/skills/ligandmpnn-design.md`

- [ ] **Step 1: Write the skill**

Create `internal/assets/embed/skills/ligandmpnn-design.md`. Start with YAML
frontmatter:

```
---
name: ligandmpnn-design
description: Design protein sequences for a fixed backbone with LigandMPNN (design.ligandmpnn)
---
```

Then ~90–120 lines, house style, with concrete `design.ligandmpnn` tool-call
JSON examples matching THE CONTRACT. Cover: the five model types and when to
pick each (`ligand_mpnn` for ligand-bound active sites — the default;
`soluble_mpnn` for soluble globular proteins; the membrane models for
transmembrane proteins); residue selection (`redesigned_residues` vs
`fixed_residues` vs `chains_to_design`, and the `A23`/`B42D` reference
format); `bias_AA` (`W:3.0,P:-2.0`) and `omit_AA` (`CG`) syntax; when to
enable `pack_side_chains`; reading the result scores (`overall_confidence`,
`ligand_confidence`, `sequence_recovery` — all 0–1, higher better); and one
worked example designing an enzyme active-site sequence around a bound ligand.

- [ ] **Step 2: Verify it loads**

Run: `go test ./internal/assets/` and `go build ./...`
Expected: PASS — the assets layer discovers the new skill and its frontmatter.

- [ ] **Step 3: Commit**

```bash
git add internal/assets/embed/skills/ligandmpnn-design.md
git commit -m "docs(design.ligandmpnn): ligandmpnn-design grounding skill"
```

---

# INTEGRATION (sequential — after Streams A, B, C, D complete)

Run by the coordinator in the `feat/ligandmpnn` worktree.

### Task INT1: Merge the four streams

- [ ] **Step 1: Merge each stream branch**

```bash
git merge --no-ff ligandmpnn/tool    -m "merge: design.ligandmpnn bespoke tool"
git merge --no-ff ligandmpnn/adapter -m "merge: design.ligandmpnn adapter"
git merge --no-ff ligandmpnn/plan    -m "merge: design.ligandmpnn /plan integration"
git merge --no-ff ligandmpnn/skill   -m "merge: ligandmpnn-design skill"
```

Expected: four clean merges — the streams touch disjoint files.

- [ ] **Step 2: Build, test, gofmt**

Run: `go build ./...`, then `go test ./...`, then `gofmt -l internal/ cmd/`
Expected: build OK; every package PASS; gofmt prints nothing.

### Task INT2: Asset skill-count check

- [ ] **Step 1: Update the embedded-skill count if asserted**

`internal/assets/assets_test.go` asserts the built-in skill count (it was
bumped to 10 when boltz2/chai1 landed). Adding `ligandmpnn-design.md` makes it
11 — run `go test ./internal/assets/`; if it fails on the count, bump the two
assertions (`!= N` and the message) from 10 to 11 and re-run.

- [ ] **Step 2: Final commit**

```bash
git add -A
git commit -m "feat(design.ligandmpnn): full LigandMPNN integration — tool 3 of 6"
```

The GPU end-to-end run is user-validated on the GB10 (LigandMPNN is
pure-PyTorch — it runs on aarch64).

---

## Self-Review

- **Spec coverage:** Component A (tool) → Stream A. Component B (`/plan`) →
  Foundation (`MethodConfig.LigandMPNN`) + Stream C. Component C (adapter) →
  Stream B. Component D (preflight) → `LigandMPNNParams.Validate` in the
  Foundation, called by A2 and C1. Component E (score ingestion) → Stream B
  (B1). Component F (skill) → Stream D. Component G (testing) → tests in every
  task + INT1.
- **Placeholder scan:** none — every step has concrete code, commands, and
  expected output. The two checkpoint filenames Stream B must confirm against
  `tools.toml` are an explicit verification step, not a placeholder.
- **Type consistency:** `domain.LigandMPNNParams` is defined once in the
  Foundation and imported everywhere — no copies, no drift. `ligandMPNNArgs`,
  `checkpointForModelType`, `parseLigandMPNNOutput`, `applyLigandMPNNMethodConfig`,
  `renderLigandMPNNSection`, `LigandMPNNParams.Validate` each keep one
  signature throughout.
