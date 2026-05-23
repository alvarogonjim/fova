# Design — `knowledge.pdb_search`

**Date:** 2026-05-23
**Branch:** `feat/knowledge-pdb-search`
**Worktree:** `/home/gonjim/Projects/proteus-pdbsearch`

## 1. Problem

`knowledge.pdb` is ID-lookup only — it wraps `data.rcsb.org/.../core/entry`. With
no way to search by target, the agent guesses PDB IDs from its training data.
During the 2026-05-21 validation session it claimed `6Q3B` was a PD-L1 structure
(it is CDK2) and fetched several unrelated entries. See
`docs/KNOWN-ISSUES-2026-05-21.md` §5.

The fix is a new, separate tool — `knowledge.pdb_search` — backed by the RCSB
search API (`search.rcsb.org`) so a query like `"PD-L1"` resolves
deterministically to a small set of candidate PDB IDs with enough metadata for
the agent to pick from.

## 2. Scope

**In scope.** A single agent tool, `knowledge.pdb_search`, that:

- takes a free-text query plus optional light attribute filters (organism,
  experimental method, max resolution),
- returns the top hits as `{pdb_id, score, title, method, resolution, year}`
  records with a `total_hits` summary,
- lives in `internal/tools/knowledge/`, registered once in `cmd/fova/main.go`.

**Out of scope.**

- Any change to `knowledge.pdb` — it stays an ID-lookup; this is a separate tool.
- A local PDB index — only the live RCSB search API.
- Sequence/structural similarity search (RCSB supports them, but the
  hallucination case is solved by full-text + filters).
- Edits to `internal/domain/`, `/plan`, or the design tools.
- Cross-session coordination — this branch's only shared edit is one new line
  in `cmd/fova/main.go` (registration alongside the other `knowledge.New*`
  lines).

## 3. Tool surface

**Name:** `knowledge.pdb_search`

**Description (shown to the agent):** "Search the RCSB PDB by free text and
optional filters; returns candidate PDB IDs with title, method, and resolution.
Use this *before* `knowledge.pdb` when you do not already know the entry ID."

**Input schema.**

```jsonc
{
  "type": "object",
  "properties": {
    "query":          { "type": "string",  "description": "Free-text search (target name, ligand, etc.). Required." },
    "organism":       { "type": "string",  "description": "Optional scientific name, e.g. \"Homo sapiens\"." },
    "method":         { "type": "string",  "description": "Optional experimental method, e.g. \"X-RAY DIFFRACTION\", \"ELECTRON MICROSCOPY\"." },
    "max_resolution": { "type": "number",  "description": "Optional resolution upper bound, in Å." },
    "limit":          { "type": "integer", "description": "Max results (default 10, max 25)." }
  },
  "required": ["query"]
}
```

**Output (JSON body returned to the agent).**

```jsonc
{
  "count": 7,
  "total_hits": 213,
  "results": [
    {
      "pdb_id": "5O45",
      "score": 0.94,
      "title": "Crystal structure of human PD-L1 in complex with ...",
      "method": "X-RAY DIFFRACTION",
      "resolution": 2.20,
      "year": 2017
    }
  ]
}
```

- `count` is `len(results)` after enrichment.
- `total_hits` is what RCSB reports before paging — useful signal that the
  query is over-broad.
- Results preserve RCSB's score-descending order.

**Display line** (one-liner shown in the TUI):
`pdb_search "<query>": 7 of 213 hits (top: 5O45 PD-L1 ... 2.20 Å)`.

**Tool interface trait values:**

