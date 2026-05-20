# v0.3/v0.4 Finalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire `pkg/proteinio` into production code, write an honest per-criterion verification record, and create annotated `v0.3.0`/`v0.4.0` release tags.

**Architecture:** Three independent workstreams. Tasks 1, 2, and 3 touch disjoint file sets and can run in parallel. Task 4 (tags) depends on Task 3's output and is interactive — run it in the main session, not a parallel agent.

**Tech Stack:** Go 1.x, standard library only (`pkg/proteinio` has no external deps), `git`.

**Spec:** `docs/superpowers/specs/2026-05-19-v03-v04-finalization-design.md`

---

## Parallelization

| Task | Files touched | Depends on |
|------|---------------|------------|
| Task 1 — ProteinMPNN adapter swap | `internal/backends/local/adapter_proteinmpnn.go` | none |
| Task 2 — `fs.read_structure` tool | `internal/tools/fs_structure.go` (new), `internal/tools/fs_structure_test.go` (new), `internal/tools/fs.go`, `cmd/proteus/main.go` | none |
| Task 3 — Verification record | `docs/VERIFICATION.md` (new) | none |
| Task 4 — Release tags | git tags only (no files) | Task 3 |

Tasks 1–3 have no shared files and no ordering constraint — dispatch them to three parallel worktree-isolated agents. Task 4 runs after Task 3 merges, in the main session, because it confirms commit SHAs and a tag push with the user.

---

## Task 1: ProteinMPNN adapter — use `proteinio.ParseFASTA`

This is a behavior-preserving refactor: replace the adapter's local ad-hoc FASTA
parser with `proteinio.ParseFASTA`. The existing adapter test suite is the
safety net — no new test is needed because no behavior changes.

**Files:**
- Modify: `internal/backends/local/adapter_proteinmpnn.go`
- Test (existing, unchanged): `internal/backends/local/adapter_proteinmpnn_test.go`

- [ ] **Step 1: Run the existing tests to establish a green baseline**

Run: `go test ./internal/backends/local/`
Expected: PASS. (`TestParseProteinMPNNOutput`, `TestParseProteinMPNNOutputSkipsEmptySequence`, `TestProteinMPNNAdapterInvoke`, and the rest all pass.)

- [ ] **Step 2: Delete the ad-hoc `fastaRecord` type and `parseFASTARecords` function**

In `internal/backends/local/adapter_proteinmpnn.go`, delete these lines verbatim (the type and the function, lines 13–36):

```go
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
```

- [ ] **Step 3: Rewrite `parseProteinMPNNOutput` to call `proteinio.ParseFASTA`**

Replace the body of `parseProteinMPNNOutput` (currently lines ~69–99) with this version. The two changes: `recs` now comes from `proteinio.ParseFASTA(bytes.NewReader(body))` with error handling, and the record fields are `rec.Header` / `rec.Sequence` instead of `rec.header` / `rec.seq`.

```go
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
		recs, err := proteinio.ParseFASTA(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("design.proteinmpnn: parse %s: %w", f, err)
		}
		for i, rec := range recs {
			if i == 0 {
				continue // native input sequence
			}
			if strings.TrimSpace(rec.Sequence) == "" {
				continue // malformed record — header with no sequence
			}
			designs = append(designs, designOut{
				Sequence:      splitChains(rec.Sequence),
				StructureFile: "",
				Scores:        proteinMPNNScores(rec.Header),
			})
		}
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("design.proteinmpnn: no designed sequences found in %s", seqsDir)
	}
	return designs, nil
}
```

- [ ] **Step 4: Fix the import block**

In the `import (...)` block at the top of `adapter_proteinmpnn.go`, add `"bytes"` and the `proteinio` package. The block should read:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alvarogonjim/proteus/pkg/proteinio"
)
```

(`strconv` and `strings` are still used — by `proteinMPNNScores` and the
`TrimSpace`/`HasPrefix`/`Split` calls elsewhere in the file.)

- [ ] **Step 5: Run gofmt, vet, and the tests**

Run: `gofmt -l internal/backends/local/adapter_proteinmpnn.go`
Expected: no output (file is formatted).

Run: `go vet ./internal/backends/local/ && go test ./internal/backends/local/`
Expected: PASS — same tests green as in Step 1. Of note, `TestParseProteinMPNNOutputSkipsEmptySequence` still passes: `proteinio.ParseFASTA` preserves the header-with-no-sequence record (empty `Sequence`), and the `strings.TrimSpace(rec.Sequence) == ""` guard skips it exactly as before.

- [ ] **Step 6: Commit**

```bash
git add internal/backends/local/adapter_proteinmpnn.go
git commit -m "$(cat <<'EOF'
refactor: parse ProteinMPNN output via pkg/proteinio

