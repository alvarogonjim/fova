# fova — Agent Grounding Fixes

**Spec date:** 2026-05-24
**Status:** Implementation-ready
**Author:** Alvaro (brainstormed with Claude Code)
**Scope:** `internal/agent/loop.go`, `internal/llm/{provider,openai,anthropic,google}.go`, `internal/assets/embed/system.md`

## 1. Summary

A live run against `Qwen3.6-27B-FP8` on `dev` (2026-05-24) revealed seven agent-quality issues. None are perf-related — they all degrade the agent's ability to ground decisions in tool output and avoid runaway behaviour. They cluster naturally as one "agent grounding fixes" batch because they share validation surface (the loop + providers + system prompt) and ship as one PR.

Seven items:

1. **Send structured tool Output to the model**, not just the Display summary. Today the agent sees `"pdb_search: 25 of 5301 hits (top: 1WXB)"` and has no other IDs to pick from — falls back to hallucinating IDs from training memory.
2. **Wire `ChatRequest.Temperature` through every provider** and default to 0.2 for tool-use turns. Currently the field exists but no provider reads it, so every turn ships at SDK-default temperature (1.0 for both Anthropic and OpenAI).
3. **Tool-grounding directive in the system prompt**: explicit "use tool output, never invent identifiers" rule.
4. **Max-iterations guard** on the agent loop (`for {}` with no bound). Bound at 25 iterations per turn.
5. **Respect `StopReason`** — reject turns where the model was truncated (`max_tokens`, `length`, `content_filter`) instead of dispatching potentially-malformed tool calls.
6. **Consistent `MaxTokens` default** of 4096 across providers (Anthropic has it; OpenAI/vLLM don't).
7. **Tool-preference prompt directive**: prefer `knowledge.*` / `fs.read` over `fs.bash` when a specialized tool fits.

## 2. Current behaviour and evidence

### 2.1 Tool Output never reaches the model

`internal/agent/loop.go:172-213` — `executeTool` returns `res.Display`, the loop calls `l.session.AddToolResult(tc.ID, display)`. `tools.Result.Output` (the structured JSON payload — e.g. `pdb_search`'s 25-hit list with titles, methods, resolutions) is built but **never sent to the model**. Every tool with a list-style result has this problem: `pdb_search`, `knowledge.openalex`, `knowledge.crossref`, `knowledge.s2`, `jobs.list`, `lab.targets_search`, etc.

**Observed**: the agent ran `pdb_search "epidermal growth factor receptor structure"` → got `top: 1WXB`, then called `knowledge.pdb` with `5ATH`, `4K0M`, `4H13`, `3N9A`, `4HYY`, `1X77`, `4N3O`, `4HIW` — none EGFR. Eventually landed `1IVO` by luck. The model couldn't see the other 24 hits; it had `1WXB` and then guessed.

### 2.2 `ChatRequest.Temperature` is dead code

Defined at `internal/llm/provider.go:31`. Grep for "Temperature" in `anthropic.go`, `openai.go`, `google.go` returns the type definition only — no provider passes the field through to the SDK. Both Anthropic and OpenAI SDKs default to `temperature: 1.0` when none is provided. Every fova turn ships at 1.0.

This compounds #2.1 — high temperature + sparse tool grounding is the recipe for hallucinated identifiers.

### 2.3 No max-iterations guard

`internal/agent/loop.go:117` is `for { ... }` with no counter. If the model keeps emitting tool calls — as observed in the EGFR run — the loop never exits except via context cancellation or the model voluntarily stopping. Real-world impact: token spend, time, and a stuck UI for any user who walks away from the terminal.

### 2.4 `StopReason` ignored

`loop.go:158` captures `resp.StopReason = ev.StopReason`. It is never read. If the model returns `stop_reason: "max_tokens"` mid-tool-call, `json.Unmarshal(tc.Input)` may succeed on truncated JSON (some shapes parse silently) — the loop dispatches a tool call with wrong arguments.

### 2.5 Inconsistent `MaxTokens` defaults

`internal/llm/anthropic.go:11`: `const defaultAnthropicMaxTokens = 4096`. Used at line 39-42 when `req.MaxTokens == 0`. OpenAI/vLLM provider (`openai.go:74-75`) only sets MaxTokens if `req.MaxTokens > 0` — otherwise uses the server's own default (could be 16384, could be unbounded). vLLM specifically inherits whatever `--max-model-len` was set at server startup.

### 2.6 `fs.bash` chosen when a specialized tool would do

`fs.bash` itself is well-guarded (allowlist of `ls cat grep sed awk jq python3 git curl wget`, deny-tokens for `rm -rf`, etc.). The issue is the model reaches for it when `knowledge.web_fetch` / `knowledge.pdb` / `fs.read` would do the same job better. The system prompt doesn't currently steer this.

## 3. Goals / non-goals

**Goals**
- Model sees structured tool data, not just summaries, when the data is compact (<8KB).
- Tool-use turns run at temperature 0.2 by default (vs SDK-default 1.0).
- Agent loop cannot exceed 25 iterations per turn.
- Truncated turns surface as `TurnErrorMsg` instead of corrupted tool calls.
- All providers share a 4096 max-tokens default.
- System prompt has two explicit grounding directives (use tool output; prefer specialized tools).

**Non-goals**
- Per-call temperature override via config or slash command (could come later; this batch sets a default).
- Per-tool payload customization (e.g. some tools wanting always-Output regardless of size). Single threshold is the v1.
- Streaming-deferred design fix for OpenAI/vLLM (`openai.go` is non-streaming today — known separate issue).
- Cost-based loop termination (budget gate exists separately in `m.budgetLimit`; this batch only adds iteration-based termination).
- Restructuring `tools.Result` to add a third field (`AgentResult`). The Smart-payload approach uses existing `Output`/`Display`; tools don't change.

## 4. Design — #1 Smart payload selection

### 4.1 Helper in `internal/agent/`

Add `modelPayload(r tools.Result) string`:

```go
// maxModelPayloadBytes caps the size of structured tool Output sent to the
// model. Above this threshold the model sees the human-readable Display
// summary instead, to avoid crowding out conversation history. 8KB fits
// every list-style result currently in fova (pdb_search ~5KB, jobs.list
// ~7KB worst case) while keeping corpus_map / web_fetch dumps summarized.
const maxModelPayloadBytes = 8 * 1024

// modelPayload picks what the model sees from a tool result. Prefers the
// structured Output when present and within the size budget; otherwise
// falls back to the human-readable Display.
func modelPayload(r tools.Result) string {
    if len(r.Output) > 0 && len(r.Output) <= maxModelPayloadBytes {
        return string(r.Output)
    }
    return r.Display
}
```

### 4.2 `executeTool` change

Replace the success-path return value:

```go
res, err := l.registry.Execute(ctx, tc.Name, input)
if err != nil {
    msg := "error: " + err.Error()
    l.bus <- ToolDoneMsg{ID: tc.ID, Name: tc.Name, Display: msg, Err: err}
    return msg
}
payload := modelPayload(res)
l.bus <- ToolDoneMsg{ID: tc.ID, Name: tc.Name, Display: res.Display}  // chat stays Display
return payload  // model sees payload
```

**The chat trace keeps Display.** `ToolDoneMsg.Display` is what `appendToolDone` renders in the chat pane — humans want the summary, not the raw JSON. The model gets the structured payload; the user gets the readable line.

### 4.3 Tests

In `internal/agent/loop_test.go`:

- `TestModelPayloadPrefersOutput` — fake tool with `Output: []byte(\`{"id":"X"}\`)`, `Display: "top: X"`; assert the session's tool-result message contains `{"id":"X"}`.
- `TestModelPayloadFallsBackToDisplayOverThreshold` — fake tool with `Output: <10KB of bytes>`, `Display: "summary"`; assert the session sees `"summary"`.
- `TestModelPayloadFallsBackToDisplayWhenOutputEmpty` — fake tool with `Output: nil`, `Display: "x"`; assert the session sees `"x"`.

## 5. Design — #2 Wire Temperature through providers

### 5.1 Constants in agent loop

```go
// defaultTemperature is fova's tool-use default — low enough to keep tool
// invocations deterministic, high enough that planning/brainstorming turns
// are not sterile. Callers may override via ChatRequest.Temperature.
const defaultTemperature = 0.2
```

### 5.2 `Loop.Run` sets defaults when caller didn't

```go
req := llm.ChatRequest{
    Model:       l.model,
    System:      l.session.SystemPrompt(),
    Messages:    l.session.Messages(),
    Tools:       l.registry.Specs(),
    Temperature: defaultTemperature,
    MaxTokens:   defaultMaxTokens, // see §7
}
```

If the loop ever needs per-call override, it's a one-field change at this site.

### 5.3 Provider wiring

**Anthropic** (`anthropic.go`, in the request builder ~line 60-70):
```go
params := anthropic.MessageNewParams{
    // ... existing ...
}
if req.Temperature > 0 {
    params.Temperature = anthropic.Float(float64(req.Temperature))
}
```

**OpenAI/vLLM** (`openai.go`, near the `MaxTokens` plumbing at line 74):
```go
if req.Temperature > 0 {
    params.Temperature = openai.Float(float64(req.Temperature))
}
```

**Google**: `google.go` is a 5-line wrapper that constructs an OpenAI provider against `generativelanguage.googleapis.com/v1beta/openai/` (Google's OpenAI-compat endpoint). Wiring Temperature in the OpenAI provider above covers Google for free; no edit to `google.go`.

### 5.4 Tests

One per provider:

```go
func TestAnthropicProviderPassesTemperature(t *testing.T) {
    // Stand up an httptest server that captures the request body; build
    // an anthropicProvider against it; call Chat with Temperature=0.3;
    // unmarshal the captured body; assert temperature == 0.3.
}
```

(`internal/llm/anthropic.go` and `openai.go` likely already have httptest-based tests for other behaviour; follow that pattern.)

## 6. Design — #3 + #7 System-prompt directives

In `internal/assets/embed/system.md`, in the "## Tool usage" block (currently around line 40), insert two new bullets at the top:

```markdown
- **Use tool output, never invent identifiers.** When a tool returns IDs
  (PDB codes, UniProt accessions, paper DOIs, job IDs), pick ONLY from
  those IDs. Never recall or guess identifiers from memory — they are
  almost always wrong.
- **Prefer specialized tools over general ones.** Use `knowledge.*`,
  `fs.read`, `viz.*` when one fits. Reach for `fs.bash` only for repo
  file/shell work the specialized tools don't cover.
```

Test in `internal/agent/prompts_test.go`:

```go
func TestSystemPromptContainsGroundingDirectives(t *testing.T) {
    // Assuming a helper exists to render the prompt with an empty catalogue;
    // if not, use BuildSystemPrompt(nil, embedded_template).
    p := BuildSystemPrompt(nil, systemMD)
    for _, want := range []string{
        "never invent identifiers",
        "Prefer specialized tools",
    } {
        if !strings.Contains(p, want) {
            t.Errorf("system prompt missing %q", want)
        }
    }
}
```

## 7. Design — #4 Max-iterations guard

### 7.1 Constant + field

```go
// defaultMaxIterations bounds one turn at 25 LLM round-trips. A well-formed
// turn finishes in 2-6 (plan → call tools → answer). Spinning past 25 is
// almost always a model-confusion loop.
const defaultMaxIterations = 25

type Loop struct {
    // ... existing fields ...
    maxIterations int
}
```

`NewLoopWithGuard` initializes `maxIterations: defaultMaxIterations`. A `Loop.SetMaxIterations(n int)` setter exists for tests; not exposed to users in this batch.

### 7.2 Sentinel + counter in `Run`

```go
var ErrMaxIterations = errors.New("turn exceeded maximum tool-call iterations")

// inside Run:
iterations := 0
for {
    if iterations >= l.maxIterations {
        l.bus <- TurnErrorMsg{Err: fmt.Errorf("%w (%d)", ErrMaxIterations, l.maxIterations)}
        return
    }
    iterations++

    if err := ctx.Err(); err != nil {
        l.bus <- TurnErrorMsg{Err: err}
        return
    }
    // ... existing body ...
}
```

### 7.3 Tests

- `TestLoopExceedingMaxIterationsErrors` — a fake tool that always succeeds + a `mockProvider` that always responds with a fresh `ToolCall`. Set `loop.SetMaxIterations(3)`. Assert `Run` emits `TurnErrorMsg{Err: ErrMaxIterations}` after exactly 3 iterations.
- `TestLoopUnderMaxIterationsCompletes` — same setup but the provider's 3rd response has no ToolCalls. Set max to 5. Assert `Run` completes with `TurnDoneMsg`, not error.

## 8. Design — #5 Respect StopReason

### 8.1 Sentinel + check

```go
var ErrModelTruncated = errors.New("model output truncated; consider raising MaxTokens or simplifying the prompt")

// stopMeansTruncated is true when the model finished because it ran out of
// output tokens or was content-filtered, NOT because it voluntarily stopped.
// Dispatching tool calls from a truncated turn risks acting on partial JSON.
func stopMeansTruncated(stop string) bool {
    switch stop {
    case "max_tokens", "length", "content_filter", "content-filter":
        return true
    }
    return false
}

// inside Run, after the stream loop and flush, before checking ToolCalls:
if stopMeansTruncated(resp.StopReason) {
    l.bus <- TurnErrorMsg{Err: fmt.Errorf("%w (stop_reason=%q)", ErrModelTruncated, resp.StopReason)}
    return
}
```

### 8.2 Stop-reason vocabulary

Anthropic returns `"max_tokens"` (its enum is `end_turn | max_tokens | stop_sequence | tool_use | refusal`). OpenAI returns `"length"` (its enum is `stop | length | content_filter | tool_calls | function_call`). Both forms are covered. `"content_filter"` and `"content-filter"` (case-variant) are also rejected — the content was suppressed, acting on it would be wrong.

### 8.3 Test

`TestLoopRejectsTruncatedTurn` — `mockProvider` returns a response with `StopReason: "max_tokens"` AND a tool call. Assert `Run` emits `TurnErrorMsg{Err: ErrModelTruncated}` and **does not** execute the tool.

## 9. Design — #6 Consistent MaxTokens default

### 9.1 Constant + default in loop

```go
// defaultMaxTokens caps a single LLM response. Mirrors Anthropic's existing
// default; this batch extends it to OpenAI/vLLM so server-side defaults
// (which can be 16k or unbounded) don't allow a runaway response.
const defaultMaxTokens = 4096
```

In `Loop.Run`'s ChatRequest construction (see §5.2 — same edit site):

```go
MaxTokens: defaultMaxTokens,
```

### 9.2 Anthropic provider becomes redundant — but kept

The existing `if req.MaxTokens == 0 { maxTokens = defaultAnthropicMaxTokens }` guard at `anthropic.go:39-42` is now redundant (the loop always sets it). Keep it as a defense-in-depth — third-party callers (tests, future tooling) that construct `ChatRequest` directly should still get a sensible default.

### 9.3 Test

`TestLoopSetsDefaultMaxTokens` — a `mockProvider` that records the `ChatRequest` it received. Call `Run` with a tool-free response. Assert the recorded request has `MaxTokens == defaultMaxTokens`.

## 10. Order of work

All seven items are independent at the file level except #6 piggy-backs on #2's edit site (both modify the `ChatRequest` literal in `Run`). Recommended order:

1. **#3 + #7 system-prompt directives** — one file edit + one prompts_test.go addition. Lowest risk.
2. **#1 smart payload** — `loop.go` + `loop_test.go`. No provider changes.
3. **#2 + #6 ChatRequest defaults + Temperature wiring** — combined because they share the same `req := llm.ChatRequest{...}` literal.
4. **#4 max-iterations guard** — `loop.go` + tests.
5. **#5 respect StopReason** — `loop.go` + tests.

Or land in parallel as worktree-isolated agents — #3+#7 / #1 / #2+#6 / #4 / #5 are five disjoint workstreams.

## 11. Backwards compatibility & risk

- **#1 Smart payload**: model sees more data than before. Worst case is a model that was fine with `Display` summaries gets confused by JSON. Not observed; if it happens, raise the threshold or add a per-tool opt-out.
- **#2 Temperature**: turns drop from temp=1.0 to temp=0.2. Brainstorming-style prose may feel terser. If users complain, raise the default or expose in config.
- **#3 + #7 prompt directives**: additive, two bullets. Adds ~120 tokens to every turn's system message — negligible.
- **#4 max-iterations**: turns capped at 25. A complex turn that legitimately needs more (multi-paper research, multi-job design pipeline) will error out. 25 is generous for v1; widen later if it bites.
- **#5 stop reason**: previously-silent truncations now surface as user-visible errors. The error message says "consider raising MaxTokens" so users have a path forward.
- **#6 MaxTokens 4096**: caps OpenAI/vLLM responses to 4096 tokens. If users had vLLM emitting longer responses, those get truncated — and #5 will catch and report it. Net win.

## 12. Out of scope

- Per-call temperature override (slash command, config).
- Per-tool payload-size override.
- Token-budget-based loop termination.
- Restructuring `tools.Result` for a third "agent-only" payload field.
- Fixing the OpenAI/vLLM "streaming-deferred" design (blocking Chat() then emit-as-one-chunk). Known separate issue.
