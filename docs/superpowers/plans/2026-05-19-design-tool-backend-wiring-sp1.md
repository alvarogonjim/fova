# Design-Tool Backend Wiring — SP1 (Foundation + ProteinMPNN) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `design.proteinmpnn` actually run ProteinMPNN through the local backend and return real designs, via a per-tool `ToolAdapter`.

**Architecture:** A `ToolAdapter` interface in `internal/backends/local/` — each adapter turns an agent design request into a real tool invocation and the tool's native output into the `{"designs":[…]}` schema. `localBackend.Run` dispatches by tool name through `local.RunDesign`. ProteinMPNN is the first (and, in SP1, only) adapter.

**Tech Stack:** Go, `modernc.org/sqlite` (unaffected), ProteinMPNN (Python, uv-managed).

**Spec:** `docs/superpowers/specs/2026-05-19-proteus-design-tool-backend-wiring-design.md` (SP1).

**Branch:** `feat/design-tool-backend-wiring` (already created).

---

## File map

| File | Change | Task |
|---|---|---|
| `internal/backends/local/adapter.go` | **new** — `ToolAdapter`, `AdapterEnv`, registry, `RunDesign` | 1 |
| `internal/backends/local/adapter_test.go` | **new** — registry / `RunDesign` tests | 1 |
| `internal/backends/local/adapter_proteinmpnn.go` | **new** — FASTA parser (T2), then the adapter (T3) | 2, 3 |
| `internal/backends/local/adapter_proteinmpnn_test.go` | **new** — parser tests (T2), adapter tests (T3) | 2, 3 |
| `internal/backends/local/testdata/proteinmpnn_sample.fa` | **new** — fixture FASTA output | 2 |
| `internal/backends/backend.go` | modify — `localBackend` dispatches to `RunDesign` | 4 |
| `internal/backends/backend_test.go` | modify — add a dispatch test | 4 |

**Parallelism:** Task 1 is the foundation — do it first. After it, **Task 4** (`internal/backends` package) and **Task 2** (`internal/backends/local` package) touch disjoint files and may run in parallel; **Task 3** follows Task 2 (same file). `internal/backends/local/runner.go` is left as-is — it is no longer used by `localBackend`, but removing it is out of scope.

---

## Task 1: Adapter foundation

**Files:**
- Create: `internal/backends/local/adapter.go`
- Create: `internal/backends/local/adapter_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/backends/local/adapter_test.go`:

```go
package local

import (
	"context"
	"strings"
	"testing"
)

func TestRunDesignUnknownToolErrors(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunDesign(context.Background(), reg, "design.nonesuch", []byte(`{}`)); err == nil {
		t.Fatal("expected an error for a tool with no adapter")
	}
}

func TestRunDesignNoAdapterMessageIsClear(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// design.rfdiffusion has no adapter in SP1 — the message must say so plainly.
	_, err = RunDesign(context.Background(), reg, "design.rfdiffusion", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/backends/local/ -run TestRunDesign`
Expected: FAIL — `undefined: RunDesign`

- [ ] **Step 3: Create `internal/backends/local/adapter.go`**

