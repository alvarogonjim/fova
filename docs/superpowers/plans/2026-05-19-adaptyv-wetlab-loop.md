# Adaptyv Wet-Lab Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the Adaptyv Foundry integration — HTTP client, webhook receiver, five `lab.*` agent tools, the wet-lab TUI, and two skills — per `docs/superpowers/specs/2026-05-19-adaptyv-wetlab-loop-design.md`.

**Architecture:** A new `internal/tools/lab` package holds the client, token storage, the agent tools, and the webhook receiver. The client uses hand-written request/response types (no codegen). The TUI gains a wet-lab panel and a rich submit modal as self-contained components; `app.go` and `main.go` wire everything in a final integration pass. The existing `domain.Experiment` types and `store.InsertExperiment`/`GetExperiment`/`ListExperiments` are reused as-is.

**Tech Stack:** Go. New dependency: `github.com/99designs/keyring`. Tests are `go test`, offline and deterministic — `httptest.Server` for the client, signed-payload POSTs for the webhook, stub clients for the tools.

---

## Execution model

- **Phase A — Foundation (sequential).** Task A: the client + token storage. Phase B depends on it.
- **Phase B — Components (4 parallel agents).** Tasks B–E. Tasks B and C both add new files to the `internal/tools/lab` package; D is `internal/tui`; E is markdown. No two tasks edit the same file.
- **Phase C — Integration (sequential).** Task F: register the tools, start the webhook receiver, wire the TUI.

### Hard rules for Phase B agents

1. **Only touch your task's files** (listed per task). `cmd/proteus/` and `internal/tui/app.go` are off-limits in Phase B — integration is Task F.
2. **Never leave a package non-compiling.** Tasks B and C share the `lab` package; when doing TDD, write a compiling stub before the failing test. A failing assertion is fine; a compile error blocks the other agent.
3. **Offline, deterministic tests only.** `httptest.Server`, signed payloads, stub clients — never hit the real network.
4. **Follow existing patterns** — match `internal/tools/design/`, `internal/tools/fold/`, `internal/store/experiments.go`, and the existing TUI panels.
5. **Do NOT run git.** `gofmt -w` your files. The orchestrator commits.

---

## File Structure

| File | Task | Responsibility |
|---|---|---|
| `internal/tools/lab/adaptyv.go` (new) | A | HTTP `Client` + hand-written API types |
| `internal/tools/lab/auth.go` (new) | A | `ADAPTYV_API_TOKEN` env + keychain token storage |
| `internal/tools/lab/tools.go` (new) | B | the five `lab.*` agent tools |
| `internal/tools/lab/tools_test.go` (new) | B | tool tests with a stub client |
| `internal/tools/lab/webhook.go` (new) | C | webhook receiver + `WebhookEventMsg` |
| `internal/domain/types.go` (modify) | C | add the `WebhookEvent` type |
| `internal/store/webhook_events.go` (new) | C | `InsertWebhookEvent` / `ListWebhookEvents` |
| `internal/tui/lab.go` (new) | D | the wet-lab panel (`labModel`) |
| `internal/tui/labmodal.go` (new) | D | the rich submit-confirmation overlay |
| `internal/skills/builtin/submit-to-adaptyv.md` (new) | E | submission skill |
| `internal/skills/builtin/close-the-loop.md` (new) | E | close-the-loop skill |
| `cmd/proteus/main.go`, `internal/tui/app.go` (modify) | F | registration + wiring |

---

## Task A: Adaptyv client and token storage (foundation)

**Files:** `internal/tools/lab/adaptyv.go`, `internal/tools/lab/auth.go` (new); `go.mod` / `go.sum` (add `github.com/99designs/keyring`).

**Read first:** `internal/tools/knowledge/` for an existing HTTP-client tool pattern, SPECS §12.1 and §7.2.8.

