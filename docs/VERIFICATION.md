# Acceptance Criteria Verification Record

This is the honest companion to the `v0.3.0` / `v0.4.0` release tags. It records
the verification status of every acceptance criterion in `docs/SPECS.md` §20 for
milestones v0.1–v0.4. Where a criterion cannot be checked in the current
environment, this document says so and says what is required.

_Last updated: 2026-05-19. Test baseline: `go test ./...` — 22 packages, all
`ok`; `go vet ./...` clean._

## EstimatedDuration calibration — ProteinMPNN (TODO — manual benchmark)

The v0.6 jobs.status enrichment (Bug 3) surfaces `elapsed` and `estimated`
to the agent. The current `EstimatedDuration` for `design.proteinmpnn`
is 30 min (`internal/tools/design/design.go:60`); a real CPU-mode
benchmark on 129 aa × 3 designs should be measured outside fova and
recorded here:

  - command: …
  - measured wall-clock: …
  - decision: keep EstimatedDuration at 30 min / lower to N min

This footnote satisfies Bug 3 AC4 (folded former Bug 8). The calibration
itself cannot run in this environment (no ProteinMPNN install). Future
tuning should backfill the bullets above after running the benchmark on a
CPU-only host with `CUDA_VISIBLE_DEVICES=` set and `num_designs=3`.

## Status legend

| Status | Meaning |
|--------|---------|
| `auto` | Covered by an automated test in the repo; the test passes. |
| `needs-run` | Needs a real run (install, tool execution, or live agent session) on a provisioned machine; not executable in this analysis. The supporting mechanism is auto-tested where noted. |
| `blocked-gpu` | Needs a working GPU build; blocked by the GB10 `sm_121` PyTorch incompatibility. |
| `blocked-account` | Needs an external account (Adaptyv Foundry, Modal). |
| `needs-eyes` | Needs a human to view TUI or graphical output. |
| `deviation` | The §20 text is stale; the criterion is met via a different surface (see Notes). |

## v0.1 — "Hello, sequence"

| # | Criterion | Status | Evidence | To verify |
|---|-----------|--------|----------|-----------|
| 1 | `fova` launches a TUI with a chat pane | `auto` | `cmd/fova/main_test.go` (command wiring), `internal/tui/chat_test.go`, `internal/tui/app_test.go` (`TestAppWideLayoutShowsPanels`) | — |
| 2 | "fold MAQ…" → agent calls `fold.esmfold`, returns a PDB path + pLDDT | `auto` | `internal/tools/fold/esmfold_test.go` (`TestEsmfoldExecute`, `TestParsePLDDT`) | — |
| 3 | Switching to a local Ollama model via `/model` works | `auto` | `internal/llm/modelregistry_test.go` (`TestModelRegistryHasOllamaModel`, `TestModelRegistrySetModel`), `internal/llm/openai_test.go` | — |
| 4 | `Ctrl+C` cancels mid-tool-call cleanly | `auto` | `internal/agent/loop_test.go` (`TestLoopCancellationMidTool`), `internal/tui/app_test.go` (`TestAppEscCancelsRunningTurn`, `TestAppCtrlCKeepsRunningUntilTurnEnds`) | — |
| 5 | Smoke test passes in CI | `auto` | `internal/agent/smoke_test.go` (`TestSmoke_FoldAndScore`); `.github/workflows/ci.yml` runs `go test -race ./...` | — |

**v0.1 totals:** 5 `auto`.

## v0.2 — "Real designs"