```go
package local

import (
	"context"
	"fmt"
	"os"
)

// ToolAdapter runs one design tool on the local backend: it turns an agent
// design request into a real tool invocation and the tool's native output into
// the {"designs":[...]} JSON the design tools expect back.
type ToolAdapter interface {
	AgentTool() string // e.g. "design.proteinmpnn"
	Recipe() string    // e.g. "proteinmpnn" — the tools.toml recipe name
	Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error)
}

// AdapterEnv is everything an adapter needs to run. It is injected so adapters
// are unit-testable with a stub Run and a temporary WorkDir.
type AdapterEnv struct {
	Recipe  ToolRecipe // resolved recipe — InstallDir and VenvDir are expanded
	Run     CmdRunner  // command runner (production: bashRunner; tests: a stub)
	WorkDir string     // a fresh temp directory the adapter may write into
}

// designOut is one design in the {"designs":[...]} envelope adapters return;
// it mirrors the schema internal/tools/design expects back from a backend.
type designOut struct {
	Sequence      map[string]string  `json:"sequence"`
	StructureFile string             `json:"structure_file"`
	Scores        map[string]float64 `json:"scores"`
}

// designsEnvelope is the top-level {"designs":[...]} JSON adapters return.
type designsEnvelope struct {
	Designs []designOut `json:"designs"`
}

// adapterRegistry maps agent tool name -> adapter. Adapters register themselves
// via registerAdapter from an init function in their own file.
var adapterRegistry = map[string]ToolAdapter{}

// registerAdapter adds an adapter to the registry.
func registerAdapter(a ToolAdapter) { adapterRegistry[a.AgentTool()] = a }

// RunDesign runs the local adapter for a design tool. It looks up the adapter,
// resolves its recipe, creates a temp WorkDir (removed on return), and invokes
// it. A design tool with no registered adapter yields a clear error.
func RunDesign(ctx context.Context, reg *Registry, agentTool string, request []byte) ([]byte, error) {
	adapter, ok := adapterRegistry[agentTool]
	if !ok {
		return nil, fmt.Errorf("%s: no local adapter on this backend yet", agentTool)
	}
	rec, ok := reg.Tool(adapter.Recipe())
	if !ok {
		return nil, fmt.Errorf("%s: recipe %q is not in the tool registry", agentTool, adapter.Recipe())
	}
	workDir, err := os.MkdirTemp("", "proteus-design-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)
	return adapter.Invoke(ctx, AdapterEnv{Recipe: rec, Run: bashRunner, WorkDir: workDir}, request)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/backends/local/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter.go internal/backends/local/adapter_test.go
git commit -m "$(printf 'feat: add the ToolAdapter foundation for design tools\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 2: ProteinMPNN FASTA parser

ProteinMPNN writes `seqs/<name>.fa` files. Record 0 is the native input sequence; records 1+ are designed sequences whose headers carry `score`, `global_score`, `seq_recovery`.

**Files:**
- Create: `internal/backends/local/testdata/proteinmpnn_sample.fa`
- Create: `internal/backends/local/adapter_proteinmpnn.go`
- Create: `internal/backends/local/adapter_proteinmpnn_test.go`

- [ ] **Step 1: Create the fixture**

Create `internal/backends/local/testdata/proteinmpnn_sample.fa` with exactly this content (a real ProteinMPNN monomer output — native record + two designed records):

```
>5L33, score=1.5883, global_score=1.5883, fixed_chains=[], designed_chains=['A'], model_name=v_48_020, git_hash=8907e6671bfbfc92303b5f79c4b5e6ce47cdef57, seed=37
HMPEEEKAARLFIEALEKGDPELMRKVISPDTRMEDNGREFTGDEVVEYVKEIQKRGEQWHLRRYTKEGNSWRFEVQVDNNGQTEQWEVQIEVRNGRIKRVTITHV
>T=0.1, sample=1, score=0.8227, global_score=0.8227, seq_recovery=0.5094
MINEEEKKALDFIEALEKADPELMKKVIEPDTKMEVNGKKYEGEEIVEFVKKLKEEGVKYKLLSYKKEGNKYVFEVEKSKNGVTKKITIEIEVENGKVKKIVITEK
>T=0.1, sample=2, score=0.8361, global_score=0.8361, seq_recovery=0.4434
SINEEEQKALDYIKALEKADPELMKKVITPDTKMTVNGKEYEGEEIVEYVKELKERGIKYKLLSYKKEGDKYVFTVERSENGKTYTITIEVKVKDGKVEEIVIKEE
```

- [ ] **Step 2: Write the failing tests**

Create `internal/backends/local/adapter_proteinmpnn_test.go`:

```go
package local

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProteinMPNNOutput(t *testing.T) {
	seqsDir := filepath.Join(t.TempDir(), "seqs")
	if err := os.MkdirAll(seqsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/proteinmpnn_sample.fa")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seqsDir, "5L33.fa"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	designs, err := parseProteinMPNNOutput(seqsDir)
	if err != nil {
		t.Fatalf("parseProteinMPNNOutput: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs (record 0 is native), got %d", len(designs))
	}
	if got := designs[0].Sequence["A"]; got == "" {
		t.Error("design 0 chain A sequence is empty")
	}
	if got := designs[0].Scores["score"]; got != 0.8227 {
		t.Errorf("design 0 score = %v, want 0.8227", got)
	}
	if got := designs[0].Scores["seq_recovery"]; got != 0.5094 {
		t.Errorf("design 0 seq_recovery = %v, want 0.5094", got)
	}
	if got := designs[1].Scores["global_score"]; got != 0.8361 {
		t.Errorf("design 1 global_score = %v, want 0.8361", got)
	}
}

func TestParseProteinMPNNOutputEmptyDirErrors(t *testing.T) {
	if _, err := parseProteinMPNNOutput(t.TempDir()); err == nil {
		t.Fatal("expected an error when no designs are present")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/backends/local/ -run TestParseProteinMPNN`
Expected: FAIL — `undefined: parseProteinMPNNOutput`

- [ ] **Step 4: Create `internal/backends/local/adapter_proteinmpnn.go`**

```go
package local

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// fastaRecord is one header/sequence pair from a FASTA file.
type fastaRecord struct {
	header string // the text after '>'
	seq    string
}

// parseFASTARecords splits FASTA text into header/sequence records, in order.
func parseFASTARecords(text string) []fastaRecord {
	var recs []fastaRecord
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			recs = append(recs, fastaRecord{header: strings.TrimPrefix(line, ">")})
			continue
		}
		if len(recs) > 0 {
			recs[len(recs)-1].seq += line
		}
	}
	return recs
}

// proteinMPNNScores pulls score / global_score / seq_recovery out of a header
// like "T=0.1, sample=1, score=0.82, global_score=0.82, seq_recovery=0.50".
func proteinMPNNScores(header string) map[string]float64 {
	scores := map[string]float64{}
	for _, field := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(field), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "score", "global_score", "seq_recovery":
			if v, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64); err == nil {
				scores[kv[0]] = v
			}
		}
	}
	return scores
}

