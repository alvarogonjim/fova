# Kickoff — `knowledge.pdb_search`

You are a fresh Claude session driving **a new `knowledge.pdb_search` agent tool** for the fova project. Read this brief first.

## Why

The validation session (`docs/KNOWN-ISSUES-2026-05-21.md` #5) caught a real footgun: `knowledge.pdb` is **ID-lookup only** (it wraps `data.rcsb.org/.../core/entry`). With no search-by-target, the agent hallucinates PDB IDs from training data — during a PD-L1 binder run it claimed `6Q3B` was a PD-L1 structure (it is actually CDK2). The fix is a small new tool wrapping the RCSB **search** API (`search.rcsb.org`) so "PD-L1" resolves deterministically to candidate IDs.

## Project context (orient yourself in this order)

1. `MEMORY.md` (auto-loaded) — cross-session picture of the tool-integration effort.
2. `docs/KNOWN-ISSUES-2026-05-21.md` §5 — the gap description.
3. `internal/tools/knowledge/pdb.go` — the existing `knowledge.pdb` ID-lookup tool (your template).
4. `internal/tools/knowledge/` — the package; every knowledge tool is one file + one test file + one `Register` call in `cmd/fova/main.go`.

## Branch + worktree

- **Worktree:** `/home/gonjim/Projects/proteus-pdbsearch` (you are likely already in it; if not, `EnterWorktree path=…`)
- **Branch:** `feat/knowledge-pdb-search` (off `dev` at `8f5dfc5`)

## Your task — concise scope

Add `knowledge.pdb_search`: a single agent tool that takes a free-text query (e.g. `"PD-L1"`, `"PDB structures of CD20 with rituximab"`) and returns a list of candidate PDB IDs (with title + resolution + a one-line summary), backed by the RCSB search API.

### Out of scope (do not expand)

- Mutations to `knowledge.pdb` — it stays an ID-lookup; the search tool is separate.
- A local PDB index — only the RCSB search API.
- Cross-session changes — no edits to `internal/domain/`, `/plan`, or design tools.

## Cross-session friction

You are running concurrently with two other Claude sessions (`feat/rfdiffusion2` and `feat/editable-review`). Yours is the cleanest of the three — **you should hit zero merge conflicts** when this lands on `dev`. Your only shared edit is one new registration line in `cmd/fova/main.go` (alongside the other `knowledge.New*` lines), which neither sibling session touches.

## Pattern to follow

This is a **small** tool — don't over-engineer. The full umbrella pattern (Foundation + 4 parallel Opus agents) is overkill here. Recommended cycle:

1. **Brainstorm** (superpowers:brainstorming) — explore the RCSB search API surface (`https://search.rcsb.org/index.html#search-api`); confirm with the user: which search modes (full-text? attribute filters? both?), result-set size cap, what fields to surface (title, resolution, method, chains, organism?). 2-3 clarifying questions max.
2. **Spec** → `docs/superpowers/specs/2026-05-23-knowledge-pdb-search-design.md`.
3. **Plan** (superpowers:writing-plans) — a small inline plan (no parallel Opus agents needed; the tool fits in one file). Save to `docs/superpowers/plans/2026-05-23-knowledge-pdb-search.md`.
4. **Implement inline** — `internal/tools/knowledge/pdb_search.go` + test + one line in `cmd/fova/main.go`. Follow `internal/tools/knowledge/pdb.go`'s shape exactly (constructor signature, `Tool` interface methods, HTTP fetcher pattern).
5. Verify `go build ./...` + `go test ./internal/tools/knowledge/ ./cmd/fova/` + `gofmt -l`.
6. Commit on `feat/knowledge-pdb-search`.

## Upstream reference

- API root: `https://search.rcsb.org/rcsbsearch/v2/query`
- Docs: `https://search.rcsb.org/index.html#search-api`
- Query types: full-text, attribute, sequence, structural — start with full-text (covers the hallucination case).

## Tests

- Unit-test the request-builder and the response parser against a committed fixture JSON. Do **not** hit the live API in tests (network flakiness).

## Merge to `dev`

When build + tests are green and you have user approval on the spec, merge `feat/knowledge-pdb-search` into `dev` (the integration/validation branch — see `[[dev-integration-branch]]` memory). The merge will be **clean** (no overlap with other tracks).

## Stop conditions — involve the user

- Spec approval (after brainstorm).
- Plan approval (before implementation).
- Dev merge (outward-ish action; confirm).

That's it. This track is small and bounded. Aim for tight scope; resist feature creep.
