# Proteus — Config System SP3: `[budget]` + `[webhook]` — Design

**Date:** 2026-05-19
**Status:** Approved, ready for planning
**Milestone:** Configuration system (SPECS §14) — SP3 of 3 (final)
**Parent design:** `docs/superpowers/specs/2026-05-19-proteus-config-system-design.md` (§10 SP3 outline)
**Predecessors:** SP1 (`internal/config` + `models.toml`) and SP2 (`config.toml` —
`[ui]`/`[defaults]`/`[knowledge]`) — both merged to `master`.

## 1. Goal & scope

Add the last two `config.toml` sections and wire them into live behaviour:

- **`[webhook]`** (enabled, port, public_url) — **fully wired.** Configures the
  existing Adaptyv webhook receiver (`lab.StartReceiver`) and the webhook URL
  used by the `lab.submit_experiment` tool and the submit-confirmation modal.
- **`[budget]`** (session_soft_limit_usd, wetlab_requires_confirmation) —
  **fully wired**, including new live session cost tracking.

The parent design (§10) flagged `[budget]` as blocked on cost-tracking. That is
no longer true: token usage is already captured per LLM call
(`internal/agent/loop.go` records `llm.Usage` on the `done` event), and the
model registry already carries per-model prices (`InputPricePer1M`,
`OutputPricePer1M`). SP3 connects those two facts into a running session cost.

After SP3 the `internal/config` config system is complete — every section of
SPECS §14.2 is parsed, validated, and consumed (except `[ui]`, intentionally
deferred to v0.5 by SP2).

## 2. The two config sections

Added to `internal/config/config_toml.go`'s `Config`:

```go
type Config struct {
	UI        UIConfig        `toml:"ui"`
	Defaults  DefaultsConfig  `toml:"defaults"`
	Knowledge KnowledgeConfig `toml:"knowledge"`
	Webhook   WebhookConfig   `toml:"webhook"`
	Budget    BudgetConfig    `toml:"budget"`
}

type WebhookConfig struct {
	Enabled   bool   `toml:"enabled"`
	Port      int    `toml:"port"`
	PublicURL string `toml:"public_url"`
}

type BudgetConfig struct {
	SessionSoftLimitUSD        float64 `toml:"session_soft_limit_usd"`
	WetlabRequiresConfirmation bool    `toml:"wetlab_requires_confirmation"`
}
```

Embedded `internal/config/config.toml` gains:

```toml
[webhook]
enabled = true
port = 9876
public_url = ""              # optional ngrok/Tailscale URL

[budget]
session_soft_limit_usd = 5.0
wetlab_requires_confirmation = true   # never disable
```

### 2.1 Validation (`Config.validate()`)

- `webhook.port` — must be in `1..65535`.
- `budget.session_soft_limit_usd` — must be `>= 0`. `0` means **no limit**
  (cost tracking still runs; the soft-limit indicator is simply never tripped).
- `budget.wetlab_requires_confirmation` — must be `true`. A `false` is a loud
  load error (`"budget.wetlab_requires_confirmation must be true (never
  disable)"`), honouring the SPECS §14.2 "never disable" guarantee.

A bad value fails `LoadConfig` with a clear, file-named error — the SP1/SP2
no-silent-fallback rule.

### 2.2 `wetlab_requires_confirmation` is validated, not wired

`lab.submit_experiment`'s `RequiresConfirmation` already returns an
unconditional `true` — that *is* the enforcement of the SPECS guarantee.
Because the config value is itself constrained to `true` (§2.1), threading it
into the tool would replace a const `true` with a different const `true`:
pointless churn. SP3 therefore **parses and validates** the field but does not
wire it. This is deliberate and documented here so a future reader does not
read the gap as an oversight.

## 3. Cost tracking

### 3.1 Pricing — `internal/llm`

A new method on `ModelRegistry`:

```go
// CostUSD prices a token-usage record with the active model's per-1M prices.
func (mr *ModelRegistry) CostUSD(u Usage) float64
```

`cost = InputTokens/1e6 * InputPricePer1M + OutputTokens/1e6 * OutputPricePer1M`,
read from the active model's `ModelDescriptor`. Local models (vLLM, Ollama)
carry `0` prices, so their cost is `0`. If no model is active it returns `0`.

A turn is priced with whatever model is active when the turn ends; switching
model mid-turn is a rare, accepted minor inaccuracy.

### 3.2 Per-turn usage — `internal/agent`

`TurnDoneMsg` currently carries nothing:

```go
type TurnDoneMsg struct{ Usage llm.Usage }   // SP3: was struct{}
```

`Loop.Run` makes one or more `StreamChat` calls per user turn (one per
tool-calling round). Today only the *final* call's `Usage` survives in the
local `resp`. SP3 adds a `turnUsage` accumulator in `Run` that sums
`InputTokens`/`OutputTokens` across every round, and `TurnDoneMsg` carries that
turn total. `TurnErrorMsg` is unchanged (a failed turn reports no cost).

### 3.3 Session cost & the soft-limit indicator — `internal/tui`

`Model` gains `sessionCost float64` and `budgetWarned bool`. On
`agent.TurnDoneMsg`:

1. `m.sessionCost += m.models.CostUSD(msg.Usage)`.
2. `m.status.cost = m.sessionCost` — the statusbar `$<cost>` segment (SPECS
   §10.7.6), dead until now, becomes live.