// splitChains maps a ProteinMPNN sequence (chains joined by '/') to chain IDs
// A, B, C, ...
func splitChains(seq string) map[string]string {
	out := map[string]string{}
	for i, chain := range strings.Split(seq, "/") {
		out[string(rune('A'+i))] = chain
	}
	return out
}

// parseProteinMPNNOutput reads every *.fa in seqsDir and returns one design per
// designed sequence (record 0 in each file is the native input — skipped).
func parseProteinMPNNOutput(seqsDir string) ([]designOut, error) {
	files, err := filepath.Glob(filepath.Join(seqsDir, "*.fa"))
	if err != nil {
		return nil, err
	}
	var designs []designOut
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		recs := parseFASTARecords(string(body))
		for i, rec := range recs {
			if i == 0 {
				continue // native input sequence
			}
			designs = append(designs, designOut{
				Sequence:      splitChains(rec.seq),
				StructureFile: "",
				Scores:        proteinMPNNScores(rec.header),
			})
		}
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("design.proteinmpnn: no designed sequences found in %s", seqsDir)
	}
	return designs, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/backends/local/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/backends/local/adapter_proteinmpnn.go internal/backends/local/adapter_proteinmpnn_test.go internal/backends/local/testdata/proteinmpnn_sample.fa
git commit -m "$(printf 'feat: add the ProteinMPNN FASTA output parser\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 3: ProteinMPNN adapter

**Files:**
- Modify: `internal/backends/local/adapter_proteinmpnn.go`
- Modify: `internal/backends/local/adapter_proteinmpnn_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/backends/local/adapter_proteinmpnn_test.go` (add `"context"`, `"encoding/json"`, `"strings"` to its import block):

```go
func TestProteinMPNNAdapterInvoke(t *testing.T) {
	workDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "backbone.pdb")
	if err := os.WriteFile(target, []byte("ATOM      1  N   MET A   1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/proteinmpnn_sample.fa")
	if err != nil {
		t.Fatal(err)
	}

	var ran []string
	stub := func(ctx context.Context, dir, cmd string) (string, error) {
		ran = append(ran, cmd)
		if strings.Contains(cmd, "protein_mpnn_run.py") {
			seqs := filepath.Join(workDir, "seqs")
			if err := os.MkdirAll(seqs, 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(seqs, "backbone.fa"), fixture, 0o644); err != nil {
				return "", err
			}
		}
		return "ok", nil
	}
	env := AdapterEnv{
		Recipe:  ToolRecipe{Name: "proteinmpnn", InstallDir: t.TempDir(), VenvDir: t.TempDir()},
		Run:     stub,
		WorkDir: workDir,
	}

	out, err := proteinMPNNAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"target":"`+target+`","num_designs":2}`))
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
	if len(ran) != 2 {
		t.Fatalf("want 2 commands (parse + inference), got %d: %v", len(ran), ran)
	}
	if !strings.Contains(ran[0], "parse_multiple_chains.py") {
		t.Errorf("command 1 should be the parse step: %s", ran[0])
	}
	if !strings.Contains(ran[1], "--num_seq_per_target 2") {
		t.Errorf("command 2 should request 2 sequences: %s", ran[1])
	}
}

