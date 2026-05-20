# Proteus — Code Review and Feature-Gap Analysis

**Date:** 2026-05-20
**Branch reviewed:** `master @ 075d1d3 Merge v0.5 SP-A: theming + keybindings`
**Scope:** the whole codebase (~80 Go files, 22 packages), with awareness that v0.5 SP-B…SP-G are in flight on parallel branches.

## Executive summary

The codebase is in **unusually good shape**. Every automated check is clean:
`go build ./...`, `go vet ./...`, `gofmt -l`, `staticcheck` (0 findings),
`deadcode -test` (0 findings), and `go test -race ./...` (22 packages, no
failures, no race detections). There is no truly dead code; there are no
silently-broken patterns; the goroutine and SQL-resource patterns are correct.

What this review surfaces is therefore not bugs — it is **feature gaps versus
general-purpose CLI coding agents** (Claude Code, OpenAI Codex, Sourcegraph
OpenCode, Gemini CLI), a small number of **stylistic / robustness
recommendations**, and **test-coverage observations**. Because v0.5 sub-projects
A–G are concurrently reshaping the TUI, knowledge, safety, viz, and replay
surfaces, this report deliberately limits proposed changes to the stable
islands of the codebase.

---

## 1. Feature gap vs general-purpose CLI coding agents

Proteus is a **specialised** agent — it is built for de novo protein design,
not general code editing. So a feature-gap comparison is only meaningful when
it tells us which generalist-agent features would still be valuable inside
Proteus's domain. The applicability column does that triage.

### 1.1 What Proteus already has (baseline)

- Multi-provider LLM stack with a registry (`internal/llm/`): Anthropic,
  OpenAI-compatible (covers Ollama), Google Gemini, vLLM. Switchable at runtime
  via `/model`.
- Skills system: markdown files under `internal/skills/builtin/` loaded by
  `internal/skills/loader.go`.
- Tool registry (`internal/tools/registry.go`) with per-tool cost estimate,
  duration estimate, and a confirmation-required flag.
- Slash-command autocomplete with key handling (v0.4 — `internal/tui/slashmenu.go`).
- Persistent SQLite store with seven tables — sessions, designs, plans, jobs,
  corpus, experiments, webhook_events (`internal/store/`).
- Bubble Tea TUI with multiple side panels and an animated thinking indicator.
- Async job manager with cancellation, ETAs, per-job log files
  (`internal/jobs/manager.go`).
- Webhook receiver with HMAC validation for Adaptyv (`internal/tools/lab/webhook.go`).
- Per-project literature corpus over Bleve (`internal/tools/knowledge/corpus.go`).
- A local backend with `uv`-driven installer / registry / runner / doctor, and
  a Modal cloud backend, behind a unified `Backend` interface.
- Tool adapters for ProteinMPNN, RFdiffusion, BindCraft (and v0.4 adds
  RFantibody, RFdiffusion2, LigandMPNN, Chai2, Boltz2, Chai1).
- Config TOML system with `[defaults]`, `[knowledge]`, `[budget]`, `[webhook]`
  (SP1–SP3).
- Per-turn / per-session LLM cost tracking with soft-limit warning.
- Annotated release tags carrying a verification summary (`docs/VERIFICATION.md`).
- Embedded safety guard with restricted-target table (v0.5 SP-E, in progress).

### 1.2 Generalist-agent features Proteus does **not** have

The table lists features present in at least one of {Claude Code, Codex,
OpenCode, Gemini CLI} that Proteus lacks, with where it sits and whether it
would benefit Proteus.

