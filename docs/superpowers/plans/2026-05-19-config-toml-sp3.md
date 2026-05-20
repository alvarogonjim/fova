# Config System SP3 — `[budget]` + `[webhook]` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `[budget]` and `[webhook]` sections to `config.toml`, wire the webhook receiver/URL from config, and add live LLM session cost tracking with a soft-limit warning.

**Architecture:** Two new `config.Config` sections. `[webhook]` drives the existing `lab.StartReceiver` and the Adaptyv callback URL. `[budget]` drives a new session cost accumulator: token usage (already captured per LLM call) is summed per turn, priced by the active model, displayed in the status bar, and warned on once it crosses the soft limit.

**Tech Stack:** Go, `github.com/BurntSushi/toml`, Bubble Tea.

**Spec:** `docs/superpowers/specs/2026-05-19-proteus-config-toml-sp3-design.md`.

**Branch:** `feat/config-toml-sp3` (already created).

---

## File map

| File | Change | Task |
|---|---|---|
| `internal/config/config.toml` | modify — add `[webhook]`/`[budget]` | 1 |
| `internal/config/config_toml.go` | modify — `WebhookConfig`, `BudgetConfig`, validation, `EffectiveURL` | 1 |
| `internal/config/config_toml_test.go` | modify — new validation tests | 1 |
| `internal/llm/modelregistry.go` (+ `_test.go`) | modify — `CostUSD` | 2 |
| `internal/agent/loop.go` (+ `loop_test.go`, `mock_test.go`) | modify — `TurnDoneMsg.Usage`, usage accumulation | 2 |
| `internal/tui/statusbar.go` (+ `statusbar_test.go`) | modify — `costLimit`, segment-styled footer | 3 |
| `internal/tui/app.go` (+ `app_test.go`) | modify — session cost, budget warning, `Deps.BudgetLimitUSD` | 3 |
| `internal/tui/app.go` | modify — `Deps.WebhookURL`, `buildSubmitModal` | 4 |
| `internal/tools/lab/tools.go` (+ `tools_test.go`) | modify — `NewSubmitExperimentTool` default URL | 4 |
| `cmd/proteus/main.go` | modify — wire `[webhook]`/`[budget]` | 4 |

Tasks are **sequential** (1 → 2 → 3 → 4). Task 3 consumes Task 1+2; Task 4 consumes Task 1.

---

## Task 1: `[budget]` + `[webhook]` config

**Files:** Modify `internal/config/config.toml`, `internal/config/config_toml.go`, `internal/config/config_toml_test.go`.

- [ ] **Step 1: Add the two sections to the embedded `internal/config/config.toml`** — append these lines to the end of the file (keep the existing `[ui]`/`[defaults]`/`[knowledge]` sections unchanged):

```toml

[webhook]
enabled = true
port = 9876
public_url = ""              # optional ngrok/Tailscale URL

[budget]
session_soft_limit_usd = 5.0
wetlab_requires_confirmation = true   # never disable
```

- [ ] **Step 2: Write the failing tests** — Append to `internal/config/config_toml_test.go` (the file already has `package config` and imports `os`, `path/filepath`, `testing`):

```go
func TestParseConfigRejectsBadPort(t *testing.T) {
	in := `
[ui]
theme = "auto"
inline_graphics = "auto"
[defaults]
compute_backend = "local"
[webhook]
enabled = true
port = 0
[budget]
session_soft_limit_usd = 5.0
wetlab_requires_confirmation = true
`
	if _, err := parseConfig(in); err == nil {
		t.Fatal("expected an error for an out-of-range webhook.port")
	}
}

func TestParseConfigRejectsNegativeBudget(t *testing.T) {
	in := `
[ui]
theme = "auto"
inline_graphics = "auto"
[defaults]
compute_backend = "local"
[webhook]
enabled = true
port = 9876
[budget]
session_soft_limit_usd = -1.0
wetlab_requires_confirmation = true
`
	if _, err := parseConfig(in); err == nil {
		t.Fatal("expected an error for a negative session_soft_limit_usd")
	}
}

func TestParseConfigRejectsDisabledWetlabConfirmation(t *testing.T) {
	in := `