Replace the adapter's ad-hoc parseFASTARecords with proteinio.ParseFASTA,
giving the package its first production caller. Behavior-preserving.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: New tool `fs.read_structure`

Add an agent tool that reads a local `.pdb`/`.cif`/`.mmcif` file and returns its
chain sequences, giving `proteinio.ChainsFromPDB` and `proteinio.ChainsFromMMCIF`
production callers.

**Files:**
- Create: `internal/tools/fs_structure.go`
- Create: `internal/tools/fs_structure_test.go`
- Modify: `internal/tools/fs.go` (add the tool to `NewFSTools`)
- Modify: `cmd/proteus/main.go` (no edit needed if registered via `NewFSTools` — see Step 6)

- [ ] **Step 1: Write the failing test**

Create `internal/tools/fs_structure_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFSReadStructurePDB(t *testing.T) {
	root := t.TempDir()
	pdb := "ATOM      1  N   MET A   1      11.104  13.207  10.000  1.00  0.00           N\n" +
		"ATOM      2  CA  MET A   1      12.000  13.000  10.000  1.00  0.00           C\n" +
		"ATOM      4  CA  LYS A   2      13.000  14.000  10.000  1.00  0.00           C\n" +
		"ATOM      5  CA  THR A   3      14.000  15.000  10.000  1.00  0.00           C\n"
	if err := os.WriteFile(filepath.Join(root, "t.pdb"), []byte(pdb), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := fsReadStructure{root: root}.Execute(context.Background(), []byte(`{"path":"t.pdb"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Chains     map[string]string `json:"chains"`
		ChainCount int               `json:"chain_count"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if out.ChainCount != 1 || out.Chains["A"] != "MKT" {
		t.Errorf("chains = %#v, chain_count = %d", out.Chains, out.ChainCount)
	}
}

func TestFSReadStructureMMCIF(t *testing.T) {
	root := t.TempDir()
	mmcif := "data_test\n" +
		"loop_\n" +
		"_atom_site.group_PDB\n" +
		"_atom_site.id\n" +
		"_atom_site.label_atom_id\n" +
		"_atom_site.label_comp_id\n" +
		"_atom_site.label_asym_id\n" +
		"_atom_site.label_seq_id\n" +
		"ATOM 1 N  MET A 1\n" +
		"ATOM 2 CA MET A 1\n" +
		"ATOM 4 CA LYS A 2\n" +
		"ATOM 5 CA THR A 3\n"
	if err := os.WriteFile(filepath.Join(root, "t.cif"), []byte(mmcif), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := fsReadStructure{root: root}.Execute(context.Background(), []byte(`{"path":"t.cif"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Chains map[string]string `json:"chains"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if out.Chains["A"] != "MKT" {
		t.Errorf("chains = %#v", out.Chains)
	}
}

func TestFSReadStructureUnsupportedExtension(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "t.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (fsReadStructure{root: root}).Execute(context.Background(), []byte(`{"path":"t.txt"}`)); err == nil {
		t.Fatal("expected an error for an unsupported extension")
	}
}

func TestFSReadStructureRejectsEscape(t *testing.T) {
	if _, err := (fsReadStructure{root: t.TempDir()}).Execute(context.Background(), []byte(`{"path":"../escape.pdb"}`)); err == nil {
		t.Fatal("expected an error for a path escaping the workspace")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tools/ -run TestFSReadStructure`
Expected: FAIL — compile error, `undefined: fsReadStructure`.

- [ ] **Step 3: Write the tool implementation**

Create `internal/tools/fs_structure.go`:

```go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/pkg/proteinio"
)

// --- fs.read_structure ---

type fsReadStructure struct{ root string }

func (fsReadStructure) Name() string { return "fs.read_structure" }
func (fsReadStructure) Description() string {
	return "Read a protein structure file (.pdb, .cif, .mmcif) within the workspace and return its per-chain amino-acid sequences."
}
func (fsReadStructure) InputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path": strProp("Path within the workspace to a .pdb, .cif or .mmcif file"),
	}, "path")
}
func (fsReadStructure) RequiresConfirmation(json.RawMessage) bool       { return false }
func (fsReadStructure) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (fsReadStructure) EstimatedDuration(json.RawMessage) time.Duration { return 50 * time.Millisecond }

func (t fsReadStructure) Execute(_ context.Context, input json.RawMessage) (Result, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	abs, err := SafeJoin(t.root, in.Path)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{}, err
	}
	var chains map[string]string
	switch ext := strings.ToLower(filepath.Ext(in.Path)); ext {
	case ".pdb":
		chains, err = proteinio.ChainsFromPDB(bytes.NewReader(data))
	case ".cif", ".mmcif":
		chains, err = proteinio.ChainsFromMMCIF(bytes.NewReader(data))
	default:
		return Result{}, fmt.Errorf("fs.read_structure: unsupported extension %q (want .pdb, .cif or .mmcif)", ext)
	}
	if err != nil {
		return Result{}, err
	}
	out, err := json.Marshal(map[string]any{
		"chains":      chains,
		"chain_count": len(chains),
	})
	if err != nil {
		return Result{}, err
	}
	ids := make([]string, 0, len(chains))
	for id := range chains {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d chain(s)", in.Path, len(chains))
	for _, id := range ids {
		fmt.Fprintf(&b, "\n  %s: %d residues", id, len(chains[id]))
	}
	return Result{
		Output:     out,
		Display:    b.String(),
		Provenance: domain.NewToolCallRef("fs.read_structure", input),
	}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tools/ -run TestFSReadStructure`
Expected: PASS — all four `TestFSReadStructure*` tests green.

- [ ] **Step 5: Register the tool via `NewFSTools`**

In `internal/tools/fs.go`, update `NewFSTools` (lines ~42–49) to include the new tool and fix the doc comment count:

```go
// NewFSTools returns the five filesystem/structure tools bound to a workspace root.
func NewFSTools(root string) []Tool {
	return []Tool{
		fsRead{root: root}, fsWrite{root: root}, fsEdit{root: root},
		fsBash{root: root, binDir: buildBashSandbox()},
		fsReadStructure{root: root},
	}
}
```

No change to `cmd/proteus/main.go` is needed: it already registers every tool
returned by `tools.NewFSTools(workspace)` in a loop (`main.go:147-149`).

- [ ] **Step 6: Run gofmt, vet, and the full suite**

Run: `gofmt -l internal/tools/`
Expected: no output.

Run: `go vet ./... && go test ./...`
Expected: PASS — the whole repo builds and tests green; `fs.read_structure` is now registered.

- [ ] **Step 7: Commit**

```bash
git add internal/tools/fs_structure.go internal/tools/fs_structure_test.go internal/tools/fs.go
git commit -m "$(cat <<'EOF'
feat: add fs.read_structure tool for chain sequences

Reads a .pdb/.cif/.mmcif file from the workspace and returns per-chain
sequences via pkg/proteinio (ChainsFromPDB / ChainsFromMMCIF).

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Verification record `docs/VERIFICATION.md`

Write an honest per-criterion verification record for SPECS §20 milestones
v0.1–v0.4. No production code changes.

**Files:**
- Create: `docs/VERIFICATION.md`

- [ ] **Step 1: Inventory the test suite**

Run: `go test ./... 2>&1 | tail -40` and `find . -name '*_test.go' | sort`
Read `docs/SPECS.md` lines 2399–2502 (the §20 milestone acceptance criteria).
Note which test packages exist and pass. This inventory is the evidence base for the table.

- [ ] **Step 2: Write `docs/VERIFICATION.md` — header and legend**

Create `docs/VERIFICATION.md` starting with:

```markdown
# Acceptance Criteria Verification Record

This is the honest companion to the `v0.3.0` / `v0.4.0` release tags. It records
the verification status of every acceptance criterion in `docs/SPECS.md` §20 for
milestones v0.1–v0.4. Where a criterion cannot be checked in the current
environment, this document says so and says what is required.

_Last updated: 2026-05-19._

## Status legend

| Status | Meaning |
|--------|---------|
| `auto` | Covered by an automated test in the repo; the test passes. |
| `manual-ok` | Verified by hand; reproducible by a human following the criterion. |
| `blocked-gpu` | Needs a working GPU build; blocked by the GB10 `sm_121` PyTorch incompatibility. |
| `blocked-account` | Needs an external account (Adaptyv Foundry, Modal). |
| `needs-eyes` | Needs a human to view TUI or graphical output. |
| `deviation` | The SPECS §20 text is stale; the criterion is met via a different surface (see Notes). |
```

- [ ] **Step 3: Write one table per milestone**

For each milestone (v0.1, v0.2, v0.3, v0.4), add a section with a table whose
columns are: `#`, `Criterion` (abbreviated from §20), `Status` (a legend value),
`Evidence` (test package/name or command, or `—`), `To verify` (what hardware,
account, or human action is still needed, or `—`).

Classification rules — apply them per criterion:

- A criterion whose logic is exercised by a passing test → `auto`, cite the test package (e.g. `internal/store/`, `internal/jobs/`, `internal/tools/knowledge/`, `internal/backends/local/`, `internal/llm/`).
- A criterion needing a real GPU tool run (v0.2 #2,3,4,6 actual installs/runs; v0.4 #5,6,7) → `blocked-gpu`, with `To verify` = "run on an `sm_121`-capable PyTorch build".
- A criterion needing Adaptyv (v0.4 #1,2,3) or Modal (v0.2 #5) → `blocked-account`.
- A criterion needing a human to view TUI output (v0.4 #8,9,10; v0.1 #1,4) → `needs-eyes` or `manual-ok` if a test covers the wiring.
- A criterion naming a CLI subcommand that is now a TUI slash command → `deviation` (see Step 4).
- v0.4 #11 (`go test ./...` / `go vet ./...` clean) → `auto`, evidence = "CI + `go test ./...`".

Every row marked `auto` MUST cite a test that exists and passes. Verify each one: open the cited test file, confirm it covers the claim, and confirm it is in the passing set from Step 1. If no test covers the claim, do not mark it `auto` — downgrade to `manual-ok`, `needs-eyes`, or a `blocked-*` status as appropriate.

- [ ] **Step 4: Write the Notes section**

Append a `## Notes` section recording these two facts verbatim:

```markdown
## Notes

### v0.2 criterion 6 was not functional at the `v0.2.0` tag

At the `v0.2.0` tag, the `design.*` → backend → real tool path was stubbed:
criterion 6 ("agent runs BindCraft, scores, returns designs") was not
functional. The genuine `ToolAdapter` wiring (ProteinMPNN, RFdiffusion,
BindCraft — sub-plans SP1–3) landed afterward. The criterion's *logic* is now
covered by tests; a real end-to-end GPU run remains `blocked-gpu`.

### CLI → TUI deviation

SPECS §20 lists `proteus install <tool>`, `proteus list tools`, `proteus doctor`
(v0.2) and `proteus auth adaptyv` (v0.4) as CLI subcommands. The `proteus`
binary now exposes only `tui` and `version`; those operations are TUI slash
commands (`/install`, `/doctor`, `/auth`). The affected criteria are marked
`deviation` and are treated as met via the TUI equivalent. The SPECS §20 text
itself is stale and is out of scope for this branch.

### `proteinio.WriteFASTA` is unwired by design

`pkg/proteinio` exports `ParseFASTA`, `WriteFASTA`, `ChainsFromPDB`, and
`ChainsFromMMCIF`. As of this branch, `ParseFASTA` is used by the ProteinMPNN
adapter and `ChainsFromPDB`/`ChainsFromMMCIF` by the `fs.read_structure` tool.
`WriteFASTA` has no production caller and remains a library-only export,
available to importers of `pkg/proteinio`.
```

- [ ] **Step 5: Verify every `auto` claim**

Run: `go test ./...`
Expected: PASS. Cross-check: each test package cited in an `auto` row appears in the passing output. Fix any row whose cited test does not exist or does not cover the claim.

- [ ] **Step 6: Commit**

```bash
git add docs/VERIFICATION.md
git commit -m "$(cat <<'EOF'
docs: add acceptance-criteria verification record

Honest per-criterion status for SPECS section 20 milestones v0.1-v0.4.
Records the v0.2 #6 tag-time gap and the CLI->TUI deviation.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Annotated release tags `v0.3.0` and `v0.4.0`

**Interactive — run in the main session, not a parallel agent.** This task
confirms commit SHAs and a tag push with the user.

**Files:** none (git tags only). Run after Tasks 1–3 are merged to the branch.

- [ ] **Step 1: Determine the `v0.4.0` anchor commit**

The config-system feature (SPECS §14) is post-v0.4 work. `v0.4.0` is placed at
the master commit immediately before the config-system merge.

Run: `git log --first-parent --oneline master | head -20`
Run: `git rev-parse 6390a8f^1`  (the parent of `Merge feat/config-system`)

The result of `git rev-parse 6390a8f^1` is the `v0.4.0` candidate. Confirm `6390a8f` is still the config-system merge with `git log -1 --format='%s' 6390a8f`.

- [ ] **Step 2: Determine the `v0.3.0` anchor commit**

v0.3 deliverables (SPECS §20 v0.3 *Implements*): `internal/tools/knowledge/`,
`internal/skills/builtin/plan-from-target.md`, `internal/skills/builtin/design-binder.md`,
`pkg/proteinio/`, `internal/tools/plan/`, the `/plan` TUI view.

Run: `git log --diff-filter=A --oneline -- internal/tools/knowledge/ internal/tools/plan/ pkg/proteinio/ internal/skills/builtin/plan-from-target.md`

The `v0.3.0` candidate is the latest (most recent) commit among the *last* v0.3
deliverable to merge. Because v0.3 and v0.4 work interleaved, this commit may
topologically precede some v0.4 commits — that is expected and acceptable. Use
`git log --oneline <candidate>..<v0.4.0-candidate>` to sanity-check that the
range between the two tags is non-empty and is v0.4-flavoured work.

- [ ] **Step 3: Present both candidate SHAs to the user and get confirmation**

Show the user: the two candidate SHAs, each commit's subject line, and a
one-line rationale. **Wait for explicit confirmation before creating tags.**
If the user wants different anchors, use those.

- [ ] **Step 4: Compose the tag messages from `docs/VERIFICATION.md`**

For each tag, build an annotated message in this shape (fill the counts and the
blocked list from the finished `docs/VERIFICATION.md` tables):

```
proteus v0.4.0 — "Closing the loop"

Verification (see docs/VERIFICATION.md):
  <N> of <M> v0.4 acceptance criteria auto-verified, <K> manual.
  Blocked: #<a>,#<b> (Adaptyv account), #<c>,#<d> (GPU — GB10 sm_121),
  #<e>,#<f> (visual check).

Note: §20 lists `proteus auth adaptyv` as a CLI subcommand; it is now the
TUI `/auth` slash command.
```

The `v0.3.0` message follows the same shape with v0.3's counts; v0.3 has no
GPU-blocked criteria.

- [ ] **Step 5: Create the annotated tags**

```bash
git tag -a v0.3.0 <v0.3.0-sha> -m "<v0.3.0 message>"
git tag -a v0.4.0 <v0.4.0-sha> -m "<v0.4.0 message>"
```

- [ ] **Step 6: Verify the tags**

Run: `git tag -l` — expect `v0.1.0`, `v0.2.0`, `v0.3.0`, `v0.4.0`.
Run: `git show v0.3.0 --stat | head -30` and `git show v0.4.0 --stat | head -30`
Expected: each shows the annotated message and points at the confirmed commit.

- [ ] **Step 7: Ask the user before pushing**

Tags are local only. Ask the user explicitly whether to push:
`git push origin v0.3.0 v0.4.0`. Do not push without an explicit go-ahead.

---

## Self-Review

- **Spec coverage:** Workstream 1 (proteinio) → Tasks 1 (1a, `ParseFASTA`) + 2 (1b, `fs.read_structure` wiring `ChainsFromPDB`/`ChainsFromMMCIF`); 1c (`WriteFASTA` left unwired) → recorded in Task 3 Step 4. Workstream 2 (verification record) → Task 3. Workstream 3 (annotated tags) → Task 4. All design sections covered.
- **Placeholder scan:** Task 4's tag messages use `<...>` slots — these are genuine runtime values (SHAs, criterion counts) that cannot exist until Tasks 1–3 land and the user confirms; Task 4 is the interactive task that fills them. No code step contains a placeholder.
- **Type consistency:** `fsReadStructure` (struct), `Name()`/`Description()`/`InputSchema()`/`RequiresConfirmation()`/`EstimatedCostUSD()`/`EstimatedDuration()`/`Execute()` match the `tools.Tool` interface. `proteinio.Record` fields `Header`/`Sequence`, and functions `ParseFASTA`/`ChainsFromPDB`/`ChainsFromMMCIF` match `pkg/proteinio`. `objectSchema`/`strProp`/`SafeJoin`/`domain.NewToolCallRef` are existing helpers used consistently.
