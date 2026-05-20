# Config System — SP1 (models.toml) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Load Proteus's model/provider catalog from a `models.toml` file instead of a hardcoded Go slice.

**Architecture:** A new `internal/config/` package owns the config types, an embedded default `models.toml`, and loading from `~/.config/proteus/models.toml` (first-run materialized). `internal/llm.NewModelRegistry` is rewritten to take a `config.Catalog` and build providers table-driven by `kind`.

**Tech Stack:** Go, `github.com/BurntSushi/toml` (already a dependency), `//go:embed`.

**Spec:** `docs/superpowers/specs/2026-05-19-proteus-config-system-design.md` (SP1, §3–§9).

**Branch:** `feat/config-system` (already created).

---

## File map

| File | Change | Task |
|---|---|---|
| `internal/config/config.go` | **new** — types, `ConfigDir`, `parseCatalog`, `DefaultCatalog` (T1), `LoadModels` (T2) | 1, 2 |
| `internal/config/models.toml` | **new** — embedded default catalog | 1 |
| `internal/config/config_test.go` | **new** — catalog tests (T1), loader tests (T2) | 1, 2 |
| `internal/llm/modelregistry.go` | rewrite — `NewModelRegistry(config.Catalog)`, table-driven `Provider()` | 3 |
| `internal/llm/modelregistry_test.go` | modify — new constructor + a ready-default test | 3 |
| `cmd/proteus/main.go` | modify — `config.LoadModels()` → `NewModelRegistry(cat)` | 3 |
| `cmd/proteus/main_test.go`, `internal/tui/app_test.go`, `internal/tui/setup_test.go` | modify — pass `config.DefaultCatalog()` to `NewModelRegistry` | 3 |

Tasks are **sequential** (1 → 2 → 3): Tasks 1 and 2 share `internal/config/config.go`; Task 3 depends on both.

---

## Task 1: `internal/config` package — types, embedded default, `DefaultCatalog`

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/models.toml`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Create the embedded default `internal/config/models.toml`**

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
api_key_env = "VLLM_API_KEY"

[[model]]
id             = "claude-opus-4-7"
display_name   = "Claude Opus 4.7"
provider       = "anthropic"
context_tokens = 200000
supports_tools = true
input_price_per_1m  = 15
output_price_per_1m = 75

[[model]]
id             = "claude-sonnet-4-6"
display_name   = "Claude Sonnet 4.6"
provider       = "anthropic"
context_tokens = 200000
supports_tools = true
input_price_per_1m  = 3
output_price_per_1m = 15

[[model]]
id             = "gpt-4o"
display_name   = "GPT-4o"
provider       = "openai"
context_tokens = 128000
supports_tools = true
input_price_per_1m  = 2.5
output_price_per_1m = 10

[[model]]
id             = "gemini-2.0-flash"
display_name   = "Gemini 2.0 Flash"
provider       = "google"
context_tokens = 1000000
supports_tools = true
input_price_per_1m  = 0.1
output_price_per_1m = 0.4

[[model]]
id             = "llama3.3:70b"
display_name   = "Llama 3.3 70B (local)"
provider       = "ollama"
context_tokens = 128000
supports_tools = true
input_price_per_1m  = 0
output_price_per_1m = 0

[[model]]
id             = "Qwen/Qwen3.6-27B"
display_name   = "Qwen3.6 27B (local vLLM)"
provider       = "vllm"
context_tokens = 32768
supports_tools = true
input_price_per_1m  = 0
output_price_per_1m = 0
```

- [ ] **Step 2: Write the failing tests**

Create `internal/config/config_test.go`:

```go
package config

import (
	"testing"
)

func TestDefaultCatalogParses(t *testing.T) {
	c := DefaultCatalog()
	if len(c.Providers) == 0 || len(c.Models) == 0 {
		t.Fatalf("default catalog empty: %d providers, %d models",
			len(c.Providers), len(c.Models))
	}
}

func TestParseCatalogRejectsUnknownProvider(t *testing.T) {
	in := `
[[provider]]
name = "openai"
kind = "openai"

[[model]]
id = "x"
provider = "nosuch"
`
	if _, err := parseCatalog(in); err == nil {
		t.Fatal("expected an error: model references an unknown provider")
	}
}

func TestParseCatalogRejectsEmpty(t *testing.T) {
	in := `
