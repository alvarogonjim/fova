# Proteus — Config System SP2: `config.toml` — Design

**Date:** 2026-05-19
**Status:** Approved, ready for planning
**Milestone:** Configuration system (SPECS §14) — SP2 of 3
**Parent design:** `docs/superpowers/specs/2026-05-19-proteus-config-system-design.md` (§10 SP2 outline)
**Predecessor:** SP1 (`internal/config` + `models.toml`) — merged to `master`.

## 1. Goal & scope

Add a `config.toml` settings file to the `internal/config` package (alongside
SP1's `models.toml`) and wire its sections into the running subsystems:

- `[defaults]` (provider, model, compute_backend) — **fully wired** this SP.
- `[knowledge]` (mailto, biorxiv_recent_days, corpus_default_max_papers) —
  **fully wired** this SP.
- `[ui]` (theme, inline_graphics) — **parsed, validated, and stored**, but its
  consumers (forced theming, inline graphics) are v0.5 deliverables (SPECS §20
  v0.5: "Themes", `internal/tui/graphics.go`). SP2 surfaces the settings; v0.5
  reads the already-parsed values.

`[webhook]` and `[budget]` stay in SP3. The OS-keychain key fallback stays
deferred (env-only), as in SP1.

## 2. The loader (`internal/config`)

Mirrors SP1's `models.toml` mechanism.

**Files:** `internal/config/config_toml.go` (new), `internal/config/config.toml`
(new, embedded default), `internal/config/config_toml_test.go` (new).

```go
// Config is the parsed config.toml (SPECS §14.2).
type Config struct {
	UI        UIConfig        `toml:"ui"`
	Defaults  DefaultsConfig  `toml:"defaults"`
	Knowledge KnowledgeConfig `toml:"knowledge"`
}

type UIConfig struct {
	Theme          string `toml:"theme"`           // auto | light | dark
	InlineGraphics string `toml:"inline_graphics"` // auto | kitty | sixel | iterm2 | off
}

type DefaultsConfig struct {
	Provider       string `toml:"provider"`        // auto | a provider name
	Model          string `toml:"model"`           // "" → the registry's auto pick
	ComputeBackend string `toml:"compute_backend"` // local | modal
}

type KnowledgeConfig struct {
	Mailto                 string `toml:"mailto"`
	BiorxivRecentDays      int    `toml:"biorxiv_recent_days"`
	CorpusDefaultMaxPapers int    `toml:"corpus_default_max_papers"`
}
```

- `//go:embed config.toml` provides the default; `LoadConfig() (Config, error)`
  reads `<ConfigDir>/config.toml`, writes the embedded default on first run
  (materialization), then parses and validates — exactly the SP1 `LoadModels`
  shape, reusing `ConfigDir()`.
- `Config.validate()` rejects unknown enum values — `Theme` ∉
  {auto, light, dark}, `InlineGraphics` ∉ {auto, kitty, sixel, iterm2, off},
  `ComputeBackend` ∉ {local, modal} — and negative integers. Failure is a clear,
  loud error (no silent fallback), as in SP1.

## 3. `config.toml` format (embedded default)

```toml
[ui]
theme = "auto"               # auto | light | dark
inline_graphics = "auto"     # auto | kitty | sixel | iterm2 | off

[defaults]
provider = "auto"            # auto | a provider name from models.toml
model = ""                   # empty → the registry's automatic pick
compute_backend = "local"    # local | modal

[knowledge]
mailto = ""                  # your email for the OpenAlex polite pool
biorxiv_recent_days = 30
corpus_default_max_papers = 30
```

**Deviation:** SPECS §14.2's sample shows `compute_backend = "modal"`; the
embedded default uses `local` so a fresh install runs without a Modal account.

## 4. Wiring `[defaults]`

- **`compute_backend`** — `cmd/proteus/main.go` resolves the backend as:
  `PROTEUS_COMPUTE_BACKEND` if that env var is set, otherwise
  `cfg.Defaults.ComputeBackend`. The env var **overrides** the file (the
  conventional precedence — a per-invocation escape hatch). The resolved value
  is passed to `backends.Select`.
- **`model` / `provider`** — a new method on the model registry:

  ```go
  // SelectDefault applies config.toml [defaults] to the active model. An empty
  // Model and an "auto"/empty Provider leave the constructor's ready-provider
  // pick untouched.
  func (mr *ModelRegistry) SelectDefault(d config.DefaultsConfig) error
  ```

  Behaviour: if `d.Model != ""`, select it (`SetModel`); else if `d.Provider`
  is set and not `"auto"`, select that provider's first model; otherwise no
  change. An unknown model or provider is an error. `cmd/proteus/main.go` calls
  `mr.SelectDefault(cfg.Defaults)` after `NewModelRegistry(cat)`.
  `NewModelRegistry`'s signature does **not** change — no churn to its 17 call
  sites.

## 5. Wiring `[knowledge]`

`buildRegistry` (in `cmd/proteus/main.go`) gains a `config.Config` parameter and
passes the relevant fields to three knowledge-tool constructors:

- **`mailto`** → `NewOpenAlex` — replaces the hardcoded `"proteus@example.com"`
  in `openalex.go` (the OpenAlex polite-pool address). An empty `mailto` omits
  the `mailto` query parameter entirely.
- **`biorxiv_recent_days`** → `NewBioRxiv` — the default recency window for a
  bioRxiv search.
- **`corpus_default_max_papers`** → `NewCorpus` — the default used when a
  `corpus` tool call omits `max_papers`.

The three constructors (`NewOpenAlex`, `NewBioRxiv`, `NewCorpus`) each gain a
parameter for their setting. The other knowledge constructors are unchanged. A
zero/empty value falls back to the current hardcoded default (mailto omitted;
30 days; 30 papers).

## 6. `[ui]` — parsed, not yet consumed

`[ui]` is decoded into `UIConfig`, validated, and materialized into
`config.toml`. `theme = "auto"` is already the TUI's behaviour (the theme uses
`AdaptiveColor` for light/dark terminals), so SP2 changes no TUI rendering.
Forced `light`/`dark` palettes and `inline_graphics` rendering are v0.5
deliverables; SP2 deliberately stops at storing valid values for them.

