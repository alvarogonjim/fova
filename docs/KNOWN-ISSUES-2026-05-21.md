# fova — Known Issues (validation session, 2026-05-21)

Bugs and gaps found while validating the full design pipeline end-to-end
with a local vLLM model (`Qwen3.5-35B-A3B-FP8`) on the GB10, target:
de novo PD-L1 binders.

Suggested branch for the fixes: `fix/validation-issues`.

The pipeline itself is sound — tool calls, knowledge stack, jobs, tool
installs, `plan.create`, and the human-in-the-loop approval gate all
work. The items below are real defects surfaced along the way.

---

## Confirmed code bugs

### 1. `/plan approve` is a dead end — design job never starts (HIGH — blocks the pipeline)
- **Where:** `internal/tui/app.go:785-804` (`/plan` handler, `approve` case)
- **Severity:** HIGH — the pipeline cannot proceed past planning
- **Symptom:** `/plan approve` loads the plan, calls
  `store.SetPlanApproved(p.ID)`, prints `"plan … approved"`, and returns.
  No design job is submitted and the agent is not re-invoked. The agent
  tells the user "/plan approve to start the design job", but the
  `Approved` flag is consumed by nothing — confirmed: no design tool and
  no agent code path reads it.
- **Fix:** after `SetPlanApproved`, hand control back to the agent —
  start an agent turn with a synthetic instruction (e.g. "design plan
  <id> is approved — submit the design job(s) per the plan"). The agent
  then orchestrates scaffold → design → predict → rank through its tools.
  Do *not* have `/plan approve` submit a single job directly: the
  pipeline is multi-stage and the agent owns orchestration.
- **Workaround:** after `/plan approve`, send a normal chat message —
  "the plan is approved, submit the BoltzGen design job now" — which
  starts an agent turn.

### 2. `knowledge.corpus_read` leaks a raw SQL error
- **Where:** `internal/tools/knowledge/corpus.go:795-806` (`readCorpus`)
- **Severity:** medium — makes the agent loop
- **Symptom:** `corpus_read` with a `paper_id` not in the corpus returns
  the raw `sql: no rows in result set` straight to the agent. The message
  names nothing and suggests no recovery, so the agent retries with more
  guessed IDs and loops.
- **Fix:** in `readCorpus`, detect `errors.Is(err, sql.ErrNoRows)` from
  `GetCorpusPaper` and return a domain error, e.g.
  `knowledge.corpus_read: paper %q is not in the corpus — use an id from a
  knowledge.corpus_search / corpus_grep result, or add it with
  knowledge.corpus_add`. Match the style of `plan.create`'s "not in
  corpus" error, which is well-worded — the agent recovered from that one.

### 3. `corpus_map` jobs never write a job log
- **Where:** `internal/tools/knowledge/corpus.go:625` (the `Spec.Run` closure)
- **Severity:** medium — this is the "no logs written" report
- **Symptom:** `submitMapJob` builds `Run: func(ctx, progress, _ io.Writer)`
  — the job-log writer is bound to `_` and discarded. `runCorpusMap` is
  never given a writer, so a `corpus_map` job's `<logDir>/<jobID>.log` is
  empty even when per-job logging is enabled.
- **Fix:** thread the `io.Writer` into `runCorpusMap` and log per-paper
  progress/errors ("paper 12/50 <title> — mapped" / failures), so the job
  log is a useful trace.

### 4. `corpus_map` duration estimate is unrealistic
- **Where:** `internal/tools/knowledge/corpus.go:586-588` (`EstimatedDuration`)
- **Severity:** low — misleads the user
- **Symptom:** returns a flat `2 * time.Minute` (comment assumes ~2s/paper).
  `corpus_map` is an LLM fan-out — one model call per paper. On a slow
  local model a ~50-paper map ran 11+ min at ~50% (projected ~22 min)
  while `jobs.status` kept showing `estimated=2m0s`.
- **Fix:** scale the estimate by `ceil(papers / concurrency)` and a
  realistic per-call latency; do not hard-code a cloud-model assumption.

### 5. No PDB search — `knowledge.pdb` is ID-lookup only
- **Where:** `internal/tools/knowledge/pdb.go` (`knowledge.pdb`)
- **Severity:** medium — pipeline reliability
- **Symptom:** `knowledge.pdb` only does "look up an RCSB entry **by ID**"
  (`data.rcsb.org/.../core/entry`). There is no search-by-target-name, so
  the agent guesses PDB IDs — it claimed `6Q3B` "is a known PD-L1
  structure" (it is CDK2) and fetched several unrelated entries.
- **Fix:** add a `knowledge.pdb_search` tool backed by the RCSB search API
  (`search.rcsb.org`) so the agent can resolve "PD-L1" → candidate IDs
  instead of hallucinating them.

---

### 6. `design.boltzgen` tool is not registered — BoltzGen plans can't run (HIGH — blocks the pipeline)
- **Where:** missing `internal/tools/design/boltzgen.go`; not registered
  in `cmd/fova/main.go:208-214`.
- **Severity:** HIGH — an approved BoltzGen plan cannot be executed.
- **Symptom:** `internal/tools/plan/compat.go` defines
  `MethodBoltzGen = "BoltzGen"` and allows it for binder design;
  `internal/backends/local/adapter_boltzgen.go` is the backend adapter;
  `install:boltzgen` works. But there is **no `design.boltzgen` agent
  tool** — `cmd/fova/main.go` registers only bindcraft, rfdiffusion,
  proteinmpnn, rfantibody, chai2, rfdiffusion2, ligandmpnn. So when an
  approved plan says "Method: BoltzGen", the agent has no tool to call
  and falls back to `design.bindcraft` — which fail-fasts on
  aarch64/Grace (the GB10), the exact reason the plan chose BoltzGen.
- **Inconsistency:** `plan.create` validates and accepts a method
  (`BoltzGen`) that has no executable `design.*` tool. Plan validation
  and the tool registry disagree.
- **Fix (3 parts — all required):**
  1. Add `internal/tools/design/boltzgen.go` with `NewBoltzGenTool(...)`
     → `design.boltzgen`, wired to the boltzgen backend recipe (mirror
     the other `design.*` tools).
  2. Register it in `cmd/fova/main.go` alongside the others.
  3. Update `internal/agent/prompts/system.md` and
     `internal/skills/builtin/design-binder.md` to prefer BoltzGen over
     BindCraft (BindCraft is unavailable on aarch64). **Editing the
     prompt/skill alone is not enough** — without parts 1-2 the agent
     still has no tool to call.
- **Also consider:** `plan.create` should reject a method whose
  `design.*` tool is not registered, so this gap fails loudly at plan
  time instead of silently at execution.

---

## Needs investigation

### 7. Confirm per-job logging is wired at startup
- **Where:** `internal/jobs/manager.go:50-55` (`SetLogDir`); check
  `cmd/fova` / TUI setup for the call.
- **Symptom:** `Manager` only writes `<logDir>/<jobID>.log` when `logDir`
  is non-empty. If `SetLogDir` is never called, *no* job logs anywhere
  (independent of bug #2, which would still leave `corpus_map` logless).
- **Action:** verify `SetLogDir` is called with a real directory at boot;
  if not, wire it.

---

## UX issues

### 8. Long-running jobs look identical to stuck/failed jobs
- **Symptom:** the jobs panel shows a spinner + progress bar but no
  last-update timestamp or rate, so a slow job (`corpus_map` crawling
  20%→50% over ~5 min) is indistinguishable from a hung one. Prompted the
  "I think those jobs failed" report.
- **Fix idea:** show time-since-last-progress, or flag a job as "stalled"
  if progress hasn't moved within some window.

### 9. No guard against duplicate concurrent `corpus_map` jobs
- **Symptom:** the agent launched `corpus_map` twice over overlapping
  paper sets (`j_9b257df9`, `j_fdeca346`); both fan out LLM calls and
  contend with each other and the agent loop.
- **Fix idea:** dedupe or warn when an equivalent `corpus_map` job is
  already running.

---

## Architectural note — local-LLM contention

`corpus_map` issues one LLM call per paper against the *same* endpoint the
agent loop uses. With a single local vLLM server, background corpus jobs
and the interactive agent starve each other — observed as multi-minute
"Thinking…" pauses while two `corpus_map` jobs ran.

Consideration: when `provider` is a local endpoint, cap `corpus_map`
concurrency low (or document running corpus-heavy steps against a
separate model).

---

## Model / orchestration observations (not code bugs)

Context for the branch — these are `A3B`-model behavior issues, mitigated
by a stronger orchestration model, not by code:

- Over-scoped the plan: 100 designs when the user asked for 5 (corrected
  on re-plan to 5/5/local).
- Switched `compute` to `modal`, silently overriding `config.toml`
  `compute_backend = "local"`.
- Hallucinated factual identifiers (PDB IDs, corpus paper IDs).
- Launched `corpus_map` twice; over-mapped ~50 papers for a 5-binder task.

Possible code-side mitigation: `plan.create` (or the TUI) could flag when
a plan's design count diverges sharply from the user's request — though
the tool does not see the natural-language ask, so this is non-trivial.

---

## Low-priority / future

- CI workflows use `actions/checkout@v4` / `actions/setup-go@v5` on
  Node 20 — deprecated June 2026. Bump when convenient.
- goreleaser `brews:` is deprecated in favor of `homebrew_casks`; migrate
  when Homebrew auto-publish is re-enabled (needs `HOMEBREW_TAP_TOKEN`).
- `internal/backends/local/installer.go:82,85` uses Unix-only
  `syscall.Kill` / `SysProcAttr.Setpgid` with no build tags. Harmless
  today (Windows was dropped from the release matrix); only matters if
  Windows support is ever wanted.

---

## Already fixed on `main` (reference — do not redo)

- CI `gofmt` failure (3 files reformatted).
- Release pipeline: dropped the Windows target (cross-build hit the
  `syscall` issue above), added `replace_existing_artifacts: true`,
  removed the `brews:` block.
- License metadata: `MIT` / `Apache` → `AGPL-3.0-or-later` in
  `.goreleaser.yaml`, `homebrew-tap/Formula/fova.rb`, `docs/DESIGN.md`
  badge, `docs/SPECS.md`.
- Duplicate startup header / mascot block removed.
- `/reload` now actually reloads `models.toml`.
