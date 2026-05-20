# Config System SP2 — `config.toml` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `config.toml` settings file and wire `[defaults]` and `[knowledge]` into the running subsystems.

**Architecture:** A `config.toml` loader in `internal/config` (mirrors SP1's `models.toml`). `[defaults].compute_backend` feeds `backends.Select` (env var overrides it); `[defaults].model`/`provider` apply via a new `ModelRegistry.SelectDefault`; `[knowledge]` fields feed the OpenAlex/bioRxiv/corpus tool constructors. `[ui]` is parsed and validated but not yet consumed (v0.5).

**Tech Stack:** Go, `github.com/BurntSushi/toml`, `//go:embed`.

**Spec:** `docs/superpowers/specs/2026-05-19-proteus-config-toml-sp2-design.md`.

**Branch:** `feat/config-toml-sp2` (already created).

---

## File map

| File | Change | Task |
|---|---|---|
| `internal/config/config.toml` | **new** — embedded default | 1 |
| `internal/config/config_toml.go` | **new** — `Config` types, `LoadConfig`, `DefaultConfig` | 1 |
| `internal/config/config_toml_test.go` | **new** — loader tests | 1 |
| `internal/llm/modelregistry.go` (+ `_test.go`) | modify — add `SelectDefault` | 2 |
| `internal/tools/knowledge/openalex.go`, `biorxiv.go`, `corpus.go` (+ their `_test.go`) | modify — constructors take a config value | 3 |
| `cmd/proteus/main.go` | modify — load `config.toml`, wire `[defaults]`/`[knowledge]` | 3 |
| `cmd/proteus/main_test.go` | modify — `buildRegistry` signature | 3 |

Tasks are **sequential** (1 → 2 → 3): Task 2 needs Task 1's `config.DefaultsConfig`; Task 3 needs both.

---

## Task 1: `config.toml` loader

**Files:** Create `internal/config/config.toml`, `internal/config/config_toml.go`, `internal/config/config_toml_test.go`.

- [ ] **Step 1: Create the embedded default `internal/config/config.toml`** with exactly this content:

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

- [ ] **Step 2: Write the failing tests** — Create `internal/config/config_toml_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigParses(t *testing.T) {
	c := DefaultConfig()
	if c.UI.Theme == "" || c.Defaults.ComputeBackend == "" {
		t.Fatalf("default config has empty fields: %+v", c)
	}
}

func TestParseConfigRejectsBadTheme(t *testing.T) {
	in := `
[ui]
theme = "neon"
inline_graphics = "auto"
[defaults]
compute_backend = "local"
`
	if _, err := parseConfig(in); err == nil {
		t.Fatal("expected an error for an invalid ui.theme")
	}
}

func TestParseConfigRejectsBadBackend(t *testing.T) {
	in := `
[ui]
theme = "auto"
inline_graphics = "auto"
[defaults]
compute_backend = "cloud"
`
	if _, err := parseConfig(in); err == nil {
		t.Fatal("expected an error for an invalid compute_backend")
	}
}

func TestLoadConfigMaterializesAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROTEUS_CONFIG_DIR", dir)
	if _, err := LoadConfig(); err != nil { // first run materializes
		t.Fatalf("first LoadConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil {
		t.Errorf("config.toml not written on first run: %v", err)
	}
	c, err := LoadConfig() // second run reads the materialized file
	if err != nil {
		t.Fatalf("second LoadConfig: %v", err)
	}
	if c.Defaults.ComputeBackend == "" {
		t.Fatal("round-tripped config is empty")
	}
}

func TestLoadConfigRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROTEUS_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("bad ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected an error for a malformed config.toml")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/config/ -run 'Config'`
Expected: FAIL — `undefined: DefaultConfig` / `parseConfig` / `LoadConfig`

- [ ] **Step 4: Create `internal/config/config_toml.go`**

```go
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

//go:embed config.toml
var defaultConfigTOML string

// UIConfig is the [ui] section of config.toml. Its consumers (theming, inline
// graphics) are v0.5 deliverables; SP2 only parses and validates these values.
type UIConfig struct {
	Theme          string `toml:"theme"`
	InlineGraphics string `toml:"inline_graphics"`
}

// DefaultsConfig is the [defaults] section of config.toml.
type DefaultsConfig struct {
	Provider       string `toml:"provider"`
	Model          string `toml:"model"`
	ComputeBackend string `toml:"compute_backend"`
}

// KnowledgeConfig is the [knowledge] section of config.toml.
type KnowledgeConfig struct {
	Mailto                 string `toml:"mailto"`
	BiorxivRecentDays      int    `toml:"biorxiv_recent_days"`
	CorpusDefaultMaxPapers int    `toml:"corpus_default_max_papers"`
}

// Config is the parsed config.toml (SPECS §14.2).
type Config struct {
	UI        UIConfig        `toml:"ui"`
	Defaults  DefaultsConfig  `toml:"defaults"`
	Knowledge KnowledgeConfig `toml:"knowledge"`
}

var (
	validThemes   = map[string]bool{"auto": true, "light": true, "dark": true}
	validGraphics = map[string]bool{"auto": true, "kitty": true, "sixel": true, "iterm2": true, "off": true}
	validBackends = map[string]bool{"local": true, "modal": true}
)

// validate checks config.toml's enum and integer fields.
func (c Config) validate() error {
	if !validThemes[c.UI.Theme] {
		return fmt.Errorf("ui.theme %q must be auto|light|dark", c.UI.Theme)
	}
	if !validGraphics[c.UI.InlineGraphics] {
		return fmt.Errorf("ui.inline_graphics %q must be auto|kitty|sixel|iterm2|off", c.UI.InlineGraphics)
	}
	if c.Defaults.ComputeBackend != "" && !validBackends[c.Defaults.ComputeBackend] {
		return fmt.Errorf("defaults.compute_backend %q must be local|modal", c.Defaults.ComputeBackend)
	}
	if c.Knowledge.BiorxivRecentDays < 0 {
		return fmt.Errorf("knowledge.biorxiv_recent_days must not be negative")
	}
	if c.Knowledge.CorpusDefaultMaxPapers < 0 {
		return fmt.Errorf("knowledge.corpus_default_max_papers must not be negative")
	}
	return nil
}

// parseConfig decodes config.toml text and validates it.
func parseConfig(text string) (Config, error) {
	var c Config
	if _, err := toml.Decode(text, &c); err != nil {
		return Config{}, fmt.Errorf("parse config.toml: %w", err)
	}
	if err := c.validate(); err != nil {
		return Config{}, fmt.Errorf("config.toml: %w", err)
	}
	return c, nil
}

// DefaultConfig returns the config embedded in the binary (no disk access).
func DefaultConfig() Config {
	c, err := parseConfig(defaultConfigTOML)
	if err != nil {
		panic("embedded config.toml is invalid: " + err.Error())
	}
	return c
}

// LoadConfig loads config.toml from <ConfigDir>. If the file does not exist,
// the embedded default is written there (first-run materialization) and
// returned. A malformed or invalid file is an error.
func LoadConfig() (Config, error) {
	dir := ConfigDir()
	path := filepath.Join(dir, "config.toml")
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Config{}, fmt.Errorf("create config dir %s: %w", dir, err)
		}
		if err := os.WriteFile(path, []byte(defaultConfigTOML), 0o644); err != nil {
			return Config{}, fmt.Errorf("write default %s: %w", path, err)
		}
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	c, err := parseConfig(string(body))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/config/ && go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.toml internal/config/config_toml.go internal/config/config_toml_test.go
git commit -m "$(printf 'feat: add the config.toml loader\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 2: `ModelRegistry.SelectDefault`

**Files:** Modify `internal/llm/modelregistry.go`, `internal/llm/modelregistry_test.go`.

- [ ] **Step 1: Write the failing tests** — Append to `internal/llm/modelregistry_test.go`:

```go
func TestModelRegistrySelectDefaultModel(t *testing.T) {
	mr := NewModelRegistry(config.DefaultCatalog())
	if err := mr.SelectDefault(config.DefaultsConfig{Model: "gpt-4o"}); err != nil {
		t.Fatalf("SelectDefault: %v", err)
	}
	if mr.ActiveModel() != "gpt-4o" {
		t.Errorf("active model = %q, want gpt-4o", mr.ActiveModel())
	}
}

func TestModelRegistrySelectDefaultProvider(t *testing.T) {
	mr := NewModelRegistry(config.DefaultCatalog())
	if err := mr.SelectDefault(config.DefaultsConfig{Provider: "google"}); err != nil {
		t.Fatalf("SelectDefault: %v", err)
	}
	if mr.ActiveProviderName() != "google" {
		t.Errorf("active provider = %q, want google", mr.ActiveProviderName())
	}
}

func TestModelRegistrySelectDefaultUnknown(t *testing.T) {
	mr := NewModelRegistry(config.DefaultCatalog())
	if err := mr.SelectDefault(config.DefaultsConfig{Model: "no-such-model"}); err == nil {
		t.Error("expected an error for an unknown default model")
	}
	if err := mr.SelectDefault(config.DefaultsConfig{Provider: "no-such-provider"}); err == nil {
		t.Error("expected an error for an unknown default provider")
	}
}

func TestModelRegistrySelectDefaultAutoIsNoop(t *testing.T) {
	mr := NewModelRegistry(config.DefaultCatalog())
	before := mr.ActiveModel()
	if err := mr.SelectDefault(config.DefaultsConfig{Provider: "auto"}); err != nil {
		t.Fatalf("SelectDefault: %v", err)
	}
	if mr.ActiveModel() != before {
		t.Errorf("auto must not change the active model: %q -> %q", before, mr.ActiveModel())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/llm/ -run TestModelRegistrySelectDefault`
Expected: FAIL — `mr.SelectDefault undefined`

- [ ] **Step 3: Add `SelectDefault` to `internal/llm/modelregistry.go`**

Append to the file (`config` and `fmt` are already imported):

```go
// SelectDefault applies config.toml [defaults] to the active model: an explicit
// Model wins; otherwise a named (non-"auto") Provider selects its first model;
// an empty or "auto" Provider leaves the constructor's ready-provider pick. An
// unknown model or provider is an error.
func (mr *ModelRegistry) SelectDefault(d config.DefaultsConfig) error {
	if d.Model != "" {
		return mr.SetModel(d.Model)
	}
	if d.Provider != "" && d.Provider != "auto" {
		for _, m := range mr.models {
			if m.ProviderName == d.Provider {
				return mr.SetModel(m.ID)
			}
		}
		return fmt.Errorf("config default provider %q has no models in models.toml", d.Provider)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/llm/ && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/llm/modelregistry.go internal/llm/modelregistry_test.go
git commit -m "$(printf 'feat: add ModelRegistry.SelectDefault for config.toml [defaults]\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 3: Wire `[defaults]` and `[knowledge]` into the subsystems

The three knowledge constructors gain a parameter; `buildRegistry` gains a `config.Config` parameter. All call sites update in this one task so the build stays green.

**Files:** Modify `internal/tools/knowledge/openalex.go`, `biorxiv.go`, `corpus.go`, `openalex_test.go`, `biorxiv_test.go`, `corpus_test.go`; `cmd/proteus/main.go`; `cmd/proteus/main_test.go`.

- [ ] **Step 1: OpenAlex — make `mailto` configurable**

In `internal/tools/knowledge/openalex.go`, add a `Mailto` field to the struct:

```go
type OpenAlex struct {
	BaseURL string
	Mailto  string
	results *Results
}
```

Change the constructor:

```go
// NewOpenAlex builds the knowledge.openalex tool. mailto, when non-empty, is
// sent to the OpenAlex polite pool.
func NewOpenAlex(r *Results, mailto string) *OpenAlex {
	return &OpenAlex{BaseURL: openAlexEndpoint, Mailto: mailto, results: r}
}
```

In `Execute`, replace the hardcoded line `q.Set("mailto", "proteus@example.com")` with:

```go
	if t.Mailto != "" {
		q.Set("mailto", t.Mailto)
	}
```

- [ ] **Step 2: bioRxiv — make the recency window configurable**

In `internal/tools/knowledge/biorxiv.go`, add a `RecentDays` field:

```go
type BioRxiv struct {
	BaseURL    string
	RecentDays int
	results    *Results
}
```

Change the constructor:

```go
// NewBioRxiv builds the knowledge.biorxiv tool. recentDays is the default
// look-back window; a value <= 0 falls back to 30.
func NewBioRxiv(r *Results, recentDays int) *BioRxiv {
	return &BioRxiv{BaseURL: bioRxivEndpoint, RecentDays: recentDays, results: r}
}
```

In `Execute`, replace `from = toTime.AddDate(0, 0, -30).Format("2006-01-02")` with:

```go
		days := t.RecentDays
		if days <= 0 {
			days = 30
		}
		from = toTime.AddDate(0, 0, -days).Format("2006-01-02")
```

- [ ] **Step 3: Corpus — make the default `max_papers` configurable**

In `internal/tools/knowledge/corpus.go`, add a `DefaultMaxPapers` field to the `Corpus` struct (next to `indexDir`):

```go
	indexDir         string
	defaultMaxPapers int
```

Change the constructor:

```go
// NewCorpus builds the knowledge.corpus tool. indexDir is where the bleve
// full-text index lives; defaultMaxPapers is the cap used when a call omits
// max_papers (a value <= 0 falls back to 30).
func NewCorpus(st *store.Store, results *Results, mapper Mapper, indexDir string, defaultMaxPapers int) *Corpus {
	c := &Corpus{st: st, results: results, mapper: mapper, indexDir: indexDir, defaultMaxPapers: defaultMaxPapers}
	c.FetchText = c.fetchTextEuropePMC
	return c
}
```

In `cmdAdd`, replace the block `max := in.MaxPapers` / `if max <= 0 { max = 30 }` with:

```go
	max := in.MaxPapers
	if max <= 0 {
		max = c.defaultMaxPapers
	}
	if max <= 0 {
		max = 30
	}
```

- [ ] **Step 4: Update the knowledge tests**

In `internal/tools/knowledge/openalex_test.go`, both `NewOpenAlex(...)` calls gain a `""` mailto argument: `NewOpenAlex(res)` → `NewOpenAlex(res, "")`, and `NewOpenAlex(NewResults())` → `NewOpenAlex(NewResults(), "")`.

In `internal/tools/knowledge/biorxiv_test.go`, both `NewBioRxiv(...)` calls gain a `0` argument: `NewBioRxiv(res)` → `NewBioRxiv(res, 0)`, and `NewBioRxiv(NewResults())` → `NewBioRxiv(NewResults(), 0)`.

In `internal/tools/knowledge/corpus_test.go`, the `NewCorpus(...)` call gains a `0` final argument:
`NewCorpus(st, res, stubMapper{answer: "fixed answer"}, filepath.Join(dir, "corpus.bleve"))` →
`NewCorpus(st, res, stubMapper{answer: "fixed answer"}, filepath.Join(dir, "corpus.bleve"), 0)`.

- [ ] **Step 5: Wire `cmd/proteus/main.go`**

In `runTUI`, the block currently reads:

```go
	cat, err := config.LoadModels()
	if err != nil {
		return err
	}
	models := llm.NewModelRegistry(cat)
	registry := buildRegistry(workspace, st, mgr, models)
```

Replace it with:

```go
	cat, err := config.LoadModels()
	if err != nil {
		return err
	}
	models := llm.NewModelRegistry(cat)
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	if err := models.SelectDefault(cfg.Defaults); err != nil {
		return err
	}
	registry := buildRegistry(workspace, st, mgr, models, cfg)
```

Change the `buildRegistry` signature:

```go
func buildRegistry(workspace string, st *store.Store, mgr *jobmgr.Manager, models *llm.ModelRegistry, cfg config.Config) *tools.Registry {
```

In `buildRegistry`, the compute-backend block currently reads:

```go
	// Compute backend (env-selectable; defaults to local).
	backend, err := backends.Select(os.Getenv("PROTEUS_COMPUTE_BACKEND"), proteusHome())
	if err != nil {
		// An unknown backend name falls back to local rather than crashing the TUI.
		backend, _ = backends.Select("local", proteusHome())
	}
```

Replace it with:

```go
	// Compute backend: PROTEUS_COMPUTE_BACKEND overrides config.toml's
	// [defaults].compute_backend (env wins; SP2 design §4).
	be := os.Getenv("PROTEUS_COMPUTE_BACKEND")
	if be == "" {
		be = cfg.Defaults.ComputeBackend
	}
	backend, err := backends.Select(be, proteusHome())
	if err != nil {
		// An unknown backend name falls back to local rather than crashing the TUI.
		backend, _ = backends.Select("local", proteusHome())
	}
```

In `buildRegistry`, the knowledge registrations currently read:

```go
	registry.Register(knowledge.NewOpenAlex(results))
```
```go
	registry.Register(knowledge.NewBioRxiv(results))
```
```go
	registry.Register(knowledge.NewCorpus(st, results, corpusMapper{models: models}, filepath.Join(workspace, "corpus.bleve")))
```

Replace those three lines with, respectively:

```go
	registry.Register(knowledge.NewOpenAlex(results, cfg.Knowledge.Mailto))
```
```go
	registry.Register(knowledge.NewBioRxiv(results, cfg.Knowledge.BiorxivRecentDays))
```
```go
	registry.Register(knowledge.NewCorpus(st, results, corpusMapper{models: models}, filepath.Join(workspace, "corpus.bleve"), cfg.Knowledge.CorpusDefaultMaxPapers))
```

- [ ] **Step 6: Update `cmd/proteus/main_test.go`**

`main_test.go` has four calls of the form
`buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil), llm.NewModelRegistry(config.DefaultCatalog()))`.
Add a `config.DefaultConfig()` final argument to each:
`buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil), llm.NewModelRegistry(config.DefaultCatalog()), config.DefaultConfig())`.
(`config` is already imported in `main_test.go`.)

- [ ] **Step 7: Run the full build and test suite**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS — all packages, vet clean.

- [ ] **Step 8: Commit**

```bash
git add internal/tools/knowledge/openalex.go internal/tools/knowledge/biorxiv.go internal/tools/knowledge/corpus.go internal/tools/knowledge/openalex_test.go internal/tools/knowledge/biorxiv_test.go internal/tools/knowledge/corpus_test.go cmd/proteus/main.go cmd/proteus/main_test.go
git commit -m "$(printf 'feat: wire config.toml [defaults] and [knowledge] into the subsystems\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Final verification

- [ ] `go build ./... && go test ./... && go vet ./...` — all green.
- [ ] Launch `./bin/proteus` once with no `~/.config/proteus/config.toml`; confirm `config.toml` is created.
- [ ] Set `[defaults].model` to a model ID from `models.toml`, relaunch, confirm `/model` shows it active.
- [ ] Set `[defaults].compute_backend = "modal"`; confirm `PROTEUS_COMPUTE_BACKEND=local ./bin/proteus` still uses local (env overrides file).