[ui]
theme = "auto"
inline_graphics = "auto"
[defaults]
compute_backend = "local"
[webhook]
enabled = true
port = 9876
[budget]
session_soft_limit_usd = 5.0
wetlab_requires_confirmation = false
`
	if _, err := parseConfig(in); err == nil {
		t.Fatal("expected an error for wetlab_requires_confirmation = false")
	}
}

func TestWebhookEffectiveURL(t *testing.T) {
	def := WebhookConfig{Enabled: true, Port: 9876}
	if got := def.EffectiveURL(); got != "http://localhost:9876/webhooks/adaptyv" {
		t.Errorf("EffectiveURL() = %q", got)
	}
	pub := WebhookConfig{Enabled: true, Port: 9876, PublicURL: "https://x.ngrok.io/"}
	if got := pub.EffectiveURL(); got != "https://x.ngrok.io/webhooks/adaptyv" {
		t.Errorf("EffectiveURL() with public_url = %q", got)
	}
}

func TestDefaultConfigHasWebhookAndBudget(t *testing.T) {
	c := DefaultConfig()
	if c.Webhook.Port == 0 || c.Budget.SessionSoftLimitUSD == 0 {
		t.Fatalf("default config missing webhook/budget values: %+v", c)
	}
	if !c.Budget.WetlabRequiresConfirmation {
		t.Fatal("default wetlab_requires_confirmation must be true")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/config/ -run 'Webhook|Budget|Port'`
Expected: FAIL — `undefined: WebhookConfig` / `c.Webhook undefined`

- [ ] **Step 4: Add the types, validation, and `EffectiveURL` to `internal/config/config_toml.go`**

Add `"strings"` to the import block.

Add these two type declarations (next to `KnowledgeConfig`):

```go
// WebhookConfig is the [webhook] section of config.toml. It configures the
// Adaptyv results webhook receiver and the callback URL sent to Adaptyv.
type WebhookConfig struct {
	Enabled   bool   `toml:"enabled"`
	Port      int    `toml:"port"`
	PublicURL string `toml:"public_url"`
}

// EffectiveURL is the URL Adaptyv should call back: public_url (with the
// webhook path appended) when set, else a localhost URL on the configured port.
func (w WebhookConfig) EffectiveURL() string {
	if w.PublicURL != "" {
		return strings.TrimRight(w.PublicURL, "/") + "/webhooks/adaptyv"
	}
	return fmt.Sprintf("http://localhost:%d/webhooks/adaptyv", w.Port)
}

// BudgetConfig is the [budget] section of config.toml.
type BudgetConfig struct {
	SessionSoftLimitUSD        float64 `toml:"session_soft_limit_usd"`
	WetlabRequiresConfirmation bool    `toml:"wetlab_requires_confirmation"`
}
```

Add the two new fields to the `Config` struct so it reads:

```go
// Config is the parsed config.toml (SPECS §14.2).
type Config struct {
	UI        UIConfig        `toml:"ui"`
	Defaults  DefaultsConfig  `toml:"defaults"`
	Knowledge KnowledgeConfig `toml:"knowledge"`
	Webhook   WebhookConfig   `toml:"webhook"`
	Budget    BudgetConfig    `toml:"budget"`
}
```

In `validate()`, add these checks immediately before the final `return nil`:

```go
	if c.Webhook.Port < 1 || c.Webhook.Port > 65535 {
		return fmt.Errorf("webhook.port %d must be between 1 and 65535", c.Webhook.Port)
	}
	if c.Budget.SessionSoftLimitUSD < 0 {
		return fmt.Errorf("budget.session_soft_limit_usd must not be negative")
	}
	if !c.Budget.WetlabRequiresConfirmation {
		return fmt.Errorf("budget.wetlab_requires_confirmation must be true (never disable)")
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w internal/config/ && go test ./internal/config/ && go build ./... && gofmt -l internal/config/`
Expected: PASS; `gofmt -l` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.toml internal/config/config_toml.go internal/config/config_toml_test.go
git commit -m "$(printf 'feat: add config.toml [webhook] and [budget] sections\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 2: Cost plumbing — `CostUSD` + per-turn usage

**Files:** Modify `internal/llm/modelregistry.go`, `internal/llm/modelregistry_test.go`, `internal/agent/loop.go`, `internal/agent/loop_test.go`, `internal/agent/mock_test.go`.

### 2a — `ModelRegistry.CostUSD`

- [ ] **Step 1: Write the failing test** — Append to `internal/llm/modelregistry_test.go` (`config` is already imported):

