# Proteus — Configuration System (SPECS §14) — Design

**Date:** 2026-05-19
**Status:** Approved, SP1 ready for planning
**Source of truth:** `docs/SPECS.md` §14.
**Predecessor:** v0.2 deferred the whole §14 config system; this milestone delivers it.

## 1. Goal

Make Proteus configurable from TOML files instead of hardcoded Go, per SPECS
§14: a `models.toml` model/provider catalog and a `config.toml` settings file
in `~/.config/proteus/`. API keys are never stored in either file — they stay
in environment variables (SPECS §14.3).

## 2. Decomposition

§14 spans the LLM registry, the TUI, knowledge tools, and the cost/webhook
subsystems — too large for one spec. Three sub-projects:

| SP | Delivers | Depends on |
|---|---|---|
| **SP1** | `internal/config/` foundation + `models.toml` (providers + models) | — |
| **SP2** | `config.toml` — `[defaults]`, `[ui]`, `[knowledge]` | SP1 |
| **SP3** | `config.toml` — `[budget]`, `[webhook]` | SP1; cost/webhook subsystems |

This document fully specs **SP1** (§3–§9). SP2 and SP3 are outlined (§10); each
gets its own design pass and plan when reached. Build order SP1 → SP2 → SP3.

## 3. SP1 — scope

A new `internal/config/` package and a data-driven `models.toml` carrying both
**providers** (endpoints) and **models**, replacing the hardcoded `builtinModels`
slice and the hardcoded provider `switch` in `internal/llm`. After SP1 a user
adds a model or an OpenAI-compatible endpoint by editing `models.toml` — no
recompile.

## 4. The `internal/config/` package

**Files:** `internal/config/config.go`, `internal/config/models.toml` (the
embedded default), `internal/config/config_test.go`.

- `ConfigDir() string` — returns `$PROTEUS_CONFIG_DIR` if set, else
  `~/.config/proteus` (SPECS §14.1).
- Types:
  - `Provider struct { Name, Kind, BaseURL, APIKeyEnv string }` — `Kind` is one
    of `anthropic`, `google`, `openai` (`openai` = any OpenAI-compatible
    Chat-Completions endpoint).
  - `Model struct { ID, DisplayName, Provider string; ContextTokens int;
    SupportsTools bool; InputPricePer1M, OutputPricePer1M float64 }`.
  - `Catalog struct { Providers []Provider; Models []Model }`.
- `LoadModels() (Catalog, error)` — reads `<ConfigDir>/models.toml`. If the file
  is absent, it parses the **embedded default**, writes that default to
  `<ConfigDir>/models.toml` (first-run materialization, so the user has a real
  file to edit), and returns it. If the file is present, it is parsed and
  returned.
- The default `models.toml` is embedded with `//go:embed models.toml` (the
  pattern `internal/backends/local/registry.go` already uses for `tools.toml`).
- TOML decoding uses `github.com/BurntSushi/toml` (already a dependency).

## 5. `models.toml` format

```toml
# Proteus model catalog. API keys are NEVER stored here — api_key_env names the
# environment variable Proteus reads the key from at run time.

[[provider]]
name        = "anthropic"
kind        = "anthropic"
api_key_env = "ANTHROPIC_API_KEY"

[[provider]]
name        = "openai"
kind        = "openai"
base_url    = "https://api.openai.com/v1"
api_key_env = "OPENAI_API_KEY"

[[provider]]
name        = "google"
kind        = "google"
api_key_env = "GOOGLE_API_KEY"

[[provider]]
name        = "ollama"
kind        = "openai"
base_url    = "http://localhost:11434/v1"

[[provider]]
name        = "vllm"
kind        = "openai"
base_url    = "http://localhost:8000/v1"
api_key_env = "VLLM_API_KEY"          # optional; an unset/empty key is fine

[[model]]
id             = "Qwen/Qwen3.6-27B"
display_name   = "Qwen3.6 27B (local vLLM)"
provider       = "vllm"
context_tokens = 32768
supports_tools = true
input_price_per_1m  = 0
output_price_per_1m = 0
```

The embedded default reproduces today's catalog exactly: the five providers
above plus the six current models (`claude-opus-4-7`, `claude-sonnet-4-6`,
`gpt-4o`, `gemini-2.0-flash`, `llama3.3:70b`, `Qwen/Qwen3.6-27B`). Adding a model
is one `[[model]]` block; adding an endpoint is one `[[provider]]` block.