| Feature | Where it lives | Applicability to Proteus | Notes |
|---|---|---|---|
| **MCP (Model Context Protocol) servers** | Claude Code | **High** | A protein-design MCP server (e.g. an external PDB / UniProt / Foundry MCP) could be subscribed instead of being hard-coded in `internal/tools/knowledge/`. Lets third parties add tools without forking. |
| **Subagent / `Agent` tool** | Claude Code | **High** | An "explore the corpus / explore the structure / draft three candidate plans" subagent could fan out work without polluting the main turn's context — like a more general version of corpus `map`. Cleanly extends what `internal/agent/loop.go` already does. |
| **Hooks (pre/post-tool, on-stop, …)** | Claude Code (`.claude/settings`) | **High** | A pre-tool-call hook is exactly what `internal/safety/guard.go` is starting to do for biosecurity checks. Generalising it to a hook system (any user-provided hook script can intercept) would let users add lab-specific guards (cost ceilings, organism allowlists) without code changes. |
| **Auto context compaction** | Claude Code | **High** | Plan-from-target sessions accumulate big tool outputs (UniProt JSON, PMC abstracts, BindCraft logs). A summariser pass when the context approaches limit would help. There is no compaction in `internal/agent/loop.go` today. |
| **Plan mode / `ExitPlanMode`** | Claude Code | **Medium** | Distinct from Proteus's `plan.create` (which produces a `DesignPlan`). A "design-plan-as-proposal-with-approve-gate" exists already (`internal/tui/plan_test.go::TestAppPlanApprove`). Worth aligning vocabulary so users coming from Claude Code aren't confused. |
| **Image input (multimodal turn)** | Claude Code, Codex (preview) | **High** | A user could drop a PNG of a binding pocket and ask "redesign around this". The provider layer in `internal/llm/` is text-only today. Inline structure graphics is the *output* side of this (v0.5 SP-B); image *input* is a separate gap. |
| **Background tool execution from chat** | Claude Code (`run_in_background`) | **Medium** | Partially covered: `jobs.*` tools already enqueue async work and surface it in the jobs panel. The remaining gap is a frictionless "fire and forget — notify me when done" UX in the chat itself. |
| **Memory file auto-loaded per session** | Claude Code (`CLAUDE.md`), Codex (`AGENTS.md`) | **Medium** | Project-specific facts ("this lab targets membrane proteins, prefers BindCraft, hates pyrosetta-licence prompts") would be useful per-workspace. `internal/skills/loader.go` loads skills; an auto-loaded `PROTEUS.md` per workspace would mirror Claude Code's pattern. |
| **Diff-streaming / structured patch tool** | Codex, OpenCode | **Low** | Proteus rarely edits code — `fs.edit` is sufficient for the occasional config/script tweak. |
| **Repo-wide symbol search** | OpenCode | **Low** | Not the domain. |
| **LSP integration** | Codex (via IDE), Claude Code IDE extension | **Low** | Terminal-first project; users open structure files in PyMOL / ChimeraX, not an LSP. |
| **Worktree / isolated-workspace tooling** | Claude Code + superpowers | **Medium** | Already used externally by the developer's workflow (this very review is in a worktree); a built-in `/worktree` slash command isn't necessary but would polish multi-experiment workflows. |
| **Permission tiers for shell** | Codex | **Medium** | `internal/tools/fs.go::bashAllowlist` + `bashDenyTokens` already implement a basic tier. Codex's graduated model (read-only → sandboxed → full) is more nuanced and worth borrowing. |
| **Slash-command argument hinting** | Claude Code | **Low** | Proteus's slash menu (`internal/tui/slashmenu.go`) is already healthy. |
| **Replay / session resume from disk** | Claude Code | **Medium** | v0.5 SP-F replay is in flight — this gap is already being closed. |

### 1.3 Things **Proteus has that the others don't**

This is just as important as the gap list, because it shows where Proteus's
focus pays off:

- A real **wet-lab integration** with webhook callback (Adaptyv) — none of the
  generalist agents touch lab automation.
- A **structured `DesignPlan`** as a first-class object, not just chat text,
  with approve/cancel/edit verbs and a persistence model.
- **Domain-tool installer with reproducible recipes** (`internal/backends/local/tools.toml`).
- A **per-project literature corpus** indexed by Bleve, with map/reduce verbs
  over its contents.