```go
func TestModelRegistryCostUSD(t *testing.T) {
	cat := config.Catalog{
		Providers: []config.Provider{{Name: "p", Kind: "anthropic"}},
		Models: []config.Model{
			{ID: "priced", Provider: "p", InputPricePer1M: 3, OutputPricePer1M: 15},
			{ID: "free", Provider: "p", InputPricePer1M: 0, OutputPricePer1M: 0},
		},
	}
	mr := NewModelRegistry(cat)
	if err := mr.SetModel("priced"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	// 1M input + 1M output at $3 / $15 per 1M = $18.
	if got := mr.CostUSD(Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}); got < 17.99 || got > 18.01 {
		t.Errorf("CostUSD(priced) = %v, want ~18", got)
	}
	if err := mr.SetModel("free"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if got := mr.CostUSD(Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}); got != 0 {
		t.Errorf("CostUSD(free) = %v, want 0", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/llm/ -run TestModelRegistryCostUSD`
Expected: FAIL — `mr.CostUSD undefined`

- [ ] **Step 3: Add `CostUSD` to `internal/llm/modelregistry.go`** — append this method to the file:

```go
// CostUSD prices a token-usage record with the active model's per-1M prices.
// Local models carry zero prices, so their cost is 0; an unset active model
// also yields 0.
func (mr *ModelRegistry) CostUSD(u Usage) float64 {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	for _, m := range mr.models {
		if m.ID == mr.activeID {
			return float64(u.InputTokens)/1e6*m.InputPricePer1M +
				float64(u.OutputTokens)/1e6*m.OutputPricePer1M
		}
	}
	return 0
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/llm/`
Expected: PASS

### 2b — Per-turn usage in the agent loop

- [ ] **Step 5: Update the mock provider** — In `internal/agent/mock_test.go`, the `StreamChat` method emits a `done` event. Change that line to forward the scripted response's usage:

Current:
```go
		ch <- llm.ChatEvent{Kind: "done", StopReason: resp.StopReason}
```
New:
```go
		ch <- llm.ChatEvent{Kind: "done", StopReason: resp.StopReason, Usage: resp.Usage}
```

- [ ] **Step 6: Write the failing test** — Append to `internal/agent/loop_test.go`:

```go
func TestLoopAccumulatesTurnUsage(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(echoTool{})
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Input: map[string]any{"text": "x"}}},
			Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}},
		{Text: "done", StopReason: "end_turn",
			Usage: llm.Usage{InputTokens: 20, OutputTokens: 7}},
	}}
	bus := make(chan tea.Msg, 32)
	loop := NewLoop(prov, "mock", reg, NewSession("sys"), bus, func(string) bool { return true })

	go func() { loop.Run(context.Background(), "go"); close(bus) }()
	msgs := drain(bus)

	for _, m := range msgs {
		if d, ok := m.(TurnDoneMsg); ok {
			if d.Usage.InputTokens != 30 || d.Usage.OutputTokens != 12 {
				t.Fatalf("turn usage = %+v, want {InputTokens:30 OutputTokens:12}", d.Usage)
			}
			return
		}
	}
	t.Fatal("no TurnDoneMsg on the bus")
}
```

- [ ] **Step 7: Run the test to verify it fails**

Run: `go test ./internal/agent/ -run TestLoopAccumulatesTurnUsage`
Expected: FAIL — `d.Usage undefined` (the field does not exist yet)

- [ ] **Step 8: Add `Usage` to `TurnDoneMsg` and accumulate it in `Run`** — In `internal/agent/loop.go`:

Change the `TurnDoneMsg` declaration:
```go
// TurnDoneMsg signals the agent finished its turn. Usage is the turn's total
// token consumption, summed across every LLM call the turn made.
type TurnDoneMsg struct{ Usage llm.Usage }
```

In `Loop.Run`, declare an accumulator before the `for` loop, right after `l.session.AddUserMessage(userInput)`:
```go
	var turnUsage llm.Usage
```

In the event loop's `case "done":`, add the two accumulation lines so the case reads:
```go
			case "done":
				resp.Usage = ev.Usage
				resp.StopReason = ev.StopReason
				turnUsage.InputTokens += ev.Usage.InputTokens
				turnUsage.OutputTokens += ev.Usage.OutputTokens
```

