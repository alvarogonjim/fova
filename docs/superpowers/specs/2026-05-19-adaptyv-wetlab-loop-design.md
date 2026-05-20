# Proteus — Adaptyv wet-lab loop — Design

**Date:** 2026-05-19
**Status:** Approved, ready for planning
**Predecessor:** v0.4 sub-project 1 (antibody / enzyme tools) — complete.
**Source of truth:** `docs/SPECS.md` §12 (Adaptyv Integration), §7.2.8 (`lab.*` tools), §14 (config).

## 1. Goal

Implement the Adaptyv Foundry integration — submit designs to the wet lab,
receive results via webhook, and surface experiments in the TUI. This closes
the design → validation loop and is the headline of the v0.4 "Closing the loop"
milestone. It is sub-project 2 of v0.4's remaining scope.

## 2. Scope

**In scope:**
- An Adaptyv HTTP client (`internal/tools/lab/adaptyv.go`).
- Keychain-backed token storage (`internal/tools/lab/auth.go`).
- The webhook receiver (`internal/tools/lab/webhook.go`).
- Five agent-facing `lab.*` tools.
- The TUI: a rich submit-confirmation modal, the wet-lab panel, a `/auth adaptyv`
  slash command.
- Two built-in skills: `submit-to-adaptyv.md`, `close-the-loop.md`.

**Out of scope:**
- No change to the design / fold tools from sub-project 1.
- No new CLI subcommands (see decision 2).
- Verifying the live Adaptyv API contract — v0.4's acceptance criteria already
  treat the staging end-to-end run as a separate manual step.

## 3. Key decisions

1. **Hand-written minimal API types**, not `oapi-codegen`. Go structs are
   defined by hand for only the five endpoints used, each with a "verify field
   names against the live OpenAPI spec" comment. The build stays fully offline
   and reproducible. *Deviates from SPECS §12.1's build-time codegen.*
2. **TUI-first command surface — no new CLI subcommands.** The token is read
   from `ADAPTYV_API_TOKEN`, falling back to the OS keychain; a `/auth adaptyv`
   slash command stores it in the keychain. Submission and status run through
   the agent `lab.*` tools and the wet-lab panel. *Deviates from SPECS §15's
   `proteus auth` / `proteus design submit` / `proteus experiment status`.*
3. **The webhook receiver uses the stdlib `net/http` ServeMux** (Go 1.22 method
   patterns), not `chi`. A single route does not justify a new dependency.
   *Deviates from the `chi` snippet in SPECS §7.2.8.*
4. **Reuse the existing persistence.** `domain.Experiment` /
   `domain.ExperimentResult` and `store.InsertExperiment` / `GetExperiment` /
   `ListExperiments` already exist and are used as-is. The `webhook_events`
   table exists but has no Go API — add a `domain.WebhookEvent` type and a
   `store.InsertWebhookEvent` method.
5. **The submit confirmation is a richer modal** than the generic yes/no
   `modalModel`: it shows the target, assay, sequence count + first three
   sequence previews, estimated cost, ~21-day turnaround, and the webhook URL
   (SPECS §12.2). It is a distinct overlay; the generic modal is unchanged.

## 4. Components

### 4.1 `internal/tools/lab/adaptyv.go` — HTTP client

A `Client{baseURL, token string; http *http.Client}` with `NewClient(token)`.
Methods, each with hand-written request/response structs:
`ListTargets`, `EstimateCost`, `SubmitExperiment`, `GetExperiment`, `GetResults`.
Base URL `https://foundry-api-public.adaptyvbio.com/api/v1`; auth header
`Authorization: Bearer <token>`. The base URL is overridable so tests point at
an `httptest.Server`.

### 4.2 `internal/tools/lab/auth.go` — token storage

`Token() (string, error)` reads `ADAPTYV_API_TOKEN` first, then the OS keychain
via `github.com/99designs/keyring`. `StoreToken(string) error` writes to the
keychain. Adds the `99designs/keyring` dependency.