| # | Criterion | Status | Evidence | To verify |
|---|-----------|--------|----------|-----------|
| 1 | `doctor` runs on a fresh machine, lists everything "not installed" | `deviation` | Logic auto-tested: `internal/backends/local/doctor_test.go` (`TestDoctorReportsToolStatus`), `internal/tui/setup_test.go` (`TestCmdDoctorPostsReport`) | Surface only — now the `/doctor` TUI command, not a CLI subcommand (see Notes) |
| 2 | `install ipsae` succeeds in < 60s on broadband | `needs-run` | Installer mechanism auto-tested: `internal/backends/local/installer_test.go` (`TestInstallerInstallRunsStepsAndWritesLock`) | Run `/install ipsae` on a networked machine |
| 3 | `install proteinmpnn` succeeds; `doctor` shows it installed | `needs-run` | Installer + doctor mechanism auto-tested (as #2, plus `TestDoctorReportsToolStatus`) | Run `/install proteinmpnn` on a networked machine |
| 4 | `install bindcraft` succeeds end-to-end (PyRosetta + AF weights); failure names the failing step | `needs-run` | Failure-naming auto-tested: `installer_test.go` (`TestInstallerInstallFailureNamesStep`) | Run `/install bindcraft` on a networked machine |
| 5 | `modal deploy` deploys Modal functions | `blocked-account` | Client + embedded functions auto-tested: `internal/backends/modal/client_test.go`, `functions_test.go` | A Modal account + `modal` CLI |
| 6 | Local-backend design run: detect → install-check → run → `score.ipsae` → ≥10 filtered designs | `blocked-gpu` | Logic auto-tested: `internal/backends/local/adapter_bindcraft_test.go`, `adapter_proteinmpnn_test.go`, `internal/tools/score/ipsae_test.go`, `score_test.go` (`TestFilterShortlistsAndRanksByIPSAE`) | A real BindCraft run on an `sm_121`-capable PyTorch build (see Notes) |
| 7 | Designs persist, survive restart, show in the designs panel with ipSAE | `auto` | `internal/store/designs_test.go` (`TestDesignInsertGet`, `TestDesignListByProject`), `internal/store/persistence_test.go` (`TestDataSurvivesReopen`), `internal/tui/designs_test.go` | — |
| 8 | Jobs panel shows running/queued/completed with ETAs | `auto` | `internal/jobs/manager_test.go`, `internal/tui/jobs_test.go` | — |
| 9 | Cancellation of a running tool works (best-effort) | `auto` | `internal/jobs/manager_test.go` (`TestManagerCancel`) | — |
| 10 | Same design task → same output schema regardless of `compute_backend` | `auto` | `internal/backends/backend_test.go`, `internal/backends/local/adapter_test.go` (`TestRunDesignNoAdapterMessageIsClear`) | — |

**v0.2 totals:** 4 `auto`, 3 `needs-run`, 1 `deviation`, 1 `blocked-account`, 1 `blocked-gpu`.

## v0.3 — "Plan from target"

| # | Criterion | Status | Evidence | To verify |
|---|-----------|--------|----------|-----------|
| 1 | Full chain: UniProt → PDB → Europe PMC → corpus → map → editable `DesignPlan` | `needs-run` | Each step auto-tested in isolation: `internal/tools/knowledge/uniprot_test.go`, `pdb_test.go`, `europepmc_test.go`, `corpus_test.go`, `internal/tools/plan/plan_test.go` (`TestPlanCreatePersistsPlan`) | A live agent session with an LLM + the live knowledge APIs |
| 2 | Plan shows target, application, method, thresholds, cost, evidence papers w/ DOIs | `auto` | `internal/tools/plan/plan_test.go`, `internal/store/plans_test.go` (`TestPlanInsertGet`), `internal/tui/plan_test.go` (`TestAppPlanCommandShowsPersistedPlan`) | — |
| 3 | User can approve / edit / cancel the plan | `auto` | `internal/tui/plan_test.go` (`TestAppPlanApprove`, `TestAppPlanCancel`), `internal/store/plans_test.go` (`TestSetPlanApproved`); edit = re-run `plan.create` | — |
| 4 | Corpus persists per project; `corpus.grep` consistent with `corpus.search` | `auto` | `internal/tools/knowledge/corpus_test.go` (`TestCorpusSearchAndGrepConsistency`), `internal/store/corpus_test.go` | — |
| 5 | OpenAI and Google providers work via `/model` | `auto` | `internal/llm/openai_test.go` (`TestOpenAIChat`), `internal/llm/google_test.go` (`TestNewGoogleProviderName`), `internal/llm/modelregistry_test.go` (`TestModelRegistryHasGoogleProvider`) | — |

**v0.3 totals:** 4 `auto`, 1 `needs-run`.

## v0.4 — "Closing the loop"

| # | Criterion | Status | Evidence | To verify |
|---|-----------|--------|----------|-----------|
| 1 | `auth adaptyv` stores the token in the keychain | `deviation` | Token-storage logic auto-tested: `internal/tools/lab/auth_test.go` (`TestTokenFromEnv`) | Surface only — now the `/auth` TUI command (see Notes) |
| 2 | Agent calls `lab.targets_search`, lists Adaptyv targets | `auto` | `internal/tools/lab/tools_test.go` (`TestLabToolTargetsSearch`) — against a stub Adaptyv server | — |
| 3 | Submission flow runs end-to-end against Adaptyv staging; modal appears; experiment ID persists | `blocked-account` | Submit + persistence + modal auto-tested: `tools_test.go` (`TestLabToolSubmitPersistsExperiment`, `TestLabToolSubmitRequiresConfirmation`), `internal/tui/labmodal_test.go`, `internal/tui/app_test.go` (`TestAppSubmitConfirmShowsRichModal`) | An Adaptyv Foundry staging account |
| 4 | Webhook receiver accepts a test POST; the wet-lab panel updates | `auto` | `internal/tools/lab/webhook_test.go` (`TestWebhookValidSignatureAccepted`, `TestWebhookRouteServedViaMux`), `internal/store/webhook_events_test.go`, `internal/tui/lab_test.go` | — |
| 5 | `install rfantibody` and `install ligandmpnn` succeed | `needs-run` | Installer mechanism auto-tested (`installer_test.go`); recipes present in `tools.toml` | Run the installs on a networked machine |
| 6 | Antibody track: design VHHs against PDB 6M0J via `design.rfantibody`, scored with `score.ipsae` | `blocked-gpu` | Tool + schema auto-tested: `internal/tools/design/design_test.go` (`TestAntibodyEnzymeToolMetadata`, `TestDesignToolsImplementToolInterface`) | A real RFantibody run on an `sm_121`-capable PyTorch build |
| 7 | Enzyme track: `design.rfdiffusion2` + `design.ligandmpnn` with `fold.chai1` validator | `blocked-gpu` | Tool + schema auto-tested: `design_test.go`, `internal/tools/fold/foldjob_test.go` | A real enzyme-design run on an `sm_121`-capable PyTorch build |
| 8 | TUI renders the §10.7 modern design (no frame, bordered input, section rules, status footer + context meter) | `needs-eyes` | Token + component logic auto-tested: `internal/tui/theme_test.go`, `internal/tui/statusbar_test.go`, `internal/tui/commandbar_test.go` | A human viewing the running TUI |
| 9 | `/` opens slash-command autocomplete; ↑/↓ select, Tab completes, Esc dismisses | `auto` | `internal/tui/slashmenu_test.go` (`TestSlashMenuVisibleWhenMatches`, `TestSlashMenuNextPrevClamp`, `TestSlashMenuViewContainsCommand`), `internal/tui/commands_test.go` (`TestMatchCommands`) | — |
| 10 | Running turn shows an animated thinking indicator (elapsed + esc hint); tool calls render as `⏺`/`⎿` traces | `auto` | `internal/tui/spinner_test.go` (`TestThinkingViewAfterStart`, `TestThinkingTickAdvancesFrame`), `internal/tui/app_test.go` (`TestAppToolBusMessagesRenderInChat`) | — |
| 11 | `go test ./...` passes and `go vet ./...` is clean after the redesign | `auto` | This baseline: 22 packages `ok`, `go vet` clean; `.github/workflows/ci.yml` | — |

**v0.4 totals:** 5 `auto`, 1 `deviation`, 1 `needs-run`, 1 `blocked-account`, 2 `blocked-gpu`, 1 `needs-eyes`.

## Summary

| Milestone | Criteria | `auto` | `needs-run` | `blocked-gpu` | `blocked-account` | `needs-eyes` | `deviation` |
|-----------|---------:|-------:|------------:|--------------:|------------------:|-------------:|------------:|
| v0.1 | 5 | 5 | — | — | — | — | — |
| v0.2 | 10 | 4 | 3 | 1 | 1 | — | 1 |
| v0.3 | 5 | 4 | 1 | — | — | — | — |
| v0.4 | 11 | 5 | 1 | 2 | 1 | 1 | 1 |
| **Total** | **31** | **18** | **5** | **3** | **2** | **1** | **2** |

18 of 31 criteria are verified by automated tests. The remaining 13 cannot be
verified in this environment: 5 need a real install/run, 3 need GPU hardware
with a compatible PyTorch build, 2 need an external account, 1 needs visual
inspection, and 2 are surface deviations from the spec text.

## Notes

### v0.2 criterion 6 was not functional at the `v0.2.0` tag

At the `v0.2.0` tag, the `design.*` → backend → real tool path was stubbed:
criterion 6 ("agent runs BindCraft, scores, returns designs") was not
functional. The genuine `ToolAdapter` wiring (ProteinMPNN, RFdiffusion,
BindCraft — sub-plans SP1–3) landed afterward. The criterion's *logic* is now
covered by tests; a real end-to-end GPU run remains `blocked-gpu`.

### CLI → TUI deviation

SPECS §20 lists `fova install <tool>`, `fova list tools`, `fova doctor`
(v0.2) and `fova auth adaptyv` (v0.4) as CLI subcommands. The `fova`
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