Change the `TurnDoneMsg` send (the line `l.bus <- TurnDoneMsg{}`):
```go
		if len(resp.ToolCalls) == 0 {
			l.bus <- TurnDoneMsg{Usage: turnUsage}
			return
		}
```

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./internal/agent/ ./internal/llm/ && go build ./...`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/llm/modelregistry.go internal/llm/modelregistry_test.go internal/agent/loop.go internal/agent/loop_test.go internal/agent/mock_test.go
git commit -m "$(printf 'feat: track per-turn LLM token cost\n\nAdd ModelRegistry.CostUSD and carry the turn token total on TurnDoneMsg.\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 3: Cost UI — status bar warning + session cost

**Files:** Modify `internal/tui/statusbar.go`, `internal/tui/statusbar_test.go`, `internal/tui/app.go`, `internal/tui/app_test.go`.

### 3a — Status bar: a soft-limit-aware cost segment

- [ ] **Step 1: Write the failing test** — Append to `internal/tui/statusbar_test.go`:

```go
func TestStatusFooterCostWarning(t *testing.T) {
	under := newStatusBarModel(NewTheme())
	under.model = "m"
	under.cost = 1.0
	under.costLimit = 5.0

	over := newStatusBarModel(NewTheme())
	over.model = "m"
	over.cost = 9.0
	over.costLimit = 5.0

	if under.footerView() == over.footerView() {
		t.Fatal("footerView() over the cost limit should differ from under it (warning styling)")
	}
}