3. If `m.budgetLimit > 0 && m.sessionCost > m.budgetLimit && !m.budgetWarned`:
   append a one-time chat warning (`entryError`-style line, e.g. `"budget:
   session cost $X.XX exceeded the $Y.YY soft limit"`) and set `budgetWarned`.

`statusBarModel` gains a `costLimit float64` field. `footerView` renders the
cost segment in the `Warning` palette colour when
`costLimit > 0 && cost > costLimit` — exactly mirroring the existing
`ctxPercent > 80` treatment of the context segment. The cost and context
segments are styled independently.

Session cost is in-memory and per-session (the SPECS field is named
*session*_soft_limit_usd); it resets on restart. No persistence — YAGNI.

## 4. Webhook wiring

### 4.1 The derived webhook URL

One helper computes the effective URL Adaptyv should call back:

- `public_url` set → `strings.TrimRight(public_url, "/") + "/webhooks/adaptyv"`
- `public_url` empty → `http://localhost:<port>/webhooks/adaptyv`

This replaces the two hardcoded `http://localhost:9876/webhooks/adaptyv`
literals (in `tui` `buildSubmitModal` and in the `lab` submit path).

### 4.2 The receiver

`cmd/proteus/main.go` builds `tui.Deps.WebhookPort` from `[webhook]`:
`cfg.Webhook.Port` when `cfg.Webhook.Enabled`, else `0`. `app.go` already
treats a `0` port as "receiver disabled" — so `enabled = false` cleanly stops
the receiver from starting. No change to `lab.StartReceiver` itself.

### 4.3 The submit URL

`tui.Deps` gains `WebhookURL string` (the §4.1 derived URL); `Model` stores it.
`buildSubmitModal` uses it as the display fallback instead of the hardcoded
literal.

`NewSubmitExperimentTool` gains a `defaultWebhookURL string` parameter; its
`Execute` fills `req.WebhookURL` with that default when the agent's tool input
omits `webhook_url`. `cmd/proteus/main.go` passes the §4.1 derived URL when it
constructs the tool. This makes a real submission carry the correct callback
URL even when the model does not specify one.

### 4.4 `tui.Deps` additions

`Deps` keeps its granular-field style (it already has `WebhookPort int`). SP3
adds `WebhookURL string` and `BudgetLimitUSD float64`. `WebhookPort` is reused.

## 5. Wiring summary (`cmd/proteus/main.go`)

`runTUI` already loads `cfg` (SP2). SP3:

- Compute `webhookURL := config-derived URL` from `cfg.Webhook`.
- `buildRegistry` passes `webhookURL` to `lab.NewSubmitExperimentTool`.
- `tui.Deps`: `WebhookPort` from `cfg.Webhook` (port or 0), `WebhookURL =
  webhookURL`, `BudgetLimitUSD = cfg.Budget.SessionSoftLimitUSD`.

## 6. Testing

All offline and deterministic.

- **`internal/config`** — `config_toml_test.go`: the embedded default still
  parses; the new `[webhook]`/`[budget]` fields round-trip; an out-of-range
  `port`, a negative `session_soft_limit_usd`, and
  `wetlab_requires_confirmation = false` are each rejected.
- **`internal/llm`** — `CostUSD`: a model with non-zero prices yields the
  expected dollar amount; a zero-price (local) model yields `0`.
- **`internal/agent`** — `Loop.Run` accumulates usage across multiple
  tool-calling rounds; `TurnDoneMsg.Usage` carries the turn total. (Uses the
  package's existing fake provider/registry test scaffolding.)
- **`internal/tui`** — `statusBarModel.footerView` renders the cost segment in
  the Warning colour once `cost > costLimit`; a `TurnDoneMsg` increments
  `sessionCost`; crossing the limit appends exactly one budget warning.
- **`internal/tools/lab`** — `submitExperimentTool.Execute` leaves a
  caller-supplied `webhook_url` intact and substitutes the default only when it
  is empty.

**Acceptance:** `config.toml` first-run includes `[webhook]`/`[budget]`;
`enabled = false` stops the webhook receiver; an invalid port or
`wetlab_requires_confirmation = false` fails startup loudly; the statusbar
`$cost` increments after a turn against a priced model and turns the Warning
colour past `session_soft_limit_usd`; `go build ./...`, `go test ./...`, and
`go vet ./...` pass; `gofmt -l` is clean.

## 7. Files

- **Modify** `internal/config/config_toml.go` (+ `config.toml`, `config_toml_test.go`)
  — `WebhookConfig`, `BudgetConfig`, validation.
- **Modify** `internal/llm/modelregistry.go` (+ `modelregistry_test.go`) —
  `CostUSD`.
- **Modify** `internal/agent/loop.go` (+ `loop_test.go`) — `TurnDoneMsg.Usage`,
  turn-usage accumulation.
- **Modify** `internal/tui/statusbar.go`, `internal/tui/app.go` (+ tests) —
  `costLimit`, session cost, budget warning, `Deps` fields, `buildSubmitModal`.
- **Modify** `internal/tools/lab/tools.go` (+ `tools_test.go`) —
  `NewSubmitExperimentTool` default webhook URL.
- **Modify** `cmd/proteus/main.go` (+ `main_test.go`) — wire `[webhook]`/`[budget]`.

## 8. Out of scope

Cost *persistence* across sessions; a per-tool cost breakdown; hard budget
*enforcement* (blocking a turn — SP3 only warns); the OS-keychain key fallback;
`[ui]` consumption (v0.5); per-project `proteus.toml`; `${ENV}` interpolation.