- **Embedded safety guard** with a curated restricted-target table (v0.5 SP-E).
- **Cost tracking with a hard soft-limit** wired into the status bar.
- **Backend abstraction over local + cloud** for the same tool surface.

### 1.4 Recommended adopts

In priority order (independent of v0.5's in-flight work):

1. **Hook system generalising the safety guard.** SP-E is implementing a single
   guard kind (restricted-target) with a TOML table. Re-shape it as a generic
   pre-tool-call hook protocol so users can register additional guards
   (cost-ceiling, organism allowlist, custom audit) without code changes.
   This is a thin generalisation of work that's already happening.
2. **MCP client.** Once the safety guard is a hook, an MCP client that turns a
   remote MCP server into a set of tools (registered via the existing
   `tools.Registry`) is the next-cheapest big win. It opens Proteus to a
   third-party tool ecosystem without forking.
3. **Subagent tool.** An `agent.spawn` (or similar) that runs a small,
   bounded inner agent loop with its own tool selection — modeled on Claude
   Code's `Agent` tool. The job manager (`internal/jobs/manager.go`) already
   has the right concurrency model.
4. **Auto-compaction in the agent loop.** When the running token count crosses
   a configurable threshold (e.g. 80% of the model's context window), emit a
   "summarise-and-replace" pass that condenses old tool outputs. The cost
   tracker already counts tokens — wire to a compactor.
5. **Image input.** Add an optional `image` content kind to
   `internal/llm/provider.go::Message`; gate it behind provider capability.
6. **Per-workspace `PROTEUS.md` auto-load.** Reuse the skills loader; treat
   `<workspace>/PROTEUS.md` as an implicit skill that always loads first.

---

## 2. Code quality findings

### 2.1 Automated checks (all clean)

| Check | Command | Result |
|---|---|---|
| Build | `go build ./...` | clean |
| Vet | `go vet ./...` | clean |
| Format | `gofmt -l internal/ pkg/ cmd/` | clean |
| Static analysis | `staticcheck ./...` | **0 findings across 22 packages** |
| Dead code (incl. tests) | `deadcode -test ./...` | **0 findings** |
| Race detector | `go test -race ./...` | clean, no races |

This is exceptional. For a codebase of this size, zero staticcheck findings
and zero truly-dead code is the strongest possible health signal short of
formal verification.

### 2.2 Specific findings (low-severity)

The codebase has no Critical or Important issues. The following are
observations / Minor-grade nudges.

#### 2.2.1 ESMFold uses `http.DefaultClient` (`internal/tools/fold/esmfold.go:93`)

```go
resp, err := http.DefaultClient.Do(req)
```

The request is built with `http.NewRequestWithContext(ctx, …)`, so context
cancellation works — that is the meaningful safety guarantee. However:

- `http.DefaultClient` has no per-client timeout. If the caller passes a
  background context with no deadline, a stuck connection can hang the agent
  turn indefinitely.
- The other HTTP-using packages (`internal/tools/knowledge/client.go`,
  `internal/tools/lab/adaptyv.go`, `internal/backends/modal/client.go`) all
  use a dedicated `*http.Client` with an explicit timeout. ESMFold is the odd
  one out.

**Recommendation:** introduce a package-level `*http.Client` with a generous
timeout (folds can run minutes — `10 * time.Minute` is reasonable) and use it.
This is the one safe code change in this review; it lives in a stable island
(`internal/tools/fold/`, not touched by any v0.5 sub-project).

#### 2.2.2 Functions reachable only from tests

`deadcode ./...` (without `-test`) lists 10 functions that production code does
not call but tests do:

| File:line | Symbol | Notes |
|---|---|---|
| `internal/backends/local/doctor.go:38` | `Report.toolInstalled` | Test convenience accessor. Acceptable. |
| `internal/backends/local/installer.go:69` | `Installer.Install` | Production uses `InstallLogged`. The plain `Install` is kept for tests; consider folding it into `InstallLogged(_, io.Discard)` instead of a separate exported method. |
| `internal/llm/anthropic.go:23` | `newAnthropicProviderWithBaseURL` | Test helper for base-URL override. Acceptable. |
| `internal/tools/lab/webhook.go:139` | `signBody` | Test-side HMAC signer; used by `TestWebhookValidSignatureAccepted`. Could move to a `_test.go` file. |
| `internal/tui/app.go:989` | `loadConfigForTest` | Should live in a `_test.go` file. |
| `internal/tui/commandbar.go:55` | `commandBarModel.setFocused` | Unused setter. |
| `internal/tui/spinner.go:61` | `thinkingModel.active` | Unused predicate. |
| `internal/tui/statusbar.go:27` | `statusBarModel.setProject` | Unused setter. |
| `internal/tui/statusbar.go:31` | `statusBarModel.setContextPercent` | Unused setter. |
| `pkg/proteinio/fasta.go:65` | `WriteFASTA` | Intentional library export (documented in `docs/VERIFICATION.md`). |

**Most TUI entries here are v0.5-active files** — they will be touched by SP-B
(inline graphics) or future polish work. Do not change them in this branch.
The non-TUI entries are stylistically improvable but not bugs.

#### 2.2.3 `internal/store/jobs.go:41` — `QueryRow` without `defer rows.Close()`

`QueryRow` doesn't return a `*sql.Rows`, so it doesn't need `Close` — this is
correct. Listed only because the grep heuristic flagged it; no action.

#### 2.2.4 `context.Background()` use sites

All four production-code `context.Background()` calls are justified roots:

- `internal/tui/app.go:173` — the webhook listener; lifetime is the app.
- `internal/tui/app.go:521` — the agent-turn root (cancel propagates to tools).
- `internal/tools/lab/webhook.go:59` — graceful-shutdown timeout root.
- `internal/jobs/manager.go:76` — job-runner context, manager-owned.

No action.

### 2.3 Test-coverage observations

Test density varies meaningfully across packages. This is information, not a
defect list.

| Package | Test funcs | Source files | Notes |
|---|---:|---:|---|
| `internal/tui` | 110 | 19 | Very thorough. |
| `internal/backends/local` | 44 | 9 | Excellent — installer / adapters covered. |
| `internal/tools/knowledge` | 37 | 12 | Good — most knowledge tools have a focused test. |
| `internal/config` | 23 | 2 | Excellent. |
| `internal/llm` | 20 | 5 | Solid. |
| `internal/store` | 19 | 8 | Good. |
| `internal/tools/lab` | 17 | 4 | Good. |
| `internal/tools` | 15 | 3 | Good. |
| `internal/agent` | 9 | 3 | **Low for a critical path.** The loop has cancellation + smoke coverage but few edge-case tests. |
| `internal/backends/modal` | 5 | 2 | Adequate. |
| `internal/tools/fold` | 7 | 4 | Adequate. |
| `internal/skills` | 3 | 1 | Adequate. |
| `internal/tools/plan` | 3 | 1 | Adequate. |
| `internal/tools/jobs` | 4 | 4 | **Borderline** — four sub-tools, one test file. |
| **`internal/tools/design`** | **6** | **8** | **Low**: per-design-tool wrappers (`bindcraft.go`, `chai2.go`, `rfantibody.go`, `rfdiffusion2.go`, `ligandmpnn.go`, etc.) share a small set of generic tests. A direct invocation test per wrapper would be worth ~1 hour of effort. |
| `internal/domain` | 6 | 1 | Excellent. |
| `pkg/proteinio` | 6 | 3 | Adequate. |
| `internal/tools/score` | 6 | 3 | Adequate. |
| `internal/version` | 1 | 1 | Trivial, fine. |

**Lowest-priority gap:** `internal/tools/design/` per-wrapper tests. The
generic `design_test.go::TestDesignToolsImplementToolInterface` confirms the
interface, but a regression introduced in a single wrapper would not be
caught.

---

## 3. Performance opportunities

The codebase is small enough and its hot paths simple enough that there are no
obvious algorithmic wins. The notable patterns:

- **Bounded fan-out is already correct.** `internal/tools/knowledge/corpus.go:316-336`
  uses a `WaitGroup` + buffered-channel semaphore + child-context cancel-on-first-error
  pattern. Textbook. No change.
- **Shared HTTP client in knowledge tools** (`internal/tools/knowledge/client.go:22`)
  — already pooled correctly. The same pattern is missing only in ESMFold (§2.2.1).
- **Streaming providers** (`internal/llm/openai.go:118`, `anthropic.go:118`) run
  their reader on a goroutine and close the channel on EOF — standard.
- **SQLite store** uses parameterised queries and proper `defer rows.Close()`;
  it does not batch inserts. For Proteus's workload (low write rate — a handful
  of designs per session, not thousands) batching would be premature.
- **TUI rendering** depends on Bubble Tea's diffing and is currently fast
  enough for visible panels; SP-A theming and SP-B inline graphics will be the
  natural place to revisit rendering perf if it becomes a felt issue.

No performance change is justified in this branch.

---

## 4. Dead code

There is no truly dead code (`deadcode -test ./...` returns 0). The 10
test-only reachable functions are listed in §2.2.2; none should be removed
because tests would break. The `chore: remove dead code` discipline visible in
the git history (`e089586`) is being maintained.

---

## 5. Prioritised recommendations

### Apply in this branch (safe, isolated, stable-island only)

- **R1.** `internal/tools/fold/esmfold.go` — replace `http.DefaultClient.Do(req)`
  with a package-level `*http.Client` (10-minute timeout). One file, no
  semantic change beyond timeout bound. See §2.2.1.

### Apply soon (small, stable-island, not in this branch unless requested)

- **R2.** Move `loadConfigForTest` (`internal/tui/app.go:989`) and `signBody`
  (`internal/tools/lab/webhook.go:139`) into companion `_test.go` files —
  trivially eliminates two of the "test-only in production source" entries.
  *Defer:* `app.go` is v0.5-active; `webhook.go` is borderline. Do this after
  v0.5 lands to avoid conflicts.

### Pursue as features (v0.5+ or v0.6)

In priority order, repeating §1.4:

- **R3.** Generalise the v0.5 safety guard into a hook system.
- **R4.** Add an MCP client behind `tools.Registry`.
- **R5.** Add a subagent tool that runs a bounded inner loop.
- **R6.** Auto-compact the conversation in `internal/agent/loop.go` when
  approaching a model's context limit.
- **R7.** Multimodal (image) input on providers that support it.
- **R8.** Per-workspace `PROTEUS.md` auto-load.

### Test-coverage improvements (any time)

- **R9.** Per-wrapper tests in `internal/tools/design/` — one direct invocation
  test per design tool wrapper.
- **R10.** Edge-case tests in `internal/agent/loop.go` — context-cancellation
  variants, malformed tool-call payloads, provider-error propagation.

---

## Appendix A — methodology

Tools used in this review (all run in worktree `/home/gonjim/Projects/proteus-review`
on branch `chore/code-review-2026-05-20`, base `master @ 075d1d3`):

```
go build ./...
go vet ./...
gofmt -l internal/ pkg/ cmd/
go test -race ./...
staticcheck ./...
deadcode ./...
deadcode -test ./...
```

The hand review focused on the stable islands (packages not currently touched
by v0.5 SP-A through SP-G branches): `pkg/proteinio/`,
`internal/backends/local/`, `internal/backends/modal/`, `internal/llm/`,
`internal/jobs/`, `internal/store/` (except `sessions.go`),
`internal/tools/{score,fold,design,plan,jobs}/`, `internal/tools/fs*.go`,
`internal/domain`, `internal/version`, `internal/config`.