func TestProteinMPNNAdapterInvokeMissingTarget(t *testing.T) {
	env := AdapterEnv{Recipe: ToolRecipe{VenvDir: t.TempDir()}, WorkDir: t.TempDir()}
	if _, err := (proteinMPNNAdapter{}).Invoke(context.Background(), env, []byte(`{"num_designs":1}`)); err == nil {
		t.Fatal("expected an error when target is missing")
	}
}

func TestProteinMPNNAdapterInvokeNotInstalled(t *testing.T) {
	target := filepath.Join(t.TempDir(), "b.pdb")
	if err := os.WriteFile(target, []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:  ToolRecipe{VenvDir: filepath.Join(t.TempDir(), "does-not-exist")},
		WorkDir: t.TempDir(),
	}
	if _, err := (proteinMPNNAdapter{}).Invoke(context.Background(), env, []byte(`{"target":"`+target+`"}`)); err == nil {
		t.Fatal("expected a 'not installed' error")
	}
}

func TestRunDesignProteinMPNNIsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A nonexistent target makes Invoke fail fast before any command runs —
	// which still proves design.proteinmpnn is registered and dispatched.
	_, err = RunDesign(context.Background(), reg, "design.proteinmpnn", []byte(`{"target":"/no/such/file.pdb"}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("design.proteinmpnn must be registered, got: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/backends/local/ -run 'TestProteinMPNNAdapter|TestRunDesignProteinMPNN'`
Expected: FAIL — `undefined: proteinMPNNAdapter`

- [ ] **Step 3: Append the adapter to `internal/backends/local/adapter_proteinmpnn.go`**

Add `"context"` and `"encoding/json"` to the file's import block, then append:

```go
// init registers the ProteinMPNN adapter with the local backend.
func init() { registerAdapter(proteinMPNNAdapter{}) }

// proteinMPNNAdapter wires design.proteinmpnn to the installed ProteinMPNN tool.
type proteinMPNNAdapter struct{}

func (proteinMPNNAdapter) AgentTool() string { return "design.proteinmpnn" }
func (proteinMPNNAdapter) Recipe() string    { return "proteinmpnn" }

// proteinMPNNRequest is the subset of the design.proteinmpnn input the adapter
// uses (hotspots is accepted by the schema but unused in SP1).
type proteinMPNNRequest struct {
	Target     string `json:"target"`
	NumDesigns int    `json:"num_designs"`
}

// copyFile copies a small file (e.g. a PDB) from src to dst.
func copyFile(src, dst string) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, body, 0o644)
}

// Invoke runs ProteinMPNN for one backbone: parse the PDB into a jsonl, run
// inference, then parse the FASTA output into the {"designs":[...]} schema.
func (proteinMPNNAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req proteinMPNNRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.proteinmpnn: invalid request: %w", err)
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return nil, fmt.Errorf("design.proteinmpnn: target is required (path to a .pdb backbone)")
	}
	if !strings.HasSuffix(target, ".pdb") {
		return nil, fmt.Errorf("design.proteinmpnn: target %q must be a .pdb file", target)
	}
	if info, err := os.Stat(target); err != nil || info.IsDir() {
		return nil, fmt.Errorf("design.proteinmpnn: target %q does not exist", target)
	}
	if info, err := os.Stat(env.Recipe.VenvDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.proteinmpnn: proteinmpnn is not installed (run /install proteinmpnn)")
	}
	numDesigns := req.NumDesigns
	if numDesigns < 1 {
		numDesigns = 1
	}

	inputsDir := filepath.Join(env.WorkDir, "inputs")
	if err := os.MkdirAll(inputsDir, 0o755); err != nil {
		return nil, err
	}
	if err := copyFile(target, filepath.Join(inputsDir, filepath.Base(target))); err != nil {
		return nil, fmt.Errorf("design.proteinmpnn: stage target: %w", err)
	}

	python := filepath.Join(env.Recipe.VenvDir, "bin", "python")
	parsedJSONL := filepath.Join(env.WorkDir, "parsed.jsonl")
	parseCmd := fmt.Sprintf("%s %s --input_path=%s --output_path=%s",
		python,
		filepath.Join(env.Recipe.InstallDir, "helper_scripts", "parse_multiple_chains.py"),
		inputsDir, parsedJSONL)
	if out, err := env.Run(ctx, env.Recipe.InstallDir, parseCmd); err != nil {
		return nil, fmt.Errorf("design.proteinmpnn: parse step failed: %w\n%s", err, out)
	}

	runCmd := fmt.Sprintf(
		"%s %s --jsonl_path %s --out_folder %s --num_seq_per_target %d --sampling_temp 0.1 --seed 37 --batch_size 1",
		python,
		filepath.Join(env.Recipe.InstallDir, "protein_mpnn_run.py"),
		parsedJSONL, env.WorkDir, numDesigns)
	if out, err := env.Run(ctx, env.Recipe.InstallDir, runCmd); err != nil {
		return nil, fmt.Errorf("design.proteinmpnn: inference step failed: %w\n%s", err, out)
	}

	designs, err := parseProteinMPNNOutput(filepath.Join(env.WorkDir, "seqs"))
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/backends/local/`
Expected: PASS (all adapter and parser tests)

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/adapter_proteinmpnn.go internal/backends/local/adapter_proteinmpnn_test.go
git commit -m "$(printf 'feat: add the ProteinMPNN local-backend adapter\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 4: Backend dispatch

`localBackend.Run` currently writes the raw request JSON and runs the recipe's `run_command` — which never matched a real tool. Replace it with adapter dispatch via `local.RunDesign`.

**Files:**
- Modify: `internal/backends/backend.go`
- Modify: `internal/backends/backend_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/backends/backend_test.go` (add `"context"` and `"strings"` to its import block):

```go
func TestLocalBackendRunNoAdapterIsClear(t *testing.T) {
	b, err := Select("local", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Run(context.Background(), "design.rfdiffusion", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/backends/ -run TestLocalBackendRunNoAdapter`
Expected: FAIL — the current `localBackend.Run` writes a temp file and runs the recipe, so the error text is `unknown tool "design.rfdiffusion"`, not `no local adapter`.

- [ ] **Step 3: Rewrite `localBackend` in `internal/backends/backend.go`**

Replace the `localBackend` type, its `Run` method, and the `local` case of `Select`. The current code is:

```go
// localBackend runs tools via the SP3 uv-managed local runner.
type localBackend struct{ runner *local.Runner }

func (b *localBackend) Name() string { return "local" }

func (b *localBackend) Run(ctx context.Context, tool string, input []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "proteus-run-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	reqPath := filepath.Join(dir, "request.json")
	if err := os.WriteFile(reqPath, input, 0o644); err != nil {
		return nil, err
	}
	// The recipes use different placeholder names for the request file; fill
	// every common one to the same path (unused names are simply ignored).
	out, err := b.runner.Run(ctx, tool, map[string]string{
		"input_json":  reqPath,
		"args_file":   reqPath,
		"input_yaml":  reqPath,
		"input_fasta": reqPath,
		"out_dir":     dir,
	})
	return []byte(out), err
}
```

Replace it with:

```go
// localBackend runs design tools via per-tool adapters (local.RunDesign).
type localBackend struct{ registry *local.Registry }

func (b *localBackend) Name() string { return "local" }

func (b *localBackend) Run(ctx context.Context, tool string, input []byte) ([]byte, error) {
	return local.RunDesign(ctx, b.registry, tool, input)
}
```

In `Select`, the `"", "local"` case currently ends with:

```go
		return &localBackend{runner: local.NewRunner(reg)}, nil
```

Change it to:

```go
		return &localBackend{registry: reg}, nil
```

Then fix the import block: `os` and `path/filepath` are no longer used anywhere in `backend.go` — remove both from the imports. (`context`, `fmt`, `local`, `modal` are still used.)

- [ ] **Step 4: Run the build and tests to verify they pass**

Run: `go build ./... && go test ./internal/backends/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/backends/backend.go internal/backends/backend_test.go
git commit -m "$(printf 'feat: dispatch local design jobs through tool adapters\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Final verification

- [ ] **Tier 0 — offline:** `go build ./... && go test ./... && go vet ./...` — all green.
- [ ] **Tier 1 — real tool, CPU (manual):** with `proteinmpnn` installed, confirm the adapter drives the real binary. From a Go scratch test or the TUI, invoke `design.proteinmpnn` with `{"target":"<a real .pdb>","num_designs":2}` and `CUDA_VISIBLE_DEVICES=` exported; expect a job that succeeds and two `domain.Design` rows with `score` / `global_score` / `seq_recovery` populated. The bundled `~/proteus/tools/proteinmpnn/inputs/PDB_monomers/pdbs/5L33.pdb` is a valid target.
- [ ] `design.rfdiffusion` / `design.bindcraft` now fail with a clear `"no local adapter on this backend yet"` message (verifies acceptance criterion 2).
