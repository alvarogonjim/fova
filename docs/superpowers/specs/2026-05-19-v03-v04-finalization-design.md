# Design: v0.3/v0.4 Finalization

**Date:** 2026-05-19
**Branch:** `chore/finalize-v03-v04` (off `master`)
**Status:** Approved — ready for implementation planning

## Context

A review of the repository against `docs/SPECS.md` surfaced five findings. This
work addresses three of them in a single release-hygiene branch:

1. **Untagged releases.** Only `v0.1.0` and `v0.2.0` tags exist. v0.3 is
   complete and merged; v0.4's code is all present. Both need release tags.
4. **`pkg/proteinio` built but unwired.** The FASTA/PDB/mmCIF parsers exist and
   are tested, but no production code calls them — a v0.3 deliverable
   (SPECS §3.3, §20 v0.3) that was never integrated into a tool.
5. **Manual acceptance criteria are unverifiable here.** Real GPU runs
   (v0.2 #2–6, v0.4 #5–7) are blocked by the GB10 `sm_121` PyTorch
   incompatibility; Adaptyv staging (v0.4 #1–3) needs an account; inline
   graphics need a human to look. There is no honest, written record of which
   §20 criteria are actually verified.

Findings 2 (the SPECS §20 CLI→TUI text is stale) and 3 (v0.2 #6 was not
functional at the `v0.2.0` tag) are **out of scope** for code changes, but are
recorded factually by Workstream 2 so the verification record stays accurate.

## Goals

- Give `pkg/proteinio` real production consumers so it is no longer dead code.
- Produce an honest, per-criterion verification record for milestones v0.1–v0.4.
- Tag `v0.3.0` and `v0.4.0` as annotated tags whose messages embed the
  verification status, so the tags reflect reality (unlike `v0.2.0`).

## Non-goals

- Rewriting the SPECS §20 CLI subcommand text (finding 2).
- Fixing the GB10 `sm_121` PyTorch incompatibility or running real GPU tools.
- Running anything against the Adaptyv staging environment.
- Wiring `proteinio.WriteFASTA` — it has no natural caller and stays a library
  export (see Workstream 1c).

---

## Workstream 1 — Wire `pkg/proteinio`

`pkg/proteinio` exports four functions. After this workstream, three have
production callers; `WriteFASTA` remains a library-only export.

### 1a. ProteinMPNN adapter: use `proteinio.ParseFASTA`

**File:** `internal/backends/local/adapter_proteinmpnn.go`

The adapter currently parses ProteinMPNN's FASTA output with a local
`fastaRecord` type and a `parseFASTARecords` function.

- Delete the `fastaRecord` type and the `parseFASTARecords` function.
- In `parseProteinMPNNOutput`, replace `parseFASTARecords(string(body))` with
  `proteinio.ParseFASTA(bytes.NewReader(body))`, handling the returned error.
- Use `rec.Header` / `rec.Sequence` (the `proteinio.Record` field names) in
  place of `rec.header` / `rec.seq`.
- `proteinMPNNScores(rec.Header)` and `splitChains(rec.Sequence)` are otherwise
  unchanged.
- Keep the existing `strings.TrimSpace(rec.Sequence) == ""` guard that skips a
  malformed "header with no sequence" record, and the `i == 0` guard that skips
  the native input sequence.

**Behavior note:** `proteinio.ParseFASTA` skips blank lines, supports multi-line
sequences, and returns an error on a sequence line before any header. This is a
superset of the deleted ad-hoc parser's behavior; no ProteinMPNN output regresses.

No test file references `fastaRecord` or `parseFASTARecords`; the adapter test
exercises this code through `parseProteinMPNNOutput`'s public path and continues
to pass unchanged.

### 1b. New tool: `fs.read_structure`

**File:** `internal/tools/fs.go` (new tool type) — or a sibling
`internal/tools/fs_structure.go` if `fs.go` grows unwieldy; implementer's call.

A new agent tool that reads a local structure file and returns its chain
sequences, giving `proteinio.ChainsFromPDB` and `proteinio.ChainsFromMMCIF`
production callers.

- **Name:** `fs.read_structure`
- **Namespace rationale:** it reads a file within the workspace, like the other
  `fs.*` tools; it is workspace-root-bound and added to the slice returned by
  `NewFSTools(root)` so it is registered through the existing loop in
  `cmd/proteus/main.go`.
- **Input schema:** `{ "path": string }` — a path within the workspace to a
  `.pdb`, `.cif`, or `.mmcif` file.
- **Behavior:** resolve `path` with `SafeJoin(root, path)`; dispatch by
  lowercased file extension — `.pdb` → `proteinio.ChainsFromPDB`,
  `.cif`/`.mmcif` → `proteinio.ChainsFromMMCIF`; an unrecognized extension is an
  error.
- **Output (JSON):** `{ "chains": { "A": "SEQ…", … }, "chain_count": N }`.
- **Tool metadata:** `RequiresConfirmation` false, `EstimatedCostUSD` 0,
  `EstimatedDuration` small (~50ms, matching `fs.read`).

This is a minor extension beyond the SPECS §7.2 v1 tool list; the design doc
records it as intentional.

### 1c. `WriteFASTA`

`proteinio.WriteFASTA` has no natural production caller in the current
codebase. It stays a `pkg/proteinio` library export, available to importers.
`docs/VERIFICATION.md` records this explicitly as a known, accepted gap.

### Testing — Workstream 1

- New unit test for `fs.read_structure`: an inline PDB string and an inline
  mmCIF `_atom_site` loop each yield the expected chain map; an unsupported
  extension returns an error; a path outside the workspace is rejected.
- `go test ./...`, `go vet ./...`, and `gofmt -l` all clean.

---

## Workstream 2 — Verification record

**File (new):** `docs/VERIFICATION.md` — a living document.

A per-criterion matrix covering every acceptance criterion in SPECS §20 for
milestones v0.1, v0.2, v0.3, and v0.4. One table per milestone; each row:

| Column | Meaning |
|--------|---------|
| **Criterion** | The §20 number and an abbreviated restatement. |
| **Status** | One of: `auto` (covered by an automated test), `manual-ok` (verified by hand this session), `blocked-gpu` (needs a working GPU build), `blocked-account` (needs an Adaptyv account/staging), `needs-eyes` (needs a human to view output), `deviation` (the spec text is stale; met via a different surface). |
| **Evidence** | Test name / package, or the command run, or `—`. |
| **To verify** | What hardware, account, or human action is still required. |

The document also includes a short narrative section recording two facts
honestly:

- **v0.2 #6 at tag time.** At the `v0.2.0` tag, the `design.*` → backend → real
  tool path was stubbed; criterion 6 was not functional. The genuine
  `ToolAdapter` wiring (ProteinMPNN/RFdiffusion/BindCraft, SP1–3) landed
  afterward. The criterion is satisfied now but was not at the tag.
- **CLI→TUI deviation.** SPECS §20 lists `proteus install` / `list tools` /
  `doctor` (v0.2) and `proteus auth adaptyv` (v0.4) as CLI subcommands. The
  binary now exposes only `tui` and `version`; those operations are TUI slash
  commands (`/install`, `/doctor`, `/auth`). The affected criteria are marked
  `deviation` and treated as met via the TUI equivalent.

Every criterion marked `auto` must name a test that exists and passes; this is
verified while writing the document (run the cited tests).

This file is the source for the tag messages in Workstream 3, and is the
artifact a future SPECS §20 rewrite (finding 2) would keep in sync.

---

## Workstream 3 — Annotated release tags

Create annotated git tags `v0.3.0` and `v0.4.0`.

### Tag messages

Each tag message contains a condensed verification summary derived from
`docs/VERIFICATION.md`:

- A count line: "N/M criteria auto-verified, K manual-ok, the remainder blocked
  on GPU / Adaptyv account / visual check."
- The headline deviation note (CLI→TUI) for that milestone.
- For `v0.3.0`, no GPU-blocked criteria; for `v0.4.0`, the blocked GPU/account
  criteria are listed by number.

### Tag placement

v0.3 and v0.4 work interleaved on `master` — there is no single commit where
"v0.3 is done and v0.4 has not started." Placement methodology:

- Anchor each tag at the commit where that milestone's **last spec'd deliverable
  merged**, determined during implementation with
  `git log --diff-filter=A -- <milestone file set>` over the files SPECS §20
  lists under each milestone's *Implements* section.
- `v0.4.0` is placed **before** the config-system feature merge
  (`6390a8f Merge feat/config-system`), since the config system (SPECS §14) is
  post-v0.4 work.
- The candidate SHAs are presented to the user for confirmation **before the
  tags are created**.

### Publishing

Tags are created **locally only**. Pushing them to `origin` is a separate,
explicit step taken on the user's go-ahead — publishing tags is outward-facing
and is not done automatically.

### Testing — Workstream 3

- `git show v0.3.0` and `git show v0.4.0` render the annotated message and point
  at the confirmed commits.
- `git tag -l` lists all four tags.

---

## Build sequence

1. **Workstream 2** — write `docs/VERIFICATION.md`; run every cited `auto` test
   to confirm it passes. Commit.
2. **Workstream 1** — adapter swap (1a), then `fs.read_structure` (1b) with its
   test; register it; `go test ./... && go vet ./...`. Commit (one or two
   commits).
3. **Workstream 3** — determine anchor SHAs, confirm with the user, create the
   two annotated tags. Confirm with the user before any push.

Workstream 2 precedes Workstream 3 because the tag messages quote it.
Workstream 1 is independent and can be done in any order relative to 2.

## Risks

- **Tag placement on interleaved history.** Mitigated by the
  `--diff-filter=A` methodology and explicit user confirmation of the SHAs
  before tags are created.
- **`fs.read_structure` is not in the SPECS §7.2 v1 tool list.** A minor,
  deliberate spec extension; recorded in this design and in `VERIFICATION.md`.
- **`ParseFASTA` behavior difference.** The new parser errors on
  sequence-before-header where the old one silently dropped the line. This
  cannot occur in well-formed ProteinMPNN output; if it ever did, surfacing the
  error is the correct behavior.