- `RequiresConfirmation`: false (read-only, no side effects).
- `EstimatedCostUSD`: 0 (free public API).
- `EstimatedDuration`: 4 s (two POSTs; matches `knowledge.pdb`'s budget at ~2×).

## 4. Architecture

The tool performs **two HTTP calls per `Execute`**: a search, then a batched
enrichment.

### 4.1 Phase 1 — search

- **Endpoint:** `POST https://search.rcsb.org/rcsbsearch/v2/query`
- **Body (sketch):**

  ```jsonc
  {
    "query": {
      "type": "group",
      "logical_operator": "and",
      "nodes": [
        { "type": "terminal", "service": "full_text", "parameters": { "value": "<query>" } },
        // 0..3 optional filter nodes follow:
        { "type": "terminal", "service": "text",
          "parameters": { "attribute": "rcsb_entity_source_organism.ncbi_scientific_name",
                          "operator": "exact_match", "value": "<organism>" } },
        { "type": "terminal", "service": "text",
          "parameters": { "attribute": "exptl.method",
                          "operator": "exact_match", "value": "<method>" } },
        { "type": "terminal", "service": "text",
          "parameters": { "attribute": "rcsb_entry_info.resolution_combined",
                          "operator": "less_or_equal", "value": <max_resolution> } }
      ]
    },
    "return_type": "entry",
    "request_options": {
      "paginate": { "start": 0, "rows": <limit> },
      "sort":     [ { "sort_by": "score", "direction": "desc" } ]
    }
  }
  ```

  If only `query` is provided (no filters), the `group` collapses to a single
  `terminal` node — same wire shape, one node in `nodes`. (For simplicity the
  builder always emits a `group`; one-node groups are accepted by RCSB.)

- **Response (relevant fields):**

  ```jsonc
  { "result_set": [ { "identifier": "5O45", "score": 0.94 }, ... ],
    "total_count": 213 }
  ```

- **204 No Content** is RCSB's "zero hits" signal. We treat it as
  `total_hits: 0, results: []` and **skip phase 2**. Not an error.

### 4.2 Phase 2 — enrich (GraphQL batch)

- **Endpoint:** `POST https://data.rcsb.org/graphql`
- **Body:** a single GraphQL query of the form

  ```graphql
  { entries(entry_ids: ["5O45", "5N2D", ...]) {
      rcsb_id
      struct { title }
      exptl { method }
      rcsb_entry_info { resolution_combined initial_release_date }
  } }
  ```

  The IDs come from phase 1's `result_set`. One round trip, no per-ID fan-out.

- **Response:** an array of `entries`. We index it into a `map[string]entry`
  keyed by `rcsb_id` and look up each phase-1 ID, so we do **not** depend on
  GraphQL returning entries in input order. Output rows are emitted in
  phase-1's score-descending order.
- A null entry, or a phase-1 ID with no matching map entry (entry vanished or
  is obsolete between phase 1 and phase 2), produces a row with the known
  `pdb_id` + `score` but empty enriched fields — not an error.

### 4.3 Plumbing — `client.go`

Add one helper next to `getJSON`:

```go
// postJSON sends a JSON body and decodes a JSON response. Non-2xx is an error,
// except 204 No Content which leaves out untouched and returns nil.
func postJSON(ctx context.Context, url string, body, out any) error
```

It uses the same `httpClient`, sets `User-Agent` and `Content-Type:
application/json`, applies the same 8 MiB response cap as `getBytes`, and
returns the same "%s returned %d: …" error shape on non-2xx. The 204
short-circuit is what lets the search caller treat zero-hits cleanly without
duplicating status checks.

Nothing else in the package POSTs JSON yet; this helper is small, focused, and
re-usable for the next tool that needs it (e.g. a future GraphQL caller).

### 4.4 File layout

- `internal/tools/knowledge/pdb_search.go` — the tool (one file).
- `internal/tools/knowledge/pdb_search_test.go` — tests.
- `internal/tools/knowledge/client.go` — adds `postJSON` only.
- `cmd/fova/main.go` — one new line, immediately after `registry.Register(knowledge.NewPDB())`:
  ```go
  registry.Register(knowledge.NewPDBSearch())
  ```

## 5. Error handling

| Condition                                  | Behavior                                                  |
|--------------------------------------------|-----------------------------------------------------------|
| `query` empty/missing                      | Return `error: knowledge.pdb_search: query is required`.  |
| `limit <= 0`                               | Default to 10.                                            |
| `limit > 25`                               | Clamp to 25.                                              |
| Search returns 204                         | Output `{count:0, total_hits:0, results:[]}`. Skip phase 2. |
| Search returns non-2xx                     | Return error verbatim from `postJSON`.                    |
| GraphQL returns non-2xx                    | Return error verbatim.                                    |
| GraphQL returns null for an entry          | Emit row with score + ID, empty enriched fields.          |
| `ctx.Done()`                               | Propagated by `http.NewRequestWithContext` — no extra logic. |

## 6. Tests

All tests use `httptest.Server` — no live RCSB calls. Fixtures are inline Go
string constants to match the existing `pdbBody` pattern in `pdb_test.go`.

1. **`TestPDBSearchExecute`** — happy path with `query` only.
   - Two `httptest.Server`s, one for search, one for GraphQL.
   - Search returns 2 hits with `total_count: 213`.
   - GraphQL returns title/method/resolution/release-date for both.
   - Asserts: `count == 2`, `total_hits == 213`, IDs and scores preserved in
     RCSB order, title/method/resolution/year populated, `Display` non-empty,
     `Provenance` set.

2. **`TestPDBSearchExecute_WithFilters`** — filters reach the wire.
   - Input has `organism: "Homo sapiens"`, `max_resolution: 3.0`.
   - Search server captures the request body and asserts it contains a `group`
     query whose `nodes` include both a `full_text` terminal and `text`
     terminals for `rcsb_entity_source_organism.ncbi_scientific_name` and
     `rcsb_entry_info.resolution_combined`. (Decode the body and walk it; do
     not string-match.)

3. **`TestPDBSearchExecute_ZeroHits`** — search server returns `204 No Content`.
   - Output is `{count:0, total_hits:0, results:[]}`.
   - GraphQL server is wired but instrumented to fail the test if hit (we must
     not enrich an empty list).

4. **`TestPDBSearchExecute_PartialEnrichment`** — GraphQL returns `null` for
   one of the entries.
   - That row is emitted with `pdb_id` + `score` set, enriched fields empty.
   - The other row is fully populated. No error.

5. **`TestPDBSearchExecuteEmptyQuery`** — empty `query` returns an error and
   makes no HTTP call.

6. **`var _ tools.Tool = (*PDBSearch)(nil)`** at file top — compile-time
   interface check, same as `pdb_test.go`.

## 7. Risks & non-risks

- **Risk: RCSB schema drift.** Field names like
  `rcsb_entry_info.resolution_combined` come from RCSB's text-search schema and
  could in principle be renamed. Mitigation: keep all schema strings in named
  constants at the top of `pdb_search.go` so a future fix is one diff hunk.
- **Risk: Search/GraphQL skew between phases.** Phase 1 yields an ID, phase 2
  returns null for it. Already handled (partial-enrichment case).
- **Non-risk: Merge conflicts.** Per the kickoff brief, this track's only
  shared edit is one new registration line; no other concurrent session
  touches that area.
- **Non-risk: Performance.** Two POSTs to a free public API. We do not cache —
  the agent's call frequency is low and RCSB is fast.

## 8. Acceptance

- `go build ./...` passes.
- `go test ./internal/tools/knowledge/ ./cmd/fova/` passes.
- `gofmt -l .` reports nothing.
- A live query like `"PD-L1"` against the real API returns plausible top hits
  (smoke-checked manually, not in CI).
- The new tool is registered in `cmd/fova/main.go` and shows up in the agent's
  tool list at startup.
