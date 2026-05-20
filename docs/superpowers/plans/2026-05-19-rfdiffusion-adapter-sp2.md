# RFdiffusion Adapter — SP2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the agent's `design.rfdiffusion` tool to run RFdiffusion through the local backend, as a second `ToolAdapter`.

**Architecture:** A `rfdiffusionAdapter` in `internal/backends/local/` following the SP1 pattern — it passes the agent's contig map straight to RFdiffusion's `run_inference.py`, then collects the generated backbone PDBs into the `{"designs":[…]}` schema. `AdapterEnv` gains a `Registry` field so the adapter can resolve the weights data asset and a persistent output directory.

**Tech Stack:** Go, RFdiffusion (Python, uv-managed, Hydra-config CLI).

**Spec:** `docs/superpowers/specs/2026-05-19-proteus-rfdiffusion-adapter-design.md`.

**Branch:** `feat/rfdiffusion-adapter` (already created).

---

## File map

| File | Change | Task |
|---|---|---|
| `internal/tools/design/design.go` | modify — add `contigs` to the shared `InputSchema()` | 1 |
| `internal/tools/design/design_test.go` | modify — test the `contigs` property | 1 |
| `internal/backends/local/adapter_rfdiffusion.go` | **new** — output parser (T2), then the adapter (T3) | 2, 3 |
| `internal/backends/local/adapter_rfdiffusion_test.go` | **new** — parser tests (T2), adapter tests (T3) | 2, 3 |
| `internal/backends/local/adapter.go` | modify — `AdapterEnv.Registry`; `RunDesign` passes it | 3 |
| `internal/backends/local/adapter_test.go` | modify — the "no adapter" test moves off `design.rfdiffusion` | 3 |
| `internal/backends/backend_test.go` | modify — the "no adapter" test moves off `design.rfdiffusion` | 3 |

**Parallelism:** **Task 1** (`internal/tools/design`) and **Task 2** (`internal/backends/local`) touch disjoint packages/files and may run in parallel. **Task 3** follows Task 2 (same file `adapter_rfdiffusion.go`).

**Note:** RFdiffusion's parser only globs filenames — it never reads PDB content — so tests synthesize `out_*.pdb` files in temp dirs; no committed fixture is needed.

---

## Task 1: Add `contigs` to the design tool input schema

`design.rfdiffusion` needs a contig map, but the shared `designTool.InputSchema()` only advertises `target`/`hotspots`/`num_designs`. Add `contigs` so the agent knows to supply it (ProteinMPNN/BindCraft harmlessly ignore the extra advertised field).

**Files:**
- Modify: `internal/tools/design/design.go`
- Test: `internal/tools/design/design_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tools/design/design_test.go`:

```go
func TestDesignToolSchemaAdvertisesContigs(t *testing.T) {
	tool := NewRFdiffusionTool(nil, nil, nil)
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("InputSchema has no properties map")
	}
	if _, ok := props["contigs"]; !ok {
		t.Error("InputSchema must advertise the contigs property")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestDesignToolSchemaAdvertisesContigs`
Expected: FAIL — `props["contigs"]` is absent.

- [ ] **Step 3: Add the `contigs` property**

In `internal/tools/design/design.go`, the `InputSchema` method's `properties` map currently reads:

```go
		"properties": map[string]any{
			"target":      map[string]any{"type": "string", "description": "Target PDB ID or file path"},
			"hotspots":    map[string]any{"type": "string", "description": "Target hotspot residues"},
			"num_designs": map[string]any{"type": "integer", "description": "Number of designs to generate"},
		},
```

Add a `contigs` entry:

```go
		"properties": map[string]any{
			"target":      map[string]any{"type": "string", "description": "Target PDB ID or file path"},
			"hotspots":    map[string]any{"type": "string", "description": "Target hotspot residues"},
			"num_designs": map[string]any{"type": "integer", "description": "Number of designs to generate"},
			"contigs":     map[string]any{"type": "string", "description": "RFdiffusion contig map (design.rfdiffusion only)"},
		},
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tools/design/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/design.go internal/tools/design/design_test.go
git commit -m "$(printf 'feat: advertise the contigs input for design.rfdiffusion\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 2: RFdiffusion output parser

RFdiffusion writes backbone PDBs as `<output_prefix>_0.pdb`, `<output_prefix>_1.pdb`, … The parser collects them into designs with `structure_file` set.

**Files:**
- Create: `internal/backends/local/adapter_rfdiffusion.go`
- Create: `internal/backends/local/adapter_rfdiffusion_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/backends/local/adapter_rfdiffusion_test.go`:

```go
package local

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRFdiffusionOutput(t *testing.T) {
	outDir := t.TempDir()
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(filepath.Join(outDir, fmt.Sprintf("out_%d.pdb", i)),
			[]byte("ATOM\nEND\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	designs, err := parseRFdiffusionOutput(outDir)
	if err != nil {
		t.Fatalf("parseRFdiffusionOutput: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(designs))
	}
	if designs[0].StructureFile == "" {
		t.Error("structure_file must be set")
	}
	if len(designs[0].Sequence) != 0 {
		t.Error("RFdiffusion designs carry no sequence")
	}
}

func TestParseRFdiffusionOutputEmptyErrors(t *testing.T) {
	if _, err := parseRFdiffusionOutput(t.TempDir()); err == nil {
		t.Fatal("expected an error when no backbone PDBs are present")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/backends/local/ -run TestParseRFdiffusion`
Expected: FAIL — `undefined: parseRFdiffusionOutput`

- [ ] **Step 3: Create `internal/backends/local/adapter_rfdiffusion.go`**

```go
package local

import (
	"fmt"
	"path/filepath"
)

// parseRFdiffusionOutput collects the backbone PDBs RFdiffusion wrote under
// outDir (out_0.pdb, out_1.pdb, ...) into designs with the structure file set.
// RFdiffusion emits backbones only — sequence and scores are left empty.
func parseRFdiffusionOutput(outDir string) ([]designOut, error) {
	files, err := filepath.Glob(filepath.Join(outDir, "out_*.pdb"))
	if err != nil {
		return nil, err
	}
	var designs []designOut
	for _, f := range files {
		designs = append(designs, designOut{
			Sequence:      map[string]string{},
			StructureFile: f,
			Scores:        map[string]float64{},
		})
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("design.rfdiffusion: no backbone PDBs found in %s", outDir)
	}
	return designs, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/backends/local/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_rfdiffusion.go internal/backends/local/adapter_rfdiffusion_test.go
git commit -m "$(printf 'feat: add the RFdiffusion backbone-output parser\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 3: RFdiffusion adapter

**Files:**
- Modify: `internal/backends/local/adapter.go`
- Modify: `internal/backends/local/adapter_rfdiffusion.go`
- Modify: `internal/backends/local/adapter_rfdiffusion_test.go`
- Modify: `internal/backends/local/adapter_test.go`
- Modify: `internal/backends/backend_test.go`

- [ ] **Step 1: Extend `AdapterEnv` with the registry**

In `internal/backends/local/adapter.go`, the `AdapterEnv` struct currently reads:

```go
type AdapterEnv struct {
	Recipe  ToolRecipe // resolved recipe — InstallDir and VenvDir are expanded
	Run     CmdRunner  // command runner (production: bashRunner; tests: a stub)
	WorkDir string     // a fresh temp directory the adapter may write into
}
```

Add a `Registry` field:

```go
type AdapterEnv struct {
	Recipe   ToolRecipe // resolved recipe — InstallDir and VenvDir are expanded
	Run      CmdRunner  // command runner (production: bashRunner; tests: a stub)
	WorkDir  string     // a fresh temp directory the adapter may write into
	Registry *Registry  // for DataAsset lookups and Home()
}
```

In the same file, `RunDesign`'s final line currently reads:

```go
	return adapter.Invoke(ctx, AdapterEnv{Recipe: rec, Run: bashRunner, WorkDir: workDir}, request)
```

Change it to pass the registry:

```go
	return adapter.Invoke(ctx, AdapterEnv{Recipe: rec, Run: bashRunner, WorkDir: workDir, Registry: reg}, request)
```

- [ ] **Step 2: Write the failing tests**

Append to `internal/backends/local/adapter_rfdiffusion_test.go` (add `"context"`, `"encoding/json"`, `"strings"` to its import block):

```go
// rfdiffStubRunner records commands and, on the run_inference call, drops two
// backbone PDBs into the directory named by inference.output_prefix.
func rfdiffStubRunner(ran *[]string) CmdRunner {
	return func(ctx context.Context, dir, cmd string) (string, error) {
		*ran = append(*ran, cmd)
		for _, tok := range strings.Fields(cmd) {
			if !strings.HasPrefix(tok, "inference.output_prefix=") {
				continue
			}
			outDir := filepath.Dir(strings.TrimPrefix(tok, "inference.output_prefix="))
			for i := 0; i < 2; i++ {
				p := filepath.Join(outDir, fmt.Sprintf("out_%d.pdb", i))
				if err := os.WriteFile(p, []byte("ATOM\nEND\n"), 0o644); err != nil {
					return "", err
				}
			}
		}
		return "ok", nil
	}
}

// rfdiffTestEnv builds an AdapterEnv with an installed-looking recipe and a
// registry whose rfdiffusion_weights directory exists on disk.
func rfdiffTestEnv(t *testing.T, ran *[]string) AdapterEnv {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	asset, ok := reg.DataAsset("rfdiffusion_weights")
	if !ok {
		t.Fatal("rfdiffusion_weights data asset is not registered")
	}
	if err := os.MkdirAll(asset.TargetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return AdapterEnv{
		Recipe:   ToolRecipe{Name: "rfdiffusion", InstallDir: t.TempDir(), VenvDir: t.TempDir()},
		Run:      rfdiffStubRunner(ran),
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
}

func TestRFdiffusionAdapterInvoke(t *testing.T) {
	var ran []string
	env := rfdiffTestEnv(t, &ran)

	out, err := rfdiffusionAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"contigs":"50-70","num_designs":2}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var env2 designsEnvelope
	if err := json.Unmarshal(out, &env2); err != nil {
		t.Fatalf("output is not valid designs JSON: %v", err)
	}
	if len(env2.Designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(env2.Designs))
	}
	if env2.Designs[0].StructureFile == "" {
		t.Error("design structure_file must be set")
	}
	if len(ran) != 1 {
		t.Fatalf("want 1 command, got %d: %v", len(ran), ran)
	}
	if !strings.Contains(ran[0], "contigmap.contigs=[50-70]") {
		t.Errorf("command must carry the contig map: %s", ran[0])
	}
	if !strings.Contains(ran[0], "inference.num_designs=2") {
		t.Errorf("command must carry num_designs: %s", ran[0])
	}
	if !strings.Contains(ran[0], "Base_ckpt.pt") {
		t.Errorf("no target → Base_ckpt.pt expected: %s", ran[0])
	}
}

func TestRFdiffusionAdapterInvokeComplexCheckpoint(t *testing.T) {
	var ran []string
	env := rfdiffTestEnv(t, &ran)
	target := filepath.Join(t.TempDir(), "t.pdb")
	if err := os.WriteFile(target, []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := rfdiffusionAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"contigs":"A1-50/0 50-70","target":"`+target+`","hotspots":"A30,A33"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.Contains(ran[0], "Complex_base_ckpt.pt") {
		t.Errorf("a target → Complex_base_ckpt.pt expected: %s", ran[0])
	}
	if !strings.Contains(ran[0], "inference.input_pdb="+target) {
		t.Errorf("command must carry the target pdb: %s", ran[0])
	}
	if !strings.Contains(ran[0], "ppi.hotspot_res=[A30,A33]") {
		t.Errorf("command must carry the hotspots: %s", ran[0])
	}
}

func TestRFdiffusionAdapterInvokeMissingContigs(t *testing.T) {
	var ran []string
	env := rfdiffTestEnv(t, &ran)
	if _, err := (rfdiffusionAdapter{}).Invoke(context.Background(), env, []byte(`{"num_designs":1}`)); err == nil {
		t.Fatal("expected an error when contigs is missing")
	}
}

func TestRFdiffusionAdapterInvokeWeightsMissing(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	// rfdiffusion_weights directory is deliberately NOT created.
	env := AdapterEnv{
		Recipe:   ToolRecipe{InstallDir: t.TempDir(), VenvDir: t.TempDir()},
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = rfdiffusionAdapter{}.Invoke(context.Background(), env, []byte(`{"contigs":"50-70"}`))
	if err == nil {
		t.Fatal("expected an error when the weights directory is absent")
	}
}

func TestRFdiffusionAdapterInvokeNotInstalled(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:   ToolRecipe{InstallDir: filepath.Join(t.TempDir(), "gone"), VenvDir: t.TempDir()},
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = rfdiffusionAdapter{}.Invoke(context.Background(), env, []byte(`{"contigs":"50-70"}`))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want a 'not installed' error, got: %v", err)
	}
}

func TestRunDesignRFdiffusionIsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Missing contigs makes Invoke fail fast — which still proves
	// design.rfdiffusion is registered and dispatched.
	_, err = RunDesign(context.Background(), reg, "design.rfdiffusion", []byte(`{"num_designs":1}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("design.rfdiffusion must be registered, got: %v", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/backends/local/ -run 'TestRFdiffusionAdapter|TestRunDesignRFdiffusion'`
Expected: FAIL — `undefined: rfdiffusionAdapter`

- [ ] **Step 4: Append the adapter to `internal/backends/local/adapter_rfdiffusion.go`**

Add `"context"`, `"encoding/json"`, `"os"`, `"strings"`, `"time"` to the file's import block, then append:

```go
// init registers the RFdiffusion adapter with the local backend.
func init() { registerAdapter(rfdiffusionAdapter{}) }

// rfdiffusionAdapter wires design.rfdiffusion to the installed RFdiffusion tool.
type rfdiffusionAdapter struct{}

func (rfdiffusionAdapter) AgentTool() string { return "design.rfdiffusion" }
func (rfdiffusionAdapter) Recipe() string    { return "rfdiffusion" }

// rfdiffusionRequest is the subset of the design.rfdiffusion input the adapter uses.
type rfdiffusionRequest struct {
	Contigs    string `json:"contigs"`
	Target     string `json:"target"`
	Hotspots   string `json:"hotspots"`
	NumDesigns int    `json:"num_designs"`
}

// Invoke runs RFdiffusion for one contig map and collects the generated
// backbone PDBs into the {"designs":[...]} schema.
func (rfdiffusionAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req rfdiffusionRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion: invalid request: %w", err)
	}
	contigs := strings.TrimSpace(req.Contigs)
	if contigs == "" {
		return nil, fmt.Errorf("design.rfdiffusion: contigs is required (the RFdiffusion contig map)")
	}
	target := strings.TrimSpace(req.Target)
	if target != "" {
		if !strings.HasSuffix(target, ".pdb") {
			return nil, fmt.Errorf("design.rfdiffusion: target %q must be a .pdb file", target)
		}
		if info, err := os.Stat(target); err != nil || info.IsDir() {
			return nil, fmt.Errorf("design.rfdiffusion: target %q does not exist", target)
		}
	}
	if info, err := os.Stat(env.Recipe.InstallDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.rfdiffusion: rfdiffusion is not installed (run /install rfdiffusion)")
	}
	if info, err := os.Stat(env.Recipe.VenvDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.rfdiffusion: rfdiffusion is not installed (run /install rfdiffusion)")
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("design.rfdiffusion: adapter registry unavailable")
	}
	asset, ok := env.Registry.DataAsset("rfdiffusion_weights")
	if !ok {
		return nil, fmt.Errorf("design.rfdiffusion: the rfdiffusion_weights data asset is not registered")
	}
	if info, err := os.Stat(asset.TargetDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.rfdiffusion: RFdiffusion weights missing — install the rfdiffusion_weights data asset")
	}
	ckpt := filepath.Join(asset.TargetDir, "Base_ckpt.pt")
	if target != "" {
		ckpt = filepath.Join(asset.TargetDir, "Complex_base_ckpt.pt")
	}
	numDesigns := req.NumDesigns
	if numDesigns < 1 {
		numDesigns = 1
	}

	outDir := filepath.Join(env.Registry.Home(), "designs",
		fmt.Sprintf("rfdiffusion-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	cmd := fmt.Sprintf(
		"%s %s inference.output_prefix=%s inference.num_designs=%d inference.ckpt_override_path=%s 'contigmap.contigs=[%s]'",
		filepath.Join(env.Recipe.VenvDir, "bin", "python"),
		filepath.Join(env.Recipe.InstallDir, "scripts", "run_inference.py"),
		filepath.Join(outDir, "out"), numDesigns, ckpt, contigs)
	if target != "" {
		cmd += " inference.input_pdb=" + target
	}
	if h := strings.TrimSpace(req.Hotspots); h != "" {
		cmd += fmt.Sprintf(" 'ppi.hotspot_res=[%s]'", h)
	}
	if out, err := env.Run(ctx, env.Recipe.InstallDir, cmd); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion: run failed: %w\n%s", err, out)
	}

	designs, err := parseRFdiffusionOutput(outDir)
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
```

- [ ] **Step 5: Move the SP1 "no adapter" tests off `design.rfdiffusion`**

`design.rfdiffusion` now has an adapter, so two SP1 tests that used it as the "no adapter" example must switch to `design.bindcraft` (still unwired until SP3).

In `internal/backends/local/adapter_test.go`, `TestRunDesignNoAdapterMessageIsClear` currently reads:

```go
	// design.rfdiffusion has no adapter in SP1 — the message must say so plainly.
	_, err = RunDesign(context.Background(), reg, "design.rfdiffusion", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
```

Replace those lines with:

```go
	// design.bindcraft has no adapter yet — the message must say so plainly.
	_, err = RunDesign(context.Background(), reg, "design.bindcraft", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
```

In `internal/backends/backend_test.go`, `TestLocalBackendRunNoAdapterIsClear` currently reads:

```go
	_, err = b.Run(context.Background(), "design.rfdiffusion", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
```

Replace those lines with:

```go
	_, err = b.Run(context.Background(), "design.bindcraft", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
```

- [ ] **Step 6: Run the build and tests to verify they pass**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS — all packages, vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/backends/local/adapter.go internal/backends/local/adapter_rfdiffusion.go internal/backends/local/adapter_rfdiffusion_test.go internal/backends/local/adapter_test.go internal/backends/backend_test.go
git commit -m "$(printf 'feat: add the RFdiffusion local-backend adapter\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Final verification

- [ ] **Tier 0 — offline:** `go build ./... && go test ./... && go vet ./...` — all green.
- [ ] `design.rfdiffusion` dispatches to its adapter (`TestRunDesignRFdiffusionIsRegistered`); `design.bindcraft` still returns the clear `"no local adapter on this backend yet"` error.
- [ ] **Tier 3 (manual, deferred):** a real RFdiffusion GPU run is gated on the GB10 `sm_121` torch fix — not part of SP2 acceptance.