- [ ] `go get github.com/99designs/keyring`.
- [ ] `auth.go`: `Token() (string, error)` — return `$ADAPTYV_API_TOKEN` if set, else read key `adaptyv` from the `proteus` keychain service via `99designs/keyring`; a clear error if neither is present. `StoreToken(token string) error` — write it to the keychain.
- [ ] `adaptyv.go`: `Client struct { baseURL, token string; http *http.Client }`, `NewClient(token string) *Client` (baseURL `https://foundry-api-public.adaptyvbio.com/api/v1`, overridable). Methods with hand-written request/response structs — each struct commented `// verify field names against the live OpenAPI spec`:
  `ListTargets(ctx) ([]Target, error)`, `EstimateCost(ctx, CostRequest) (*CostEstimate, error)`, `SubmitExperiment(ctx, SubmitRequest) (*Experiment, error)`, `GetExperiment(ctx, id string) (*Experiment, error)`, `GetResults(ctx, id string) ([]Result, error)`. All set `Authorization: Bearer <token>`; non-2xx → an error including the status and body.
- [ ] Tests (`adaptyv_test.go`, `auth_test.go`): the client against an `httptest.Server` returning canned JSON for each method; `Token()` resolves the env var (use `t.Setenv`). Keychain writes are not unit-tested (OS-dependent) — keep `StoreToken` thin.
- [ ] Verify: `go build ./internal/tools/lab/`, `go test ./internal/tools/lab/`, `gofmt -w`.

## Task B: the lab.* agent tools

**Owns (only these):** `internal/tools/lab/tools.go`, `internal/tools/lab/tools_test.go` (new).

**Read first:** `internal/tools/fold/esmfold.go` and `internal/tools/design/design.go` for the `tools.Tool` interface and the synchronous vs job-based patterns; `internal/tools/design/design_test.go` for the test style; SPECS §7.2.8; Task A's `adaptyv.go` (the `Client`).

- [ ] Implement five tools in `tools.go`, each a small struct over a `*Client`, implementing `tools.Tool`:
  `lab.targets_search`, `lab.cost_estimate`, `lab.experiment_status`, `lab.results` — synchronous, `RequiresConfirmation` false. `lab.submit_experiment` — `RequiresConfirmation` returns **true**; on `Execute` it calls `Client.SubmitExperiment`, then persists a `domain.Experiment` (reuse `store.InsertExperiment`) carrying the returned `external_id`, and returns a `tools.Result`.
- [ ] Constructors `NewTargetsSearchTool(c *Client)`, … taking the client (and `*store.Store` where persistence is needed).
- [ ] Tests: a stub client (an interface the tools depend on, or a `Client` pointed at an `httptest.Server`) — assert each tool's `Name()`, that each implements `tools.Tool`, that `lab.submit_experiment.RequiresConfirmation()` is true, and that a submit persists an `Experiment` with the external id.
- [ ] Verify: `go build ./internal/tools/lab/`, `go test ./internal/tools/lab/ -run TestLabTool`, `gofmt -w`.

## Task C: webhook receiver and webhook-event persistence

**Owns (only these):** `internal/tools/lab/webhook.go`, `webhook_test.go` (new); `internal/domain/types.go` (modify — add one type); `internal/store/webhook_events.go`, `webhook_events_test.go` (new).

**Read first:** SPECS §7.2.8 (the webhook block) and §12.3; `internal/store/experiments.go` for the store method pattern; `internal/domain/types.go` around the `Experiment` type.

- [ ] `domain/types.go`: add a `WebhookEvent` struct (`ID string`, `ExperimentID ExperimentID`, `Received time.Time`, `Payload []byte`/`json.RawMessage`, `EventType string`). Additive — do not change existing types.
- [ ] `store/webhook_events.go`: `InsertWebhookEvent(WebhookEvent) error` and `ListWebhookEvents(ExperimentID) ([]WebhookEvent, error)`, mirroring `experiments.go`. The `webhook_events` table already exists in the schema — confirm its columns and match them.
- [ ] `webhook.go`: `StartReceiver(ctx context.Context, port int, st *store.Store, bus chan<- tea.Msg) error` — an `http.Server` whose `http.ServeMux` has `POST /webhooks/adaptyv`. The handler: verify the HMAC signature (a shared-secret HMAC-SHA256 over the body; on mismatch respond 401 and persist nothing), `store.InsertWebhookEvent`, update the matching `Experiment`'s status/results, and send a `WebhookEventMsg{ExperimentID}` on `bus`. Define the exported `WebhookEventMsg` type here.
- [ ] Tests: POST a correctly-signed payload to the handler (via `httptest`) and assert a `WebhookEvent` is stored, the `Experiment` updated, and a message reaches the bus channel; POST a bad-signature payload and assert a 401 with nothing persisted.
- [ ] Verify: `go build ./internal/tools/lab/ ./internal/store/`, `go test ./internal/tools/lab/ -run TestWebhook` and `go test ./internal/store/ -run TestWebhook`, `gofmt -w`.