## 7. Wiring & error handling

- `cmd/proteus/main.go` `runTUI`: `cfg, err := config.LoadConfig()` (fail
  startup loudly on error) → resolve the compute backend → `buildRegistry(...,
  cfg)` → `mr.SelectDefault(cfg.Defaults)`.
- A malformed `config.toml`, an unknown enum value, or a `SelectDefault` against
  a model/provider absent from `models.toml` each yield a clear error.

## 8. Testing

All offline and deterministic.
- `config_toml_test.go`: the embedded default parses; `LoadConfig` materializes
  on first run and round-trips on the second; malformed TOML and each invalid
  enum are rejected.
- `internal/llm`: `SelectDefault` — explicit model override, provider override,
  `auto`/empty no-op, and unknown-model/provider errors.
- `internal/tools/knowledge`: `NewOpenAlex` with a mailto includes it (and omits
  it when empty); `NewBioRxiv`/`NewCorpus` honour their configured defaults.
- `cmd/proteus/main_test.go`: updated for the new `buildRegistry` signature.

**Acceptance:** `config.toml` is created on first run; editing
`[defaults].model` changes the model selected at launch; `[defaults].compute_backend`
selects the backend unless `PROTEUS_COMPUTE_BACKEND` overrides it;
`[knowledge].mailto` reaches OpenAlex requests; `go test ./...` and
`go vet ./...` pass.

## 9. Files

- Create: `internal/config/config_toml.go`, `internal/config/config.toml`,
  `internal/config/config_toml_test.go`.
- Modify: `internal/llm/modelregistry.go` (+ `modelregistry_test.go`) — add
  `SelectDefault`.
- Modify: `cmd/proteus/main.go` (load config, resolve backend, wire) and
  `cmd/proteus/main_test.go` (`buildRegistry` signature).
- Modify: `internal/tools/knowledge/openalex.go`, `biorxiv.go`, `corpus.go`
  (+ their tests) — constructors take their setting.

## 10. Out of scope

`[webhook]` and `[budget]` (SP3); forced light/dark theming and inline graphics
(v0.5); the OS-keychain key fallback; per-project `proteus.toml`; `${ENV}`
interpolation in `config.toml`.