### 4.3 `internal/tools/lab/webhook.go` — webhook receiver

`StartReceiver(ctx, port int, st *store.Store, bus chan<- tea.Msg) error` —
an `http.Server` with a `POST /webhooks/adaptyv` route on an `http.ServeMux`.
The handler verifies the HMAC signature, persists the event via
`store.InsertWebhookEvent`, updates the matching `Experiment` (status /
results), and sends a `WebhookEventMsg` on the bus so the TUI shows a
notification. Run on a goroutine for the TUI's lifetime.

### 4.4 `internal/tools/lab/` — agent tools

Five tools implementing the `tools.Tool` interface (SPECS §7.2.8):
`lab.targets_search`, `lab.cost_estimate`, `lab.submit_experiment`,
`lab.experiment_status`, `lab.results`. Each wraps a `Client` call.
`lab.submit_experiment` returns `RequiresConfirmation() == true`; on success it
persists an `Experiment` with the returned `external_id`.

### 4.5 TUI

- `internal/tui/lab.go` — the wet-lab panel (`labModel`): the active experiment
  and "day N of ~21", styled like the jobs / designs panels.
- A rich submit-confirmation overlay (SPECS §12.2), added alongside the existing
  `modalModel` / `pickerModel` overlays.
- A `/auth adaptyv <token>` slash command calling `lab.StoreToken`.
- The root model handles `WebhookEventMsg` — refreshes the panel and posts a
  chat notification.

### 4.6 Skills

`internal/skills/builtin/submit-to-adaptyv.md` and `close-the-loop.md`,
following the existing built-in skill format.

## 5. Data flow

**Submission:** agent calls `lab.cost_estimate` → calls `lab.submit_experiment`
→ the registry sees `RequiresConfirmation` → the TUI shows the rich modal → on
confirm, the client POSTs to Adaptyv with the webhook URL → the returned
`experiment_id` is stored on a new `Experiment` record.

**Results:** Adaptyv POSTs to `/webhooks/adaptyv` → the handler verifies HMAC,
persists a `WebhookEvent`, updates the `Experiment`, and emits `WebhookEventMsg`
→ the wet-lab panel refreshes. With no public URL configured, the agent
backfills via `lab.experiment_status` on startup.

## 6. Error handling

- Missing token → tools return a clear error pointing the user at `/auth adaptyv`.
- HMAC mismatch → the webhook handler responds 401 and logs; nothing is persisted.
- Network / non-2xx responses → surfaced inline in chat, never silently dropped.
- `lab.submit_experiment` without confirmation → not possible; the registry
  enforces the modal.

## 7. Testing

Offline and deterministic, consistent with v0.1–v0.4:
- The client is tested against an `httptest.Server` returning canned JSON.
- The webhook handler is tested by POSTing a correctly-signed payload (and a
  bad-signature payload) directly to the handler and asserting the store / bus.
- The `lab.*` tools are tested with a stub client.
- TUI components use the existing snapshot / render-string patterns.
- **Manual (unchanged):** a real submission against Adaptyv staging needs an
  account and is verified separately, per v0.4's acceptance criteria.

## 8. Acceptance criteria

1. `/auth adaptyv <token>` stores the token in the OS keychain; the client then
   authenticates without the env var.
2. The agent calls `lab.targets_search` and lists Adaptyv targets (against a
   stub in tests).
3. `lab.submit_experiment` triggers the rich confirmation modal; on confirm an
   `Experiment` with an `external_id` is persisted.
4. A signed test POST to `/webhooks/adaptyv` is accepted, persists a
   `WebhookEvent`, updates the `Experiment`, and refreshes the wet-lab panel; a
   bad-signature POST is rejected.
5. `go test ./...` passes and `go vet ./...` is clean.

## 9. Execution approach

Invoke `writing-plans` to produce a task-level plan, then build it with parallel
Opus agents: a foundation step (the client + token storage), then independent
agents for the `lab.*` tools, the webhook receiver, the TUI, and the skills,
then an integration pass that registers the tools and starts the receiver.