## Task D: wet-lab TUI components

**Owns (only these):** `internal/tui/lab.go`, `internal/tui/labmodal.go`, and their `_test.go` files (new). Do NOT edit `app.go`.

**Read first:** `internal/tui/jobs.go` and `designs.go` (panel pattern, `sectionRule`, the v0.4 theme tokens), `internal/tui/modal.go` (the overlay pattern), SPECS §10.2 and §12.2.

- [ ] `lab.go`: a `labModel` panel mirroring `jobsModel` — `newLabModel(th Theme)`, `setExperiments([]domain.Experiment)`, `setWidth(int)`, `View() string`. It renders a `wet-lab` section rule and, per experiment, a line like `expt_4 · day 3 of ~21` (compute the day from `SubmittedAt`); an actionable empty state.
- [ ] `labmodal.go`: a `submitModal` overlay model rendering the SPECS §12.2 box — target, assay, sequence count + first three sequence previews, estimated cost, `~21 days`, webhook URL — with `[ y ] submit  [ n / esc ] cancel`. Constructor takes the submit details; `view(th Theme, width int) string`.
- [ ] Tests: the panel renders the section label, an experiment line, and the empty state; the modal's `view` contains the target, the cost, and `~21 days`.
- [ ] Verify: `go build ./internal/tui/`, `go test ./internal/tui/ -run 'TestLab|TestSubmitModal'`, `gofmt -w`.

## Task E: skills

**Owns (only these):** `internal/skills/builtin/submit-to-adaptyv.md`, `close-the-loop.md` (new).

**Read first:** `internal/skills/builtin/design-binder.md` for the format; SPECS §8.2 (the "Procedure" / "Pre-flight checks" / "Confirmation" content for submission, and the close-the-loop content).

- [ ] `submit-to-adaptyv.md`: when to use, pre-flight checks (`lab.cost_estimate`, solubility), the confirmation step, and `lab.submit_experiment`. Reference only real tools (`lab.*`, `score.*`).
- [ ] `close-the-loop.md`: interpreting returned kinetics, comparing predicted vs measured, and feeding results back into the next design round.
- [ ] Verify: `go test ./internal/skills/` still passes (the loader picks up the new files).

---

## Task F: Integration (orchestrator, after A–E)

**Files:** `cmd/proteus/main.go`, `internal/tui/app.go`, `main_test.go`.

- [ ] `main.go` `buildRegistry`: construct a `lab.Client` from `lab.Token()` and register the five `lab.*` tools.
- [ ] `main.go` `runTUI`: start `lab.StartReceiver` on a goroutine (port from config, default 9876) with the agent bus; respect `[webhook] enabled`.
- [ ] `app.go`: add the `labModel` to the layout (right column, under designs); add the `submitModal` as a new overlay (extend the `overlay` enum, `handleKey`, `View`); add a `/auth adaptyv <token>` case to `runSlashCommand` calling `lab.StoreToken`; handle `lab.WebhookEventMsg` in `Update` (refresh the panel, post a chat notification).
- [ ] Update `main_test.go` to assert the `lab.*` tools are registered.
- [ ] Verify: `gofmt -l` empty, `go vet ./...` clean, `go build ./...` clean, `go test ./...` all pass.
- [ ] Commit.

---

## Self-Review checklist

- [ ] Every component in the design doc §4 maps to a task.
- [ ] No Phase B task edits `cmd/proteus/` or `internal/tui/app.go`.
- [ ] Tasks B and C touch different files within the `lab` package.
- [ ] Tool names (`lab.targets_search`, `lab.cost_estimate`, `lab.submit_experiment`, `lab.experiment_status`, `lab.results`) are consistent between Tasks B and F.
- [ ] `go test ./...` and `go vet ./...` clean after Task F.
