# BindCraft Adapter — SP3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the agent's `design.bindcraft` tool to run BindCraft through the local backend, as the third and final `ToolAdapter`.

**Architecture:** A `bindCraftAdapter` in `internal/backends/local/` following the SP1/SP2 pattern — the agent supplies BindCraft's target-settings as a JSON object (pass-through), the adapter injects its own `design_path`, runs `bindcraft.py --settings`, and parses the accepted designs (PDBs + a stats CSV) into the `{"designs":[…]}` schema.

**Tech Stack:** Go, BindCraft (Python, uv-managed, AlphaFold/JAX).

**Spec:** `docs/superpowers/specs/2026-05-19-proteus-bindcraft-adapter-design.md`.

**Branch:** `feat/bindcraft-adapter` (already created).

---

## File map

| File | Change | Task |
|---|---|---|
| `internal/tools/design/design.go` | modify — add `settings` to the shared `InputSchema()` | 1 |
| `internal/tools/design/design_test.go` | modify — test the `settings` property | 1 |
| `internal/backends/local/adapter_bindcraft.go` | **new** — results parser (T2), then the adapter (T3) | 2, 3 |
| `internal/backends/local/adapter_bindcraft_test.go` | **new** — parser tests (T2), adapter tests (T3) | 2, 3 |
| `internal/backends/local/adapter_test.go` | modify — the "no adapter" test moves to a fabricated name | 3 |
| `internal/backends/backend_test.go` | modify — the "no adapter" test moves to a fabricated name | 3 |

**Parallelism:** **Task 1** (`internal/tools/design`) and **Task 2** (`internal/backends/local`) touch disjoint packages/files and may run in parallel. **Task 3** follows Task 2 (same file `adapter_bindcraft.go`).

**No foundation change** — `AdapterEnv.Registry` already exists (added in SP2).

---

## Task 1: Add `settings` to the design tool input schema

`design.bindcraft` takes a BindCraft target-settings object, but the shared `designTool.InputSchema()` does not advertise it. Add a `settings` object property (ProteinMPNN/RFdiffusion harmlessly ignore it).

**Files:**
- Modify: `internal/tools/design/design.go`
- Test: `internal/tools/design/design_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/tools/design/design_test.go`:

```go
func TestDesignToolSchemaAdvertisesSettings(t *testing.T) {
	tool := NewBindCraftTool(nil, nil, nil)
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("InputSchema has no properties map")
	}
	if _, ok := props["settings"]; !ok {
		t.Error("InputSchema must advertise the settings property")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tools/design/ -run TestDesignToolSchemaAdvertisesSettings`
Expected: FAIL — `props["settings"]` is absent.

- [ ] **Step 3: Add the `settings` property**

In `internal/tools/design/design.go`, the `InputSchema` method's `properties` map currently reads:

```go
		"properties": map[string]any{
			"target":      map[string]any{"type": "string", "description": "Target PDB ID or file path"},
			"hotspots":    map[string]any{"type": "string", "description": "Target hotspot residues"},
			"num_designs": map[string]any{"type": "integer", "description": "Number of designs to generate"},
			"contigs":     map[string]any{"type": "string", "description": "RFdiffusion contig map (design.rfdiffusion only)"},
		},
```

Add a `settings` entry:

```go
		"properties": map[string]any{
			"target":      map[string]any{"type": "string", "description": "Target PDB ID or file path"},
			"hotspots":    map[string]any{"type": "string", "description": "Target hotspot residues"},
			"num_designs": map[string]any{"type": "integer", "description": "Number of designs to generate"},
			"contigs":     map[string]any{"type": "string", "description": "RFdiffusion contig map (design.rfdiffusion only)"},
			"settings":    map[string]any{"type": "object", "description": "BindCraft target-settings JSON (design.bindcraft only)"},
		},
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tools/design/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tools/design/design.go internal/tools/design/design_test.go
git commit -m "$(printf 'feat: advertise the settings input for design.bindcraft\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 2: BindCraft results parser

BindCraft writes accepted designs to `<design_path>/Accepted/*.pdb` and a `final_design_stats.csv`. The parser collects the PDBs and enriches them from the CSV when present.

**Files:**
- Create: `internal/backends/local/adapter_bindcraft.go`
- Create: `internal/backends/local/adapter_bindcraft_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/backends/local/adapter_bindcraft_test.go`:

```go
package local

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBindCraftOutput(t *testing.T) {
	designPath := t.TempDir()
	accepted := filepath.Join(designPath, "Accepted")
	if err := os.MkdirAll(accepted, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"design_1.pdb", "design_2.pdb"} {
		if err := os.WriteFile(filepath.Join(accepted, n), []byte("ATOM\nEND\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	statsCSV := "Design,Sequence,Average_pLDDT,Average_i_pTM\n" +
		"design_1,MKLV,0.91,0.78\n" +
		"design_2,GSHM,0.88,0.72\n"
	if err := os.WriteFile(filepath.Join(designPath, "final_design_stats.csv"), []byte(statsCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	designs, err := parseBindCraftOutput(designPath)
	if err != nil {
		t.Fatalf("parseBindCraftOutput: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(designs))
	}
	if designs[0].StructureFile == "" {
		t.Error("structure_file must be set")
	}
	if designs[0].Sequence["A"] != "MKLV" {
		t.Errorf("design_1 sequence = %q, want MKLV", designs[0].Sequence["A"])
	}
	if designs[0].Scores["Average_pLDDT"] != 0.91 {
		t.Errorf("design_1 Average_pLDDT = %v, want 0.91", designs[0].Scores["Average_pLDDT"])
	}
	if designs[1].Scores["Average_i_pTM"] != 0.72 {
		t.Errorf("design_2 Average_i_pTM = %v, want 0.72", designs[1].Scores["Average_i_pTM"])
	}
}

func TestParseBindCraftOutputNoCSV(t *testing.T) {
	designPath := t.TempDir()
	accepted := filepath.Join(designPath, "Accepted")
	if err := os.MkdirAll(accepted, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(accepted, "d.pdb"), []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseBindCraftOutput(designPath)
	if err != nil {
		t.Fatalf("parseBindCraftOutput: %v", err)
	}
	if len(designs) != 1 || designs[0].StructureFile == "" {
		t.Fatalf("want 1 design with a structure file, got %+v", designs)
	}
}

func TestParseBindCraftOutputEmptyErrors(t *testing.T) {
	if _, err := parseBindCraftOutput(t.TempDir()); err == nil {
		t.Fatal("expected an error when no accepted designs are present")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/backends/local/ -run TestParseBindCraft`
Expected: FAIL — `undefined: parseBindCraftOutput`

- [ ] **Step 3: Create `internal/backends/local/adapter_bindcraft.go`**

```go
package local

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// bindCraftStats is one design's CSV-derived sequence and scores.
type bindCraftStats struct {
	sequence string
	scores   map[string]float64
}

// parseBindCraftStatsCSV reads final_design_stats.csv into a map keyed by
// design name (the PDB stem). A missing file yields an empty map and no error.
func parseBindCraftStatsCSV(path string) (map[string]bindCraftStats, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bindCraftStats{}, nil
		}
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	out := map[string]bindCraftStats{}
	if len(rows) < 2 {
		return out, nil
	}
	header := rows[0]
	for _, row := range rows[1:] {
		st := bindCraftStats{scores: map[string]float64{}}
		name := ""
		for i, col := range header {
			if i >= len(row) {
				break
			}
			val := strings.TrimSpace(row[i])
			switch strings.ToLower(strings.TrimSpace(col)) {
			case "design", "design_name":
				if name == "" {
					name = val
				}
			case "sequence":
				st.sequence = val
			default:
				if v, err := strconv.ParseFloat(val, 64); err == nil {
					st.scores[strings.TrimSpace(col)] = v
				}
			}
		}
		if name == "" && len(row) > 0 {
			name = strings.TrimSpace(row[0])
		}
		if name != "" {
			out[strings.TrimSuffix(name, ".pdb")] = st
		}
	}
	return out, nil
}

// parseBindCraftOutput collects accepted BindCraft designs from designPath: the
// PDBs in Accepted/, enriched with sequence and scores from
// final_design_stats.csv when that file is present.
func parseBindCraftOutput(designPath string) ([]designOut, error) {
	pdbs, err := filepath.Glob(filepath.Join(designPath, "Accepted", "*.pdb"))
	if err != nil {
		return nil, err
	}
	stats, err := parseBindCraftStatsCSV(filepath.Join(designPath, "final_design_stats.csv"))
	if err != nil {
		return nil, err
	}
	var designs []designOut
	for _, pdb := range pdbs {
		stem := strings.TrimSuffix(filepath.Base(pdb), ".pdb")
		d := designOut{
			Sequence:      map[string]string{},
			StructureFile: pdb,
			Scores:        map[string]float64{},
		}
		if st, ok := stats[stem]; ok {
			if st.sequence != "" {
				d.Sequence["A"] = st.sequence
			}
			d.Scores = st.scores
		}
		designs = append(designs, d)
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("design.bindcraft: no accepted designs found in %s", designPath)
	}
	return designs, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/backends/local/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_bindcraft.go internal/backends/local/adapter_bindcraft_test.go
git commit -m "$(printf 'feat: add the BindCraft results-directory parser\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 3: BindCraft adapter

**Files:**
- Modify: `internal/backends/local/adapter_bindcraft.go`
- Modify: `internal/backends/local/adapter_bindcraft_test.go`
- Modify: `internal/backends/local/adapter_test.go`
- Modify: `internal/backends/backend_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/backends/local/adapter_bindcraft_test.go` (add `"context"`, `"encoding/json"`, `"strings"` to its import block):

```go
// bindCraftStubRunner records commands and, on the bindcraft.py call, reads the
// settings file named after --settings, then writes a fixture results dir
// (Accepted/*.pdb + final_design_stats.csv) into that settings' design_path.
func bindCraftStubRunner(ran *[]string) CmdRunner {
	return func(ctx context.Context, dir, cmd string) (string, error) {
		*ran = append(*ran, cmd)
		_, after, ok := strings.Cut(cmd, "--settings ")
		if !ok {
			return "", nil
		}
		settingsFile, _, _ := strings.Cut(after, " ")
		body, err := os.ReadFile(settingsFile)
		if err != nil {
			return "", err
		}
		var s map[string]any
		if err := json.Unmarshal(body, &s); err != nil {
			return "", err
		}
		designPath, _ := s["design_path"].(string)
		accepted := filepath.Join(designPath, "Accepted")
		if err := os.MkdirAll(accepted, 0o755); err != nil {
			return "", err
		}
		for _, n := range []string{"design_1.pdb", "design_2.pdb"} {
			if err := os.WriteFile(filepath.Join(accepted, n), []byte("ATOM\nEND\n"), 0o644); err != nil {
				return "", err
			}
		}
		statsCSV := "Design,Sequence,Average_pLDDT\ndesign_1,MKLV,0.91\ndesign_2,GSHM,0.88\n"
		if err := os.WriteFile(filepath.Join(designPath, "final_design_stats.csv"), []byte(statsCSV), 0o644); err != nil {
			return "", err
		}
		return "ok", nil
	}
}

// bindCraftTestEnv builds an AdapterEnv with an installed-looking recipe and a
// registry whose alphafold_params directory exists on disk.
func bindCraftTestEnv(t *testing.T, ran *[]string) AdapterEnv {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	asset, ok := reg.DataAsset("alphafold_params")
	if !ok {
		t.Fatal("alphafold_params data asset is not registered")
	}
	if err := os.MkdirAll(asset.ExtractTo, 0o755); err != nil {
		t.Fatal(err)
	}
	return AdapterEnv{
		Recipe:   ToolRecipe{Name: "bindcraft", InstallDir: t.TempDir(), VenvDir: t.TempDir()},
		Run:      bindCraftStubRunner(ran),
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
}

func TestBindCraftAdapterInvoke(t *testing.T) {
	var ran []string
	env := bindCraftTestEnv(t, &ran)
	starting := filepath.Join(t.TempDir(), "target.pdb")
	if err := os.WriteFile(starting, []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := bindCraftAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"settings":{"starting_pdb":"`+starting+`","chains":"A","number_of_final_designs":2}}`))
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
	if !strings.HasPrefix(env2.Designs[0].StructureFile, env.Registry.Home()) {
		t.Errorf("structure_file %q must be under PROTEUS_HOME %q (outlives the temp WorkDir)",
			env2.Designs[0].StructureFile, env.Registry.Home())
	}
	if env2.Designs[0].Sequence["A"] != "MKLV" {
		t.Errorf("design sequence should come from the stats CSV, got %q", env2.Designs[0].Sequence["A"])
	}
	if len(ran) != 1 || !strings.Contains(ran[0], "bindcraft.py --settings ") {
		t.Fatalf("want one bindcraft.py --settings command, got: %v", ran)
	}
}

func TestBindCraftAdapterInvokeMissingSettings(t *testing.T) {
	var ran []string
	env := bindCraftTestEnv(t, &ran)
	if _, err := (bindCraftAdapter{}).Invoke(context.Background(), env, []byte(`{}`)); err == nil {
		t.Fatal("expected an error when settings is missing")
	}
}

func TestBindCraftAdapterInvokeBadStartingPDB(t *testing.T) {
	var ran []string
	env := bindCraftTestEnv(t, &ran)
	_, err := bindCraftAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"settings":{"starting_pdb":"/no/such/file.pdb"}}`))
	if err == nil {
		t.Fatal("expected an error when starting_pdb does not exist")
	}
	if len(ran) != 0 {
		t.Errorf("a bad starting_pdb must not run any command, got %d", len(ran))
	}
}

func TestBindCraftAdapterInvokeParamsMissing(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// alphafold_params directory is deliberately NOT created.
	env := AdapterEnv{
		Recipe:   ToolRecipe{Name: "bindcraft", InstallDir: t.TempDir(), VenvDir: t.TempDir()},
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = bindCraftAdapter{}.Invoke(context.Background(), env, []byte(`{"settings":{"chains":"A"}}`))
	if err == nil {
		t.Fatal("expected an error when the alphafold_params directory is absent")
	}
}

func TestBindCraftAdapterInvokeNotInstalled(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:   ToolRecipe{InstallDir: filepath.Join(t.TempDir(), "gone"), VenvDir: t.TempDir()},
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = bindCraftAdapter{}.Invoke(context.Background(), env, []byte(`{"settings":{"chains":"A"}}`))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want a 'not installed' error, got: %v", err)
	}
}

func TestRunDesignBindCraftIsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Missing settings makes Invoke fail fast — still proves design.bindcraft
	// is registered and dispatched.
	_, err = RunDesign(context.Background(), reg, "design.bindcraft", []byte(`{}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("design.bindcraft must be registered, got: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/backends/local/ -run 'TestBindCraftAdapter|TestRunDesignBindCraft'`
Expected: FAIL — `undefined: bindCraftAdapter`

- [ ] **Step 3: Append the adapter to `internal/backends/local/adapter_bindcraft.go`**

Add `"context"`, `"encoding/json"`, `"time"` to the file's import block, then append:

```go
// init registers the BindCraft adapter with the local backend.
func init() { registerAdapter(bindCraftAdapter{}) }

// bindCraftAdapter wires design.bindcraft to the installed BindCraft tool.
type bindCraftAdapter struct{}

func (bindCraftAdapter) AgentTool() string { return "design.bindcraft" }
func (bindCraftAdapter) Recipe() string    { return "bindcraft" }

// bindCraftRequest is the subset of the design.bindcraft input the adapter
// uses: BindCraft's target-settings object, passed through verbatim.
type bindCraftRequest struct {
	Settings json.RawMessage `json:"settings"`
}

// Invoke writes the agent-supplied BindCraft settings (with design_path
// overridden), runs bindcraft.py, and parses the accepted designs.
func (bindCraftAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req bindCraftRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.bindcraft: invalid request: %w", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(req.Settings, &settings); err != nil || len(settings) == 0 {
		return nil, fmt.Errorf("design.bindcraft: settings is required (a BindCraft target-settings JSON object)")
	}
	if sp, ok := settings["starting_pdb"].(string); ok && strings.TrimSpace(sp) != "" {
		if info, err := os.Stat(sp); err != nil || info.IsDir() {
			return nil, fmt.Errorf("design.bindcraft: starting_pdb %q does not exist", sp)
		}
	}
	if info, err := os.Stat(env.Recipe.InstallDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.bindcraft: bindcraft is not installed (run /install bindcraft)")
	}
	if info, err := os.Stat(env.Recipe.VenvDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.bindcraft: bindcraft is not installed (run /install bindcraft)")
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("design.bindcraft: adapter registry unavailable")
	}
	asset, ok := env.Registry.DataAsset("alphafold_params")
	if !ok {
		return nil, fmt.Errorf("design.bindcraft: the alphafold_params data asset is not registered")
	}
	if info, err := os.Stat(asset.ExtractTo); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.bindcraft: AlphaFold params missing — install the alphafold_params data asset")
	}

	designPath := filepath.Join(env.Registry.Home(), "designs",
		fmt.Sprintf("bindcraft-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(designPath, 0o755); err != nil {
		return nil, err
	}
	settings["design_path"] = designPath

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	settingsFile := filepath.Join(env.WorkDir, "settings.json")
	if err := os.WriteFile(settingsFile, settingsJSON, 0o644); err != nil {
		return nil, err
	}

	cmd := fmt.Sprintf("%s %s --settings %s",
		filepath.Join(env.Recipe.VenvDir, "bin", "python"),
		filepath.Join(env.Recipe.InstallDir, "bindcraft.py"),
		settingsFile)
	if out, err := env.Run(ctx, env.Recipe.InstallDir, cmd); err != nil {
		return nil, fmt.Errorf("design.bindcraft: run failed: %w\n%s", err, out)
	}

	designs, err := parseBindCraftOutput(designPath)
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/backends/local/`
Expected: PASS

- [ ] **Step 5: Move the "no adapter" tests to a fabricated tool name**

After SP3, all three real `design.*` tools have adapters, so the two tests that used `design.bindcraft` as the "no adapter" example must switch to a name that is not — and will never be — a registered adapter.

In `internal/backends/local/adapter_test.go`, `TestRunDesignNoAdapterMessageIsClear` currently contains:

```go
	// design.bindcraft has no adapter yet — the message must say so plainly.
	_, err = RunDesign(context.Background(), reg, "design.bindcraft", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
```

Replace those lines with:

```go
	// Every real design.* tool has an adapter after SP3 — use a fabricated name.
	_, err = RunDesign(context.Background(), reg, "design.nonesuch", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
```

In `internal/backends/backend_test.go`, `TestLocalBackendRunNoAdapterIsClear` currently contains:

```go
	_, err = b.Run(context.Background(), "design.bindcraft", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
```

Replace those lines with:

```go
	_, err = b.Run(context.Background(), "design.nonesuch", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
```

- [ ] **Step 6: Run the build and tests to verify they pass**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS — all packages, vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/backends/local/adapter_bindcraft.go internal/backends/local/adapter_bindcraft_test.go internal/backends/local/adapter_test.go internal/backends/backend_test.go
git commit -m "$(printf 'feat: add the BindCraft local-backend adapter\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Final verification

- [ ] **Tier 0 — offline:** `go build ./... && go test ./... && go vet ./...` — all green.
- [ ] All three `design.*` tools (`proteinmpnn`, `rfdiffusion`, `bindcraft`) dispatch to their adapters; an unregistered name still returns the clear `"no local adapter on this backend yet"` error.
- [ ] **Tier 3 (manual, deferred):** a real BindCraft GPU run is gated on the GB10 `sm_121` torch fix — not part of SP3 acceptance.