[[provider]]
name = "openai"
kind = "openai"
`
	if _, err := parseCatalog(in); err == nil {
		t.Fatal("expected an error: catalog has no models")
	}
}

func TestConfigDirHonoursEnv(t *testing.T) {
	t.Setenv("PROTEUS_CONFIG_DIR", "/tmp/proteus-cfg-xyz")
	if got := ConfigDir(); got != "/tmp/proteus-cfg-xyz" {
		t.Errorf("ConfigDir = %q, want the env override", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL — `undefined: DefaultCatalog` / `parseCatalog` / `ConfigDir`

- [ ] **Step 4: Create `internal/config/config.go`**

```go
// Package config loads Proteus's TOML configuration (SPECS §14).
package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

//go:embed models.toml
var defaultModelsTOML string

// Provider is an LLM endpoint. Kind is anthropic | google | openai (openai =
// any OpenAI-compatible Chat Completions endpoint). API keys are never stored
// here; APIKeyEnv names the environment variable to read the key from.
type Provider struct {
	Name      string `toml:"name"`
	Kind      string `toml:"kind"`
	BaseURL   string `toml:"base_url"`
	APIKeyEnv string `toml:"api_key_env"`
}

// Model is one selectable model.
type Model struct {
	ID               string  `toml:"id"`
	DisplayName      string  `toml:"display_name"`
	Provider         string  `toml:"provider"`
	ContextTokens    int     `toml:"context_tokens"`
	SupportsTools    bool    `toml:"supports_tools"`
	InputPricePer1M  float64 `toml:"input_price_per_1m"`
	OutputPricePer1M float64 `toml:"output_price_per_1m"`
}

// Catalog is the parsed models.toml — the providers and models Proteus knows.
type Catalog struct {
	Providers []Provider `toml:"provider"`
	Models    []Model    `toml:"model"`
}

// ConfigDir returns Proteus's config directory: $PROTEUS_CONFIG_DIR if set,
// otherwise ~/.config/proteus (SPECS §14.1).
func ConfigDir() string {
	if d := os.Getenv("PROTEUS_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "proteus")
	}
	return filepath.Join(home, ".config", "proteus")
}

// validate checks the catalog is internally consistent.
func (c Catalog) validate() error {
	if len(c.Models) == 0 {
		return fmt.Errorf("no models defined")
	}
	known := map[string]bool{}
	for _, p := range c.Providers {
		known[p.Name] = true
	}
	for _, m := range c.Models {
		if !known[m.Provider] {
			return fmt.Errorf("model %q references unknown provider %q", m.ID, m.Provider)
		}
	}
	return nil
}

// parseCatalog decodes models.toml text and validates it.
func parseCatalog(text string) (Catalog, error) {
	var c Catalog
	if _, err := toml.Decode(text, &c); err != nil {
		return Catalog{}, fmt.Errorf("parse models.toml: %w", err)
	}
	if err := c.validate(); err != nil {
		return Catalog{}, fmt.Errorf("models.toml: %w", err)
	}
	return c, nil
}

// DefaultCatalog returns the catalog embedded in the binary (no disk access).
func DefaultCatalog() Catalog {
	c, err := parseCatalog(defaultModelsTOML)
	if err != nil {
		panic("embedded models.toml is invalid: " + err.Error())
	}
	return c
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/config/ && go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/models.toml internal/config/config_test.go
git commit -m "$(printf 'feat: add the config package and embedded models.toml\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 2: `config.LoadModels` — disk loading and first-run materialization

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go` (add `"os"` and `"path/filepath"` to its import block):

```go
func TestLoadModelsMaterializesDefaultOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROTEUS_CONFIG_DIR", dir)

	c, err := LoadModels()
	if err != nil {
		t.Fatalf("LoadModels: %v", err)
	}
	if len(c.Models) == 0 {
		t.Fatal("materialized catalog is empty")
	}
	if _, err := os.Stat(filepath.Join(dir, "models.toml")); err != nil {
		t.Errorf("models.toml was not written on first run: %v", err)
	}
}

func TestLoadModelsReadsUserFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROTEUS_CONFIG_DIR", dir)
	custom := `
[[provider]]
name = "vllm"
kind = "openai"
base_url = "http://localhost:9999/v1"

[[model]]
id = "my-model"
display_name = "My Model"
provider = "vllm"
context_tokens = 8192
supports_tools = true
`
	if err := os.WriteFile(filepath.Join(dir, "models.toml"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadModels()
	if err != nil {
		t.Fatalf("LoadModels: %v", err)
	}
	if len(c.Models) != 1 || c.Models[0].ID != "my-model" {
		t.Fatalf("user file not used: %+v", c.Models)
	}
}

func TestLoadModelsRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PROTEUS_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "models.toml"),
		[]byte("not valid toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadModels(); err == nil {
		t.Fatal("expected an error for a malformed models.toml")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/ -run TestLoadModels`
Expected: FAIL — `undefined: LoadModels`

- [ ] **Step 3: Add `LoadModels` to `internal/config/config.go`**

Append to `internal/config/config.go`:

```go
// LoadModels loads the model catalog from <ConfigDir>/models.toml. If the file
// does not exist, the embedded default is written there (first-run
// materialization) and returned. A malformed or invalid file is an error.
func LoadModels() (Catalog, error) {
	path := filepath.Join(ConfigDir(), "models.toml")
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
			return Catalog{}, err
		}
		if err := os.WriteFile(path, []byte(defaultModelsTOML), 0o644); err != nil {
			return Catalog{}, err
		}
		return DefaultCatalog(), nil
	}
	if err != nil {
		return Catalog{}, fmt.Errorf("read %s: %w", path, err)
	}
	c, err := parseCatalog(string(body))
	if err != nil {
		return Catalog{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "$(printf 'feat: load models.toml from disk, materializing the default\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 3: Data-driven model registry

Rewrite `internal/llm` to build the registry from a `config.Catalog`. The `NewModelRegistry` signature changes, so every call site must update in this one task to keep the build green.

**Files:**
- Modify: `internal/llm/modelregistry.go`
- Modify: `internal/llm/modelregistry_test.go`
- Modify: `cmd/proteus/main.go`
- Modify: `cmd/proteus/main_test.go`
- Modify: `internal/tui/app_test.go`
- Modify: `internal/tui/setup_test.go`

- [ ] **Step 1: Rewrite `internal/llm/modelregistry.go`**

Replace the entire file with:

```go
package llm

import (
	"fmt"
	"os"
	"sync"

	"github.com/alvarogonjim/proteus/internal/config"
)

// ModelEntry is one selectable model plus the name of the provider serving it.
type ModelEntry struct {
	ModelDescriptor
	ProviderName string
}

// ModelRegistry tracks available models and the active selection. All methods
// are safe for concurrent use; mu guards activeID and the clients map.
type ModelRegistry struct {
	models    []ModelEntry
	providers map[string]config.Provider // provider name -> definition
	mu        sync.Mutex
	activeID  string
	clients   map[string]Provider // provider name -> built client (lazy)
}

// NewModelRegistry builds the registry from a config catalog. The active model
// is the first whose provider is ready (its API-key env var is set, or it
// needs no key); failing that, the first model in the catalog.
func NewModelRegistry(cat config.Catalog) *ModelRegistry {
	mr := &ModelRegistry{
		providers: map[string]config.Provider{},
		clients:   map[string]Provider{},
	}
	for _, p := range cat.Providers {
		mr.providers[p.Name] = p
	}
	for _, m := range cat.Models {
		mr.models = append(mr.models, ModelEntry{
			ModelDescriptor: ModelDescriptor{
				ID:               m.ID,
				DisplayName:      m.DisplayName,
				ContextTokens:    m.ContextTokens,
				SupportsTools:    m.SupportsTools,
				InputPricePer1M:  m.InputPricePer1M,
				OutputPricePer1M: m.OutputPricePer1M,
			},
			ProviderName: m.Provider,
		})
	}
	mr.activeID = mr.defaultModelID()
	return mr
}

// providerReady reports whether a provider can be used: it needs no API key,
// or its key env var is set.
func (mr *ModelRegistry) providerReady(name string) bool {
	p, ok := mr.providers[name]
	if !ok {
		return false
	}
	return p.APIKeyEnv == "" || os.Getenv(p.APIKeyEnv) != ""
}

// defaultModelID picks the first model whose provider is ready, else the first.
func (mr *ModelRegistry) defaultModelID() string {
	if len(mr.models) == 0 {
		return ""
	}
	for _, m := range mr.models {
		if mr.providerReady(m.ProviderName) {
			return m.ID
		}
	}
	return mr.models[0].ID
}

// Models returns the registered models.
func (mr *ModelRegistry) Models() []ModelEntry { return mr.models }

// ActiveModel returns the active model ID.
func (mr *ModelRegistry) ActiveModel() string {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	return mr.activeID
}

// ActiveProviderName returns the provider name for the active model.
func (mr *ModelRegistry) ActiveProviderName() string {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	return mr.providerNameLocked()
}

// providerNameLocked resolves the active model's provider name. The caller
// must hold mr.mu.
func (mr *ModelRegistry) providerNameLocked() string {
	for _, m := range mr.models {
		if m.ID == mr.activeID {
			return m.ProviderName
		}
	}
	return ""
}

// SetModel switches the active model by ID.
func (mr *ModelRegistry) SetModel(id string) error {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	for _, m := range mr.models {
		if m.ID == id {
			mr.activeID = id
			return nil
		}
	}
	return fmt.Errorf("unknown model %q", id)
}

// Provider returns the constructed client for the active model's provider,
// building it on first use, dispatched by the provider's Kind.
func (mr *ModelRegistry) Provider() (Provider, error) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	name := mr.providerNameLocked()
	if c, ok := mr.clients[name]; ok {
		return c, nil
	}
	def, ok := mr.providers[name]
	if !ok {
		return nil, fmt.Errorf("no provider definition for %q", name)
	}
	var p Provider
	switch def.Kind {
	case "anthropic":
		p = NewAnthropicProvider(os.Getenv(def.APIKeyEnv))
	case "google":
		p = NewGoogleProvider(os.Getenv(def.APIKeyEnv))
	case "openai":
		key := os.Getenv(def.APIKeyEnv) // "" when APIKeyEnv is unset
		if key == "" {
			key = "none" // OpenAI-compatible local servers ignore auth; the SDK wants a non-empty key
		}
		p = NewOpenAIProvider(def.Name, def.BaseURL, key)
	default:
		return nil, fmt.Errorf("provider %q has unknown kind %q", name, def.Kind)
	}
	mr.clients[name] = p
	return p, nil
}
```

- [ ] **Step 2: Update `internal/llm/modelregistry_test.go`**

Add `"github.com/alvarogonjim/proteus/internal/config"` to the import block. Replace every bare `NewModelRegistry()` call with `NewModelRegistry(config.DefaultCatalog())` (6 occurrences). Then append a test for the ready-provider default selection:

```go
func TestModelRegistryDefaultPicksReadyProvider(t *testing.T) {
	// Model 0's provider needs a key (unset here); model 1's provider needs none.
	cat := config.Catalog{
		Providers: []config.Provider{
			{Name: "cloud", Kind: "openai", BaseURL: "https://x/v1", APIKeyEnv: "TEST_KEY_DEFINITELY_UNSET"},
			{Name: "local", Kind: "openai", BaseURL: "http://localhost:1/v1"},
		},
		Models: []config.Model{
			{ID: "cloud-m", Provider: "cloud", SupportsTools: true},
			{ID: "local-m", Provider: "local", SupportsTools: true},
		},
	}
	mr := NewModelRegistry(cat)
	if mr.ActiveModel() != "local-m" {
		t.Errorf("default model = %q, want local-m (its provider needs no key)", mr.ActiveModel())
	}
}
```

- [ ] **Step 3: Run the `llm` tests to verify they pass**

Run: `go test ./internal/llm/`
Expected: PASS

- [ ] **Step 4: Wire `cmd/proteus/main.go`**

Add `"github.com/alvarogonjim/proteus/internal/config"` to the import block. In `runTUI`, the line `models := llm.NewModelRegistry()` currently reads:

```go
	models := llm.NewModelRegistry()
```

Replace it with:

```go
	cat, err := config.LoadModels()
	if err != nil {
		return err
	}
	models := llm.NewModelRegistry(cat)
```

(`runTUI` already returns `error` and already declares `err` earlier via `:=`; this `err` is a new short-var declaration in a new statement — if the compiler reports "no new variables" or "err redeclared", change `cat, err :=` to `cat, loadErr := ...` and test `loadErr`.)

- [ ] **Step 5: Update the remaining test call sites**

In each of `cmd/proteus/main_test.go`, `internal/tui/app_test.go`, and `internal/tui/setup_test.go`: add `"github.com/alvarogonjim/proteus/internal/config"` to the import block, and replace every `llm.NewModelRegistry()` with `llm.NewModelRegistry(config.DefaultCatalog())` (4 occurrences in `main_test.go`, 5 in `app_test.go`, 1 in `setup_test.go`).

- [ ] **Step 6: Run the full build and test suite**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS — all packages, vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/llm/modelregistry.go internal/llm/modelregistry_test.go cmd/proteus/main.go cmd/proteus/main_test.go internal/tui/app_test.go internal/tui/setup_test.go
git commit -m "$(printf 'feat: build the model registry from the config catalog\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Final verification

- [ ] `go build ./... && go test ./... && go vet ./...` — all green.
- [ ] Run `./bin/proteus` once with no `~/.config/proteus/models.toml`; confirm the file is created and the TUI starts with the same six models in `/model`.
- [ ] Add a `[[model]]` block to `~/.config/proteus/models.toml`, relaunch, confirm it appears in `/model` — no rebuild.
- [ ] Confirm no API keys appear anywhere in `models.toml`.