## 6. Data-driven providers in `internal/llm`

- `NewModelRegistry()` becomes `NewModelRegistry(cat config.Catalog) *ModelRegistry`
  — it builds its model list from `cat.Models` and keeps `cat.Providers` for
  client construction. The hardcoded `builtinModels` slice is deleted.
- `Provider()` becomes table-driven: resolve the active model's `[[provider]]`
  by name, then build the client by `Kind`:
  - `anthropic` → `NewAnthropicProvider(os.Getenv(p.APIKeyEnv))`
  - `google` → `NewGoogleProvider(os.Getenv(p.APIKeyEnv))`
  - `openai` → `NewOpenAIProvider(p.Name, p.BaseURL, os.Getenv(p.APIKeyEnv))`
  The `anthropic`/`openai`/`google`/`ollama`/`vllm` `switch` is removed, and so
  are the `VLLM_BASE_URL` / `OLLAMA_BASE_URL` env-var URL overrides — the
  endpoint now lives in `base_url` in `models.toml`. (Migration: a user who set
  `VLLM_BASE_URL` edits the `vllm` provider's `base_url` instead.)
- `internal/llm` imports `internal/config` for the `Catalog`/`Provider`/`Model`
  types. `internal/config` imports nothing from `internal/llm` — one-way, no cycle.

## 7. Default model selection

`NewModelRegistry` selects, as the active model, the **first model in
`models.toml` whose provider is ready** — its `APIKeyEnv` is set in the
environment, or it needs no key (empty `APIKeyEnv`, e.g. a local endpoint). If
no model's provider is ready, the first model in the file is selected. (SP2's
`config.toml [defaults].model` will add an explicit override.)

## 8. Wiring & error handling

- `cmd/proteus/main.go` `runTUI`: `cat, err := config.LoadModels()` (fail
  startup loudly on error) → `llm.NewModelRegistry(cat)`.
- A malformed `models.toml` → `LoadModels` returns an error naming the file and
  the parse problem; startup fails — no silent fallback to defaults.
- A `[[model]]` whose `provider` matches no `[[provider]].name` → error at load.
- An empty catalog (no models) → error at load.

## 9. SP1 testing

All offline and deterministic.
- `config_test.go`: the embedded default parses into a non-empty `Catalog`;
  `LoadModels` against a temp `PROTEUS_CONFIG_DIR` materializes the file on
  first run and round-trips a user file on the second; malformed TOML,
  unknown-provider reference, and empty-catalog each error.
- `modelregistry_test.go`: updated for `NewModelRegistry(cat)` — table-driven
  provider construction for each `Kind`; default-model selection picks a
  ready provider.
- Existing `NewModelRegistry()` call sites (`cmd/proteus/main.go`, the
  `internal/tui` tests) are updated to pass a catalog (`config.LoadModels()` in
  production; a small fixture catalog or the embedded default in tests).

**Acceptance:** `models.toml` is created on first run; adding a `[[model]]` or
`[[provider]]` and relaunching surfaces it in `/model` with no rebuild; API keys
are read only from the environment; `go test ./...` and `go vet ./...` pass.

## 10. SP2 / SP3 outline

- **SP2 — `config.toml` for live subsystems.** Same `internal/config` package
  loads `<ConfigDir>/config.toml`: `[defaults]` (provider, model,
  compute_backend) → the model registry and `backends.Select`; `[ui]` (theme,
  inline_graphics) → `internal/tui`; `[knowledge]` (mailto, biorxiv_recent_days,
  corpus_default_max_papers) → `internal/tools/knowledge`. Embedded default +
  first-run materialization, as SP1.
- **SP3 — `[budget]` + `[webhook]`.** `[budget]` (session_soft_limit_usd,
  wetlab_requires_confirmation) and `[webhook]` (enabled, port, public_url).
  `[webhook]` configures the existing `lab.StartReceiver`; `[budget]` needs
  cost-tracking, which is still deferred — SP3 lands with or after it.

## 11. Out of scope (SP1)

`config.toml` and all its sections (SP2/SP3); the OS-keychain key fallback
(SPECS §14.3 "then OS keychain" — env-only for now); `${ENV}` interpolation
inside `models.toml` values; per-project `proteus.toml`.