func TestStatusFooterNoLimitNoWarning(t *testing.T) {
	// costLimit == 0 means "no limit": a high cost must not trip the warning.
	a := newStatusBarModel(NewTheme())
	a.model = "m"
	a.cost = 1.0
	b := newStatusBarModel(NewTheme())
	b.model = "m"
	b.cost = 999.0
	if a.footerView() != b.footerView() {
		t.Fatal("footerView() must not warn on cost when costLimit is 0")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestStatusFooterCostWarning`
Expected: FAIL — `under.costLimit undefined`

- [ ] **Step 3: Add the `costLimit` field** — In `internal/tui/statusbar.go`, add a field to `statusBarModel` so it reads:

```go
type statusBarModel struct {
	theme      Theme
	width      int
	provider   string
	model      string
	cost       float64
	costLimit  float64
	project    string
	ctxPercent int
}
```

- [ ] **Step 4: Replace `footerView` and add `splitClip`** — In `internal/tui/statusbar.go`, replace the entire `footerView` method (from its doc comment through its closing brace) with:

```go
// footerView renders the bottom status line (SPECS §10.7.6):
//
//	<hint>   <model> · $<cost> · <NN>% context
//
// The cost segment turns Warning once it exceeds costLimit (costLimit 0 = no
// limit); the context segment turns Warning above 80%. The plain text is
// clipped to width before any styling so the rendered output never overflows.
func (s statusBarModel) footerView() string {
	prefix := fmt.Sprintf("%s   %s · ", footerHint(), orDash(s.model))
	cost := fmt.Sprintf("$%.2f", s.cost)
	ctx := fmt.Sprintf(" · %d%% context", s.ctxPercent)

	prefix, cost, ctx = splitClip(prefix, cost, ctx, s.width)

	costStyle := s.theme.Footer
	if s.costLimit > 0 && s.cost > s.costLimit {
		costStyle = s.theme.Footer.Foreground(s.theme.Palette.Warning)
	}
	ctxStyle := s.theme.Footer
	if s.ctxPercent > 80 {
		ctxStyle = s.theme.Footer.Foreground(s.theme.Palette.Warning)
	}
	return s.theme.Footer.Render(prefix) + costStyle.Render(cost) + ctxStyle.Render(ctx)
}

// splitClip joins a+b+c, clips the result to w runes (w<=0 = no clipping), and
// returns the three segments of the clipped string. A segment clipped away
// entirely comes back empty. Clipping the plain text up front keeps styling
// escape sequences from being counted or cut.
func splitClip(a, b, c string, w int) (string, string, string) {
	full := []rune(a + b + c)
	if w > 0 && len(full) > w {
		full = full[:w]
	}
	aEnd := len([]rune(a))
	bEnd := aEnd + len([]rune(b))
	cut := func(lo, hi int) string {
		if lo > len(full) {
			lo = len(full)
		}
		if hi > len(full) {
			hi = len(full)
		}
		if lo >= hi {
			return ""
		}
		return string(full[lo:hi])
	}
	return cut(0, aEnd), cut(aEnd, bEnd), cut(bEnd, len(full))
}
```

- [ ] **Step 5: Run the status bar tests to verify they pass**

Run: `go test ./internal/tui/ -run TestStatus`
Expected: PASS — including the existing `TestStatusFooterView`, `TestStatusFooterContextWarning`, and `TestStatusFooterWidthClip`.

### 3b — App: session cost accumulation and the budget warning

- [ ] **Step 6: Write the failing test** — Append to `internal/tui/app_test.go`. If `app_test.go` does not already import `config` and `llm`, add `"github.com/alvarogonjim/proteus/internal/config"` and `"github.com/alvarogonjim/proteus/internal/llm"` to its import block:

```go
func TestAddTurnCostAccumulatesAndWarns(t *testing.T) {
	cat := config.Catalog{
		Providers: []config.Provider{{Name: "p", Kind: "anthropic"}},
		Models:    []config.Model{{ID: "m", Provider: "p", InputPricePer1M: 100, OutputPricePer1M: 100}},
	}
	m := &Model{
		chat:        newChatModel(NewTheme(), 80, 20),
		status:      newStatusBarModel(NewTheme()),
		models:      llm.NewModelRegistry(cat),
		budgetLimit: 5.0,
	}

	// 10k in + 10k out at $100 / 1M = $1.00 + $1.00 = $2.00 — under the limit.
	m.addTurnCost(llm.Usage{InputTokens: 10_000, OutputTokens: 10_000})
	if m.sessionCost < 1.99 || m.sessionCost > 2.01 {
		t.Fatalf("sessionCost = %v, want ~2.00", m.sessionCost)
	}
	if m.budgetWarned {
		t.Fatal("budget warned before the limit was crossed")
	}

	// A large turn pushes the session well past the $5 limit.
	m.addTurnCost(llm.Usage{InputTokens: 10_000_000, OutputTokens: 0})
	if !m.budgetWarned {
		t.Fatal("expected a budget warning after crossing the limit")
	}
	if m.status.cost != m.sessionCost {
		t.Errorf("status cost %v not synced with sessionCost %v", m.status.cost, m.sessionCost)
	}
}
```

- [ ] **Step 7: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestAddTurnCost`
Expected: FAIL — `m.addTurnCost undefined` / `budgetLimit` field missing

- [ ] **Step 8: Add the fields, the `Deps` entry, and `addTurnCost`** — In `internal/tui/app.go`:

Add three fields to the `Model` struct (place them next to `sessionStart`, near the other session fields):
```go
	sessionCost float64 // running LLM cost for this TUI session, in USD
	budgetLimit float64 // [budget].session_soft_limit_usd; 0 = no limit
	budgetWarned bool   // true once the soft-limit warning has been shown
```

Add a field to the `Deps` struct (next to `WebhookPort`):
```go
	BudgetLimitUSD float64 // [budget].session_soft_limit_usd; 0 = no limit
```

In `New`, after the existing `m.status.model = ...` / `m.status.provider = ...` lines, add:
```go
	m.budgetLimit = d.BudgetLimitUSD
	m.status.costLimit = d.BudgetLimitUSD
```

Add the `addTurnCost` method (place it after `New`; ensure `fmt` is in the import block — add it if missing):
```go
// addTurnCost adds a finished turn's LLM cost to the running session total,
// syncs the status bar, and appends a one-time warning once the soft budget
// limit is crossed (budgetLimit 0 = no limit, so no warning).
func (m *Model) addTurnCost(u llm.Usage) {
	m.sessionCost += m.models.CostUSD(u)
	m.status.cost = m.sessionCost
	if m.budgetLimit > 0 && m.sessionCost > m.budgetLimit && !m.budgetWarned {
		m.budgetWarned = true
		m.chat.appendError(fmt.Sprintf(
			"budget: session cost $%.2f exceeded the $%.2f soft limit",
			m.sessionCost, m.budgetLimit))
	}
}
```

- [ ] **Step 9: Call `addTurnCost` from the `TurnDoneMsg` handler** — In `internal/tui/app.go`, the `case agent.TurnDoneMsg:` handler. Add the `addTurnCost` call so it reads:

```go
	case agent.TurnDoneMsg:
		m.running = false
		m.turnCancel = nil
		m.thinking.stop()
		m.cmdbar.setRunning(false)
		m.addTurnCost(msg.Usage)
		return m, m.waitForBus()
```

- [ ] **Step 10: Run the tests to verify they pass**

Run: `gofmt -w internal/tui/ && go test ./internal/tui/ && go build ./... && gofmt -l internal/tui/`
Expected: PASS; `gofmt -l` prints nothing.

- [ ] **Step 11: Commit**

```bash
git add internal/tui/statusbar.go internal/tui/statusbar_test.go internal/tui/app.go internal/tui/app_test.go
git commit -m "$(printf 'feat: track session LLM cost with a soft-limit warning\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 4: Webhook wiring

**Files:** Modify `internal/tools/lab/tools.go`, `internal/tools/lab/tools_test.go`, `internal/tui/app.go`, `cmd/proteus/main.go`.

### 4a — `lab.submit_experiment` default webhook URL

- [ ] **Step 1: Write the failing test** — Append to `internal/tools/lab/tools_test.go`:

```go
func TestSubmitExperimentToolDefaultsWebhookURL(t *testing.T) {
	tool := &submitExperimentTool{defaultWebhookURL: "https://example.test/webhooks/adaptyv"}

	// Caller omits webhook_url → the configured default fills it in.
	var req SubmitRequest
	if err := json.Unmarshal([]byte(`{"target_id":"t","assay_type":"a","sequences":[]}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.WebhookURL == "" {
		req.WebhookURL = tool.defaultWebhookURL
	}
	if req.WebhookURL != "https://example.test/webhooks/adaptyv" {
		t.Errorf("default not applied: %q", req.WebhookURL)
	}

	// A caller-supplied webhook_url is preserved.
	var req2 SubmitRequest
	if err := json.Unmarshal([]byte(`{"target_id":"t","assay_type":"a","sequences":[],"webhook_url":"https://caller.test/cb"}`), &req2); err != nil {
		t.Fatal(err)
	}
	if req2.WebhookURL == "" {
		req2.WebhookURL = tool.defaultWebhookURL
	}
	if req2.WebhookURL != "https://caller.test/cb" {
		t.Errorf("caller URL overwritten: %q", req2.WebhookURL)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tools/lab/ -run TestSubmitExperimentToolDefaultsWebhookURL`
Expected: FAIL — `unknown field defaultWebhookURL in struct literal`

- [ ] **Step 3: Add `defaultWebhookURL` to the tool** — In `internal/tools/lab/tools.go`:

Add the field to the struct:
```go
type submitExperimentTool struct {
	c                 *Client
	st                *store.Store
	defaultWebhookURL string
}
```

Change the constructor:
```go
// NewSubmitExperimentTool returns the lab.submit_experiment tool. It persists a
// domain.Experiment to st on every successful submission. defaultWebhookURL is
// the Adaptyv callback URL used when a submission omits its own webhook_url.
func NewSubmitExperimentTool(c *Client, st *store.Store, defaultWebhookURL string) *submitExperimentTool {
	return &submitExperimentTool{c: c, st: st, defaultWebhookURL: defaultWebhookURL}
}
```

In `Execute`, immediately after `json.Unmarshal(input, &req)` succeeds (before the `t.c.SubmitExperiment` call), add:
```go
	if req.WebhookURL == "" {
		req.WebhookURL = t.defaultWebhookURL
	}
```

- [ ] **Step 4: Update the other `NewSubmitExperimentTool` test call sites** — In `internal/tools/lab/tools_test.go` there are three more calls of `NewSubmitExperimentTool(c, st)` (around lines 36, 52, 59 and one near line 168). Add a `""` third argument to **every** `NewSubmitExperimentTool(...)` call in the file: `NewSubmitExperimentTool(c, st)` → `NewSubmitExperimentTool(c, st, "")`. Grep the file for `NewSubmitExperimentTool(` to be sure none are missed.

- [ ] **Step 5: Run the lab tests to verify they pass**

Run: `go test ./internal/tools/lab/`
Expected: PASS

### 4b — TUI: `Deps.WebhookURL` and the submit modal

- [ ] **Step 6: Wire the webhook URL through the TUI** — In `internal/tui/app.go`:

Add a field to the `Model` struct (next to the `budgetLimit` field added in Task 3):
```go
	webhookURL string // Adaptyv callback URL shown in the submit modal
```

Add a field to the `Deps` struct (next to `WebhookPort`):
```go
	WebhookURL string // Adaptyv callback URL (config-derived)
```

In `New`, after the `m.status.costLimit = ...` line from Task 3, add:
```go
	m.webhookURL = d.WebhookURL
```

Change `buildSubmitModal` to take the fallback URL. Its signature and the fallback line become:
```go
// buildSubmitModal parses a lab.submit_experiment tool input into the rich
// confirmation overlay (SPECS §12.2). defaultURL is shown when the request
// carries no webhook_url of its own.
func buildSubmitModal(input json.RawMessage, defaultURL string) submitModal {
	var req lab.SubmitRequest
	_ = json.Unmarshal(input, &req)
	seqs := make([]string, 0, len(req.Sequences))
	for _, s := range req.Sequences {
		seqs = append(seqs, s.Sequence)
	}
	url := req.WebhookURL
	if url == "" {
		url = defaultURL
	}
	return submitModal{
		TargetName: orDash(req.TargetID),
		AssayType:  orDash(req.AssayType),
		Sequences:  seqs,
		WebhookURL: url,
	}
}
```

Update the single `buildSubmitModal` call site (in the `agent.ConfirmRequestMsg` handler) from `buildSubmitModal(m.pendingInput)` to:
```go
		m.submit = buildSubmitModal(m.pendingInput, m.webhookURL)
```

- [ ] **Step 7: Run the TUI build to verify it compiles**

Run: `go build ./internal/tui/ && go test ./internal/tui/`
Expected: PASS

### 4c — `cmd/proteus/main.go`: wire `[webhook]` and `[budget]`

- [ ] **Step 8: Wire main.go** — In `cmd/proteus/main.go`:

In `buildRegistry`, the `lab.submit_experiment` registration currently reads:
```go
	registry.Register(lab.NewSubmitExperimentTool(labClient, st))
```
Change it to pass the config-derived webhook URL:
```go
	registry.Register(lab.NewSubmitExperimentTool(labClient, st, cfg.Webhook.EffectiveURL()))
```
(`buildRegistry` already receives `cfg config.Config` — added in SP2.)

In `runTUI`, the `tui.New(tui.Deps{...})` literal currently reads:
```go
	app := tui.New(tui.Deps{
		Registry:     registry,
		Models:       models,
		SystemPrompt: agent.SystemPrompt,
		Store:        st,
		Jobs:         mgr,
		Local:        localReg,
		ProteusHome:  home,
		WebhookPort:  9876,
	})
```
Replace it with (env-free; values come from `cfg`, already loaded in `runTUI` by SP2):
```go
	webhookPort := 0
	if cfg.Webhook.Enabled {
		webhookPort = cfg.Webhook.Port
	}
	app := tui.New(tui.Deps{
		Registry:       registry,
		Models:         models,
		SystemPrompt:   agent.SystemPrompt,
		Store:          st,
		Jobs:           mgr,
		Local:          localReg,
		ProteusHome:    home,
		WebhookPort:    webhookPort,
		WebhookURL:     cfg.Webhook.EffectiveURL(),
		BudgetLimitUSD: cfg.Budget.SessionSoftLimitUSD,
	})
```

- [ ] **Step 9: Run the full build and test suite**

Run: `gofmt -w internal/ cmd/ pkg/ && go build ./... && go test ./... && go vet ./... && gofmt -l internal/ cmd/ pkg/`
Expected: PASS — all packages, vet clean, `gofmt -l` prints nothing.

- [ ] **Step 10: Commit**

```bash
git add internal/tools/lab/tools.go internal/tools/lab/tools_test.go internal/tui/app.go cmd/proteus/main.go
git commit -m "$(printf 'feat: wire config.toml [webhook] into the receiver and submit flow\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Final verification

- [ ] `go build ./... && go test ./... && go vet ./...` — all green.
- [ ] `gofmt -l internal/ cmd/ pkg/` — prints nothing.
- [ ] Delete `~/.config/proteus/config.toml`, launch `proteus` once; confirm the regenerated file contains `[webhook]` and `[budget]`.
- [ ] Set `[webhook].enabled = false`, relaunch; confirm the webhook receiver does not bind port 9876 (`ss -ltn | grep 9876` shows nothing).
- [ ] Set `[budget].wetlab_requires_confirmation = false`, relaunch; confirm startup fails with a clear error.
- [ ] With a priced model active, run a turn; confirm the status bar `$cost` increments past `$0.00`.
