# First-Run Onboarding Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A full-screen in-TUI wizard that runs once on first launch to set up the fova data folder, LLM provider (+ API key in the OS keychain), theme, compute backend, knowledge email, and budget — re-runnable via `/onboarding`.

**Architecture:** A self-contained `wizardModel` Bubble Tea component drives a step flow; it is run standalone before the main TUI on first launch and embedded as an `overlayWizard` for re-runs. Supporting changes make the wizard's choices take effect: a shared `internal/secrets` keychain helper, env→keychain LLM key resolution, a persisted `data_dir` config field, and config-aware data-folder resolution.

**Tech Stack:** Go 1.22, `charmbracelet/bubbletea` v1.3.10, `charmbracelet/bubbles` v1.0.0 (`textinput`), `charmbracelet/lipgloss`, `99designs/keyring` v1.2.2.

**Spec:** `docs/superpowers/specs/2026-05-22-onboarding-wizard-design.md`

**Branch:** `feat/onboarding-wizard` (already created off `main`; the spec is committed there).

---

## File Structure

**New**
- `internal/secrets/secrets.go` + `secrets_test.go` — OS-keychain Get/Set for LLM keys, with an in-memory test seam.
- `internal/tui/ollama.go` + `ollama_test.go` — the local-Ollama probe.
- `internal/tui/wizard.go` + `wizard_test.go` — the `wizardModel` component, steps, `WizardResult`, `RunOnboarding`, `ApplyWizardResult`.
- `cmd/fova/onboarding.go` + `onboarding_test.go` — first-run detection and harness.

**Modified**
- `internal/llm/modelregistry.go` — API-key resolution env → keychain.
- `internal/config/config_toml.go` + `internal/config/config.toml` — the `data_dir` field.
- `cmd/fova/main.go` — config-aware `fovaHome()`, `runTUI` reorder + first-run hook.
- `internal/tui/app.go` — `overlayWizard`, embedded `*wizardModel`, `/onboarding`.
- `internal/tui/commands.go` — register `/onboarding`.

## Task dependency notes

Tasks 1, 3, 5 are independent (disjoint files) and base-independent — safe to parallelize. Task 2 needs Task 1. Task 4 needs Task 3. Tasks 6–9 need Tasks 1–5 and run sequentially (6 → 7 → 8 → 9).

---

## Task 1: `internal/secrets` keychain helper

**Files:**
- Create: `internal/secrets/secrets.go`
- Test: `internal/secrets/secrets_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/secrets/secrets_test.go`:

```go
package secrets

import "testing"

func TestSetGetRoundTrip(t *testing.T) {
	defer UseInMemoryKeyring()()
	if err := Set("anthropic-api-key", "sk-xyz"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := Get("anthropic-api-key")
	if !ok || got != "sk-xyz" {
		t.Errorf("Get = %q, %v; want sk-xyz, true", got, ok)
	}
}

func TestGetMissing(t *testing.T) {
	defer UseInMemoryKeyring()()
	if _, ok := Get("absent"); ok {
		t.Error("Get of a missing key should return ok=false")
	}
}

func TestAPIKeyName(t *testing.T) {
	if got := APIKeyName("anthropic"); got != "anthropic-api-key" {
		t.Errorf("APIKeyName = %q, want anthropic-api-key", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/secrets/ -v`
Expected: FAIL — package does not compile (`secrets.go` missing).

- [ ] **Step 3: Implement `secrets.go`**

Create `internal/secrets/secrets.go`:

```go
// Package secrets reads and writes fova's secrets (LLM API keys) in the OS
// keychain, under the same "fova" service the Adaptyv token uses.
package secrets

import "github.com/99designs/keyring"

const service = "fova"

// open returns the keyring fova stores secrets in. It is a package var so
// tests can swap in an in-memory keyring via UseInMemoryKeyring.
var open = func() (keyring.Keyring, error) {
	return keyring.Open(keyring.Config{ServiceName: service})
}

// APIKeyName is the keychain entry name for a provider's API key. The wizard
// (writer) and the llm package (reader) both derive the name this way.
func APIKeyName(provider string) string { return provider + "-api-key" }

// Get returns the secret stored under name, and whether it was found.
func Get(name string) (string, bool) {
	ring, err := open()
	if err != nil {
		return "", false
	}
	item, err := ring.Get(name)
	if err != nil {
		return "", false
	}
	return string(item.Data), true
}

// Set stores value under name in the OS keychain.
func Set(name, value string) error {
	ring, err := open()
	if err != nil {
		return err
	}
	return ring.Set(keyring.Item{Key: name, Data: []byte(value)})
}

// UseInMemoryKeyring swaps the keyring for an in-memory one and returns a
// function that restores the previous opener. For tests in this and other
// packages.
func UseInMemoryKeyring() func() {
	prev := open
	ring := keyring.NewArrayKeyring(nil)
	open = func() (keyring.Keyring, error) { return ring, nil }
	return func() { open = prev }
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/secrets/ -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/
git commit -m "$(cat <<'EOF'
feat(secrets): OS-keychain helper for LLM API keys

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: LLM API-key resolution — env → keychain

**Files:**
- Modify: `internal/llm/modelregistry.go`
- Test: `internal/llm/modelregistry_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/llm/modelregistry_test.go` add (ensure `"github.com/alvarogonjim/fova/internal/secrets"` and `"github.com/alvarogonjim/fova/internal/config"` are imported):

```go
func TestResolveKeyEnvWins(t *testing.T) {
	defer secrets.UseInMemoryKeyring()()
	_ = secrets.Set("anthropic-api-key", "from-keychain")
	t.Setenv("TEST_ANTHROPIC_KEY", "from-env")
	p := config.Provider{Name: "anthropic", APIKeyEnv: "TEST_ANTHROPIC_KEY"}
	if got := resolveKey(p); got != "from-env" {
		t.Errorf("resolveKey = %q, want from-env", got)
	}
}

func TestResolveKeyFallsBackToKeychain(t *testing.T) {
	defer secrets.UseInMemoryKeyring()()
	_ = secrets.Set("anthropic-api-key", "from-keychain")
	p := config.Provider{Name: "anthropic", APIKeyEnv: "TEST_ANTHROPIC_KEY_UNSET"}
	if got := resolveKey(p); got != "from-keychain" {
		t.Errorf("resolveKey = %q, want from-keychain", got)
	}
}

func TestResolveKeyNoneAvailable(t *testing.T) {
	defer secrets.UseInMemoryKeyring()()
	p := config.Provider{Name: "openai", APIKeyEnv: "TEST_OPENAI_KEY_UNSET"}
	if got := resolveKey(p); got != "" {
		t.Errorf("resolveKey = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/llm/ -run ResolveKey -v`
Expected: FAIL — `resolveKey` undefined.

- [ ] **Step 3: Add `resolveKey` and use it**

In `internal/llm/modelregistry.go`, add `"github.com/alvarogonjim/fova/internal/secrets"` to the import block. Add this function (after `NewModelRegistry`):

```go
// resolveKey returns a provider's API key: the APIKeyEnv environment variable
// when set, otherwise the value stored in the OS keychain (SPECS §14.3).
func resolveKey(p config.Provider) string {
	if p.APIKeyEnv != "" {
		if v := os.Getenv(p.APIKeyEnv); v != "" {
			return v
		}
	}
	if v, ok := secrets.Get(secrets.APIKeyName(p.Name)); ok {
		return v
	}
	return ""
}
```

Replace the body of `providerReady`:

```go
func (mr *ModelRegistry) providerReady(name string) bool {
	p, ok := mr.providers[name]
	if !ok {
		return false
	}
	return p.APIKeyEnv == "" || resolveKey(p) != ""
}
```

In `Provider()`, replace the three key reads:

```go
	switch def.Kind {
	case "anthropic":
		p = NewAnthropicProvider(resolveKey(def))
	case "google":
		p = NewGoogleProvider(resolveKey(def))
	case "openai":
		key := resolveKey(def)
		if key == "" {
			key = "none" // OpenAI-compatible local servers ignore auth; the SDK wants a non-empty key
		}
		p = NewOpenAIProvider(def.Name, def.BaseURL, key)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/llm/ -v`
Expected: PASS (the new tests and the pre-existing llm tests).

- [ ] **Step 5: Commit**

```bash
git add internal/llm/modelregistry.go internal/llm/modelregistry_test.go
git commit -m "$(cat <<'EOF'
feat(llm): resolve API keys env -> OS keychain

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `data_dir` config field

**Files:**
- Modify: `internal/config/config_toml.go`
- Modify: `internal/config/config.toml`
- Test: `internal/config/config_toml_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/config/config_toml_test.go` add:

```go
func TestConfigDataDirRoundTrip(t *testing.T) {
	t.Setenv("FOVA_CONFIG_DIR", t.TempDir())
	c := DefaultConfig()
	if c.Defaults.DataDir != "" {
		t.Errorf("the embedded default data_dir should be empty, got %q", c.Defaults.DataDir)
	}
	c.Defaults.DataDir = "/home/u/proteins"
	if err := SaveConfig(c); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Defaults.DataDir != "/home/u/proteins" {
		t.Errorf("DataDir = %q, want /home/u/proteins", got.Defaults.DataDir)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ -run ConfigDataDir -v`
Expected: FAIL — `c.Defaults.DataDir` undefined.

- [ ] **Step 3: Add the field**

In `internal/config/config_toml.go`, in the `DefaultsConfig` struct, add the `DataDir` field after `ComputeBackend`:

```go
// DefaultsConfig is the [defaults] section of config.toml.
type DefaultsConfig struct {
	Provider       string `toml:"provider"`
	Model          string `toml:"model"`
	ComputeBackend string `toml:"compute_backend"`
	// DataDir is the fova data folder ($FOVA_HOME equivalent). Empty means the
	// ~/fova default. An explicit $FOVA_HOME environment variable overrides it.
	DataDir string `toml:"data_dir"`
}
```

In `internal/config/config.toml`, in the `[defaults]` section, add the line after `compute_backend`:

```toml
data_dir        = ""             # fova data folder; empty = ~/fova
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS (the new test and the pre-existing config tests).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config_toml.go internal/config/config.toml internal/config/config_toml_test.go
git commit -m "$(cat <<'EOF'
feat(config): add [defaults].data_dir for the fova data folder

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Config-aware `fovaHome()`

**Files:**
- Modify: `cmd/fova/main.go`
- Test: `cmd/fova/main_test.go`

- [ ] **Step 1: Write the failing tests**

In `cmd/fova/main_test.go` add (ensure `"github.com/alvarogonjim/fova/internal/config"` is imported):

```go
func TestResolveFovaHomeEnvWins(t *testing.T) {
	t.Setenv("FOVA_HOME", "/tmp/env-home")
	cfg := config.DefaultConfig()
	cfg.Defaults.DataDir = "/tmp/config-home"
	if got := resolveFovaHome(cfg); got != "/tmp/env-home" {
		t.Errorf("resolveFovaHome = %q, want /tmp/env-home", got)
	}
}

func TestResolveFovaHomeUsesConfig(t *testing.T) {
	t.Setenv("FOVA_HOME", "")
	cfg := config.DefaultConfig()
	cfg.Defaults.DataDir = "/tmp/config-home"
	if got := resolveFovaHome(cfg); got != "/tmp/config-home" {
		t.Errorf("resolveFovaHome = %q, want /tmp/config-home", got)
	}
}

func TestResolveFovaHomeDefault(t *testing.T) {
	t.Setenv("FOVA_HOME", "")
	cfg := config.DefaultConfig() // DataDir is ""
	got := resolveFovaHome(cfg)
	if got == "" || got == "/tmp/config-home" {
		t.Errorf("resolveFovaHome with no override = %q, want the ~/fova default", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/fova/ -run ResolveFovaHome -v`
Expected: FAIL — `resolveFovaHome` undefined.

- [ ] **Step 3: Make `fovaHome()` config-aware**

In `cmd/fova/main.go`, ensure `"strings"` is imported. Replace the existing `fovaHome` function with:

```go
// resolvedHome holds the fova data directory for this process. runTUI sets it
// once from config; fovaHome() returns it.
var resolvedHome string

// fovaHome returns the fova data directory for this process.
func fovaHome() string {
	if resolvedHome != "" {
		return resolvedHome
	}
	return defaultFovaHome()
}

// defaultFovaHome is the data dir absent any config: $FOVA_HOME or ~/fova.
func defaultFovaHome() string {
	if h := os.Getenv("FOVA_HOME"); h != "" {
		return h
	}
	uh, err := os.UserHomeDir()
	if err != nil {
		return "fova"
	}
	return filepath.Join(uh, "fova")
}

// resolveFovaHome resolves the data dir: an explicit $FOVA_HOME wins, then
// config.toml's [defaults].data_dir, then the ~/fova default.
func resolveFovaHome(cfg config.Config) string {
	if h := os.Getenv("FOVA_HOME"); h != "" {
		return h
	}
	if d := strings.TrimSpace(cfg.Defaults.DataDir); d != "" {
		return expandTilde(d)
	}
	return defaultFovaHome()
}

// expandTilde expands a leading ~ to the user's home directory.
func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if uh, err := os.UserHomeDir(); err == nil {
			return filepath.Join(uh, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}
```

- [ ] **Step 4: Set `resolvedHome` early in `runTUI`**

In `cmd/fova/main.go`, in `runTUI`, move the `config.LoadConfig()` call to the very top of the function and set `resolvedHome` from it, before `defaultWorkspace()` is called. The start of `runTUI` becomes:

```go
func runTUI() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	resolvedHome = resolveFovaHome(cfg)

	workspace, err := defaultWorkspace()
	if err != nil {
		return err
	}
	// ... the rest of runTUI unchanged, EXCEPT: delete the later
	// `cfg, err := config.LoadConfig()` line (config is already loaded above).
```

Find and delete the now-duplicate `cfg, err := config.LoadConfig()` block later in `runTUI` (the one near `tui.ApplyTheme(cfg.UI.Theme)`); `cfg` from the top of the function is reused.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./cmd/fova/ -v`
Expected: PASS — `ResolveFovaHome` tests and the pre-existing `cmd/fova` tests (`TestRunTUIWires...`, `TestVersionCommandPrints`).

- [ ] **Step 6: Commit**

```bash
git add cmd/fova/main.go cmd/fova/main_test.go
git commit -m "$(cat <<'EOF'
feat(cmd): resolve the fova data folder from config

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Local-Ollama probe

**Files:**
- Create: `internal/tui/ollama.go`
- Test: `internal/tui/ollama_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/ollama_test.go`:

```go
package tui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeOllamaDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if !probeOllama(srv.URL) {
		t.Error("probeOllama should detect a responding server")
	}
}

func TestProbeOllamaNotDetected(t *testing.T) {
	if probeOllama("http://127.0.0.1:1") {
		t.Error("probeOllama should return false for an unreachable server")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run ProbeOllama -v`
Expected: FAIL — `probeOllama` undefined.

- [ ] **Step 3: Implement `ollama.go`**

Create `internal/tui/ollama.go`:

```go
package tui

import (
	"net/http"
	"time"
)

// probeOllama reports whether an Ollama server answers at baseURL. It does a
// short-timeout GET of /api/tags; an OK response counts as detected. Any
// error or non-OK status is simply "not detected" — the probe never fails the
// wizard.
func probeOllama(baseURL string) bool {
	client := http.Client{Timeout: 300 * time.Millisecond}
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run ProbeOllama -v`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/ollama.go internal/tui/ollama_test.go
git commit -m "$(cat <<'EOF'
feat(tui): local-Ollama detection probe

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: The wizard component

**Files:**
- Create: `internal/tui/wizard.go`
- Test: `internal/tui/wizard_test.go`

The `wizardModel` is one cohesive Bubble Tea component. It has three step kinds
(`stepInfo` / `stepPick` / `stepInput`); the steps are data built by
`buildWizardSteps`. Keys: `Enter` next/finish, `Shift+Tab` back, `Esc` skip the
wizard, `↑/↓` move a pick selection, `Ctrl+S` defer the API-key step. The
component emits `wizardDoneMsg` when finished or skipped.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/wizard_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/config"
)

// testCatalog is a minimal provider catalog for wizard tests.
func testCatalog() config.Catalog {
	return config.Catalog{Providers: []config.Provider{
		{Name: "ollama", Kind: "openai", BaseURL: "http://localhost:11434/v1"},
		{Name: "anthropic", Kind: "anthropic", APIKeyEnv: "TEST_ANTH_KEY_UNSET"},
	}}
}

// drainDone feeds a tea.Cmd until it yields a wizardDoneMsg, or returns false.
func drainDone(cmd tea.Cmd) (wizardDoneMsg, bool) {
	if cmd == nil {
		return wizardDoneMsg{}, false
	}
	msg := cmd()
	if d, ok := msg.(wizardDoneMsg); ok {
		return d, true
	}
	return wizardDoneMsg{}, false
}

func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func runes(s string) tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestWizardSkipEmitsSkippedDone(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), false)
	_, cmd := w.Update(key(tea.KeyEsc))
	d, ok := drainDone(cmd)
	if !ok || !d.Skipped {
		t.Fatalf("Esc should emit a skipped wizardDoneMsg, got %+v ok=%v", d, ok)
	}
}

func TestWizardWelcomeIsUnnumbered(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), false)
	if w.steps[w.idx].numbered {
		t.Error("the welcome step should not be numbered")
	}
}

func TestWizardNavigatesForwardAndBack(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), false)
	start := w.idx
	w.Update(key(tea.KeyEnter)) // leave welcome
	if w.idx == start {
		t.Fatal("Enter on welcome should advance")
	}
	w.Update(key(tea.KeyShiftTab)) // back
	if w.idx != start {
		t.Errorf("Shift+Tab should return to the welcome step")
	}
}

func TestWizardProviderPickSkipsKeyStepForLocal(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), true)
	w.gotoStep("provider")
	// ollama is the first choice; select it and advance.
	w.Update(key(tea.KeyEnter))
	if got := w.steps[w.idx].id; got == "apikey" {
		t.Error("a local provider must skip the API-key step")
	}
}

func TestWizardProviderPickShowsKeyStepForPaid(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), false)
	w.gotoStep("provider")
	w.Update(key(tea.KeyDown))  // move to anthropic
	w.Update(key(tea.KeyEnter)) // commit + advance
	if got := w.steps[w.idx].id; got != "apikey" {
		t.Errorf("a paid provider with no env var must show the API-key step, landed on %q", got)
	}
}

func TestWizardBudgetRejectsNonNumber(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), false)
	w.gotoStep("budget")
	w.input.SetValue("not-a-number")
	w.Update(key(tea.KeyEnter))
	if w.errMsg == "" {
		t.Error("a non-numeric budget should produce an inline error and not advance")
	}
	if w.steps[w.idx].id != "budget" {
		t.Error("an invalid budget must not advance the wizard")
	}
}

func TestWizardFinishEmitsResult(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), true)
	w.gotoStep("summary")
	w.result.Provider = "ollama"
	_, cmd := w.Update(key(tea.KeyEnter))
	d, ok := drainDone(cmd)
	if !ok || d.Skipped {
		t.Fatalf("Enter on summary should emit a non-skipped wizardDoneMsg, got %+v ok=%v", d, ok)
	}
	if d.Result.Provider != "ollama" {
		t.Errorf("the result should carry the collected provider, got %q", d.Result.Provider)
	}
}

func TestWizardViewShowsStepTitle(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), false)
	w.width, w.height = 80, 24
	if !strings.Contains(w.View(), w.steps[w.idx].title) {
		t.Error("the view should render the current step's title")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run Wizard -v`
Expected: FAIL — `newWizardModel`, `wizardModel`, etc. undefined.

- [ ] **Step 3: Implement `wizard.go`**

Create `internal/tui/wizard.go`:

```go
package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alvarogonjim/fova/internal/config"
)

// WizardResult is the set of choices the onboarding wizard collected.
type WizardResult struct {
	DataDir        string
	Provider       string
	APIKeyProvider string
	APIKeyEnv      string
	APIKey         string
	Theme          string
	ComputeBackend string
	KnowledgeEmail string
	BudgetUSD      float64
}

// wizardDoneMsg is emitted when the wizard finishes (Skipped false) or is
// skipped (Skipped true).
type wizardDoneMsg struct {
	Result  WizardResult
	Skipped bool
}

type wizardStepKind int

const (
	stepInfo wizardStepKind = iota
	stepPick
	stepInput
)

// wizardChoice is one option of a stepPick step.
type wizardChoice struct{ id, label, tag string }

// wizardStep is one screen of the wizard.
type wizardStep struct {
	id       string
	kind     wizardStepKind
	numbered bool
	title    string
	body     string
	choices  []wizardChoice          // stepPick
	masked   bool                    // stepInput: mask the entry
	validate func(string) error      // stepInput: nil = always valid
	active   func(WizardResult) bool // nil = always shown
}

// wizardModel is the onboarding wizard component.
type wizardModel struct {
	theme         Theme
	width, height int
	catalog       config.Catalog

	steps  []wizardStep
	idx    int
	result WizardResult

	input   textinput.Model
	pickCur int
	errMsg  string

	finished bool
	skipped  bool
}

// newWizardModel builds the wizard. ollamaUp is the result of the local-Ollama
// probe (injected so the constructor stays offline and testable).
func newWizardModel(th Theme, cat config.Catalog, ollamaUp bool) *wizardModel {
	ti := textinput.New()
	ti.Prompt = "  "
	m := &wizardModel{
		theme:   th,
		catalog: cat,
		steps:   buildWizardSteps(cat, ollamaUp),
		input:   ti,
		result: WizardResult{
			DataDir: "~/fova", Theme: "auto", ComputeBackend: "local", BudgetUSD: 5,
		},
	}
	m.enterStep()
	return m
}

// buildWizardSteps assembles the ordered step list.
func buildWizardSteps(cat config.Catalog, ollamaUp bool) []wizardStep {
	apiKeyActive := func(r WizardResult) bool {
		for _, p := range cat.Providers {
			if p.Name == r.Provider {
				return p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) == ""
			}
		}
		return false
	}
	return []wizardStep{
		{
			id: "welcome", kind: stepInfo, numbered: false,
			title: "fova — a terminal agent for de novo protein design",
			body: "First time here. This quick setup picks your LLM provider, " +
				"where fova keeps its files, and a few defaults.\n\n" +
				"fova is free by default — a local model needs no account.",
		},
		{
			id: "folder", kind: stepInput, numbered: true,
			title: "Where should fova keep its data?",
			body:  "Projects, design files, job logs and local model caches live here.",
			validate: func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("enter a folder path")
				}
				return nil
			},
		},
		{
			id: "provider", kind: stepPick, numbered: true,
			title:   "Choose your LLM provider",
			body:    "fova works fully free with a local model. Paid providers are optional.",
			choices: providerChoices(cat, ollamaUp),
		},
		{
			id: "apikey", kind: stepInput, numbered: true, masked: true,
			title:  "API key",
			body:   "Paste a key — fova stores it in your OS keychain, never in a plain file. Ctrl+S to set it up later.",
			active: apiKeyActive,
		},
		{
			id: "theme", kind: stepPick, numbered: true,
			title: "Colour theme",
			body:  "fova adapts to your terminal, or you can force light or dark.",
			choices: []wizardChoice{
				{id: "auto", label: "Auto", tag: "match the terminal"},
				{id: "dark", label: "Dark", tag: ""},
				{id: "light", label: "Light", tag: ""},
			},
		},
		{
			id: "compute", kind: stepPick, numbered: true,
			title: "Compute backend",
			body:  "Where design jobs run. Local uses uv-managed GPU tools (/install, /doctor); Modal needs the Modal CLI and /modal deploy.",
			choices: []wizardChoice{
				{id: "local", label: "Local", tag: "uv-managed GPU tools"},
				{id: "modal", label: "Modal", tag: "bring-your-own cloud GPU"},
			},
		},
		{
			id: "email", kind: stepInput, numbered: true,
			title: "Knowledge email (optional)",
			body:  "Used for the OpenAlex polite pool — it improves literature-API rate limits. Leave blank to skip.",
			validate: func(s string) error {
				s = strings.TrimSpace(s)
				if s == "" {
					return nil
				}
				if strings.ContainsAny(s, " \t") || !strings.Contains(s, "@") || !strings.Contains(s, ".") {
					return fmt.Errorf("that does not look like an email")
				}
				return nil
			},
		},
		{
			id: "budget", kind: stepInput, numbered: true,
			title: "Session budget",
			body:  "The per-session paid-LLM soft limit, in USD, that triggers a warning.",
			validate: func(s string) error {
				v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
				if err != nil || v < 0 {
					return fmt.Errorf("enter a number greater than or equal to 0")
				}
				return nil
			},
		},
		{
			id: "summary", kind: stepInfo, numbered: true,
			title: "All set — review and confirm",
			body:  "Enter writes config.toml to ~/.config/fova/ and opens fova.",
		},
	}
}

// providerChoices maps the catalog's providers to pick choices.
func providerChoices(cat config.Catalog, ollamaUp bool) []wizardChoice {
	out := make([]wizardChoice, 0, len(cat.Providers))
	for _, p := range cat.Providers {
		tag := "free · local · no account"
		if p.APIKeyEnv != "" {
			tag = "paid · needs an API key"
		}
		if p.Name == "ollama" && ollamaUp {
			tag += " · detected"
		}
		out = append(out, wizardChoice{id: p.Name, label: capitalize(p.Name), tag: tag})
	}
	return out
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (m *wizardModel) Init() tea.Cmd { return textinput.Blink }

// gotoStep jumps directly to the step with the given id (used by tests and by
// the summary's "back" navigation). It re-runs enterStep.
func (m *wizardModel) gotoStep(id string) {
	for i, s := range m.steps {
		if s.id == id {
			m.idx = i
			m.enterStep()
			return
		}
	}
}

// enterStep initializes the per-step widget for the current step.
func (m *wizardModel) enterStep() {
	m.errMsg = ""
	step := m.steps[m.idx]
	switch step.kind {
	case stepInput:
		m.input.EchoMode = textinput.EchoNormal
		if step.masked {
			m.input.EchoMode = textinput.EchoPassword
		}
		m.input.SetValue(m.inputDefault(step.id))
		m.input.CursorEnd()
		m.input.Focus()
	case stepPick:
		m.pickCur = m.pickIndex(step)
	}
}

// inputDefault returns the pre-filled value for an input step.
func (m *wizardModel) inputDefault(id string) string {
	switch id {
	case "folder":
		return m.result.DataDir
	case "email":
		return m.result.KnowledgeEmail
	case "budget":
		return strconv.FormatFloat(m.result.BudgetUSD, 'f', -1, 64)
	default: // apikey
		return ""
	}
}

// pickIndex returns the choice index matching the already-collected value.
func (m *wizardModel) pickIndex(step wizardStep) int {
	var want string
	switch step.id {
	case "provider":
		want = m.result.Provider
	case "theme":
		want = m.result.Theme
	case "compute":
		want = m.result.ComputeBackend
	}
	for i, c := range step.choices {
		if c.id == want {
			return i
		}
	}
	return 0
}

// visible reports whether a step is shown given the collected result.
func (m *wizardModel) visible(s wizardStep) bool {
	return s.active == nil || s.active(m.result)
}

// advance moves to the next visible step, or finishes on the last one.
func (m *wizardModel) advance() tea.Cmd {
	for i := m.idx + 1; i < len(m.steps); i++ {
		if m.visible(m.steps[i]) {
			m.idx = i
			m.enterStep()
			return nil
		}
	}
	m.finished = true
	return m.done()
}

// back moves to the previous visible step.
func (m *wizardModel) back() {
	for i := m.idx - 1; i >= 0; i-- {
		if m.visible(m.steps[i]) {
			m.idx = i
			m.enterStep()
			return
		}
	}
}

// done emits the terminal wizardDoneMsg.
func (m *wizardModel) done() tea.Cmd {
	res, skipped := m.result, m.skipped
	return func() tea.Msg { return wizardDoneMsg{Result: res, Skipped: skipped} }
}

// commit validates and stores the current step's value into the result.
// It returns false (with m.errMsg set) when validation fails.
func (m *wizardModel) commit() bool {
	step := m.steps[m.idx]
	switch step.kind {
	case stepInput:
		val := m.input.Value()
		if step.validate != nil {
			if err := step.validate(val); err != nil {
				m.errMsg = err.Error()
				return false
			}
		}
		m.storeInput(step.id, val)
	case stepPick:
		if len(step.choices) > 0 {
			m.storePick(step.id, step.choices[m.pickCur].id)
		}
	}
	return true
}

func (m *wizardModel) storeInput(id, val string) {
	val = strings.TrimSpace(val)
	switch id {
	case "folder":
		m.result.DataDir = val
	case "apikey":
		m.result.APIKey = val
		m.result.APIKeyProvider = m.result.Provider
		m.result.APIKeyEnv = m.providerEnv(m.result.Provider)
	case "email":
		m.result.KnowledgeEmail = val
	case "budget":
		m.result.BudgetUSD, _ = strconv.ParseFloat(val, 64)
	}
}

func (m *wizardModel) storePick(id, choice string) {
	switch id {
	case "provider":
		m.result.Provider = choice
	case "theme":
		m.result.Theme = choice
		ApplyTheme(choice) // apply live so the change is visible immediately
	case "compute":
		m.result.ComputeBackend = choice
	}
}

// providerEnv returns a provider's API-key environment variable name.
func (m *wizardModel) providerEnv(name string) string {
	for _, p := range m.catalog.Providers {
		if p.Name == name {
			return p.APIKeyEnv
		}
	}
	return ""
}

func (m *wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	if m.steps[m.idx].kind == stepInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *wizardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	step := m.steps[m.idx]
	switch msg.Type {
	case tea.KeyEsc:
		m.skipped = true
		return m, m.done()
	case tea.KeyCtrlC:
		m.skipped = true
		return m, m.done()
	case tea.KeyShiftTab:
		m.back()
		return m, nil
	case tea.KeyCtrlS:
		if step.id == "apikey" { // defer: leave the key empty
			return m, m.advance()
		}
	case tea.KeyEnter:
		if m.commit() {
			return m, m.advance()
		}
		return m, nil
	case tea.KeyUp:
		if step.kind == stepPick && m.pickCur > 0 {
			m.pickCur--
		}
		return m, nil
	case tea.KeyDown:
		if step.kind == stepPick && m.pickCur < len(step.choices)-1 {
			m.pickCur++
		}
		return m, nil
	}
	if step.kind == stepInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// counter renders "step N of M" over the visible, numbered steps.
func (m *wizardModel) counter() string {
	total, pos := 0, 0
	for i, s := range m.steps {
		if !s.numbered || !m.visible(s) {
			continue
		}
		total++
		if i <= m.idx {
			pos++
		}
	}
	return fmt.Sprintf("step %d of %d", pos, total)
}

func (m *wizardModel) View() string {
	step := m.steps[m.idx]
	var b strings.Builder

	if step.numbered {
		b.WriteString(m.theme.Header.Render("fova setup"))
		b.WriteString("   " + m.theme.Muted.Render(m.counter()) + "\n\n")
	} else {
		b.WriteString(m.theme.Header.Render("fova — first-run setup") + "\n\n")
	}

	b.WriteString(m.theme.AgentText.Render(step.title) + "\n\n")
	b.WriteString(m.theme.Muted.Render(step.body) + "\n\n")

	switch step.kind {
	case stepPick:
		for i, c := range step.choices {
			row := "  " + c.label
			if c.tag != "" {
				row += "  " + m.theme.Subtle.Render(c.tag)
			}
			if i == m.pickCur {
				row = m.theme.PickerSel.Render("▸ " + c.label)
				if c.tag != "" {
					row += "  " + m.theme.Subtle.Render(c.tag)
				}
			}
			b.WriteString(row + "\n")
		}
	case stepInput:
		b.WriteString(m.input.View() + "\n")
	case stepInfo:
		if step.id == "summary" {
			b.WriteString(m.summaryView())
		}
	}

	if m.errMsg != "" {
		b.WriteString("\n" + m.theme.Error.Render("✗ "+m.errMsg) + "\n")
	}
	b.WriteString("\n" + m.theme.Subtle.Render(m.footer(step)))

	body := b.String()
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
	}
	return body
}

// summaryView renders the collected choices.
func (m *wizardModel) summaryView() string {
	r := m.result
	key := "not set"
	if r.APIKey != "" {
		key = "saved to keychain"
	}
	rows := []string{
		fmt.Sprintf("  data folder   %s", r.DataDir),
		fmt.Sprintf("  provider      %s", orDash(r.Provider)),
		fmt.Sprintf("  api key       %s", key),
		fmt.Sprintf("  theme         %s", r.Theme),
		fmt.Sprintf("  compute       %s", r.ComputeBackend),
		fmt.Sprintf("  email         %s", orDash(r.KnowledgeEmail)),
		fmt.Sprintf("  budget        $%.2f per session", r.BudgetUSD),
	}
	return m.theme.AgentText.Render(strings.Join(rows, "\n")) + "\n"
}

// footer renders the per-step key hints.
func (m *wizardModel) footer(step wizardStep) string {
	parts := []string{"esc skip setup"}
	if m.idx > 0 {
		parts = append([]string{"shift+tab back"}, parts...)
	}
	switch {
	case step.id == "summary":
		parts = append([]string{"enter finish"}, parts...)
	case step.id == "apikey":
		parts = append([]string{"enter store & next", "ctrl+s set up later"}, parts...)
	default:
		parts = append([]string{"enter next"}, parts...)
	}
	return strings.Join(parts, "  ·  ")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run Wizard -v`
Expected: PASS (all eight wizard tests).
Run: `go test ./internal/tui/`
Expected: PASS (no regressions).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/wizard.go internal/tui/wizard_test.go
git commit -m "$(cat <<'EOF'
feat(tui): the onboarding wizard component

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: `ApplyWizardResult` and `RunOnboarding`

**Files:**
- Modify: `internal/tui/wizard.go` (append the apply + standalone-run functions)
- Test: `internal/tui/wizard_test.go` (append)

- [ ] **Step 1: Write the failing tests**

In `internal/tui/wizard_test.go` add (ensure `"os"`, `"path/filepath"`, and
`"github.com/alvarogonjim/fova/internal/secrets"` are imported):

```go
func TestApplyWizardResultWritesConfig(t *testing.T) {
	t.Setenv("FOVA_CONFIG_DIR", t.TempDir())
	defer secrets.UseInMemoryKeyring()()
	err := ApplyWizardResult(WizardResult{
		Provider: "anthropic", Theme: "dark", ComputeBackend: "modal",
		KnowledgeEmail: "a@b.com", BudgetUSD: 9,
		APIKeyProvider: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY", APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("ApplyWizardResult: %v", err)
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Defaults.Provider != "anthropic" || cfg.UI.Theme != "dark" ||
		cfg.Defaults.ComputeBackend != "modal" || cfg.Knowledge.Mailto != "a@b.com" ||
		cfg.Budget.SessionSoftLimitUSD != 9 {
		t.Errorf("config not written as expected: %+v", cfg)
	}
	if got, ok := secrets.Get(secrets.APIKeyName("anthropic")); !ok || got != "sk-test" {
		t.Errorf("API key not stored: %q %v", got, ok)
	}
}

func TestApplyWizardResultCreatesDataDir(t *testing.T) {
	t.Setenv("FOVA_CONFIG_DIR", t.TempDir())
	defer secrets.UseInMemoryKeyring()()
	dir := filepath.Join(t.TempDir(), "newhome")
	if err := ApplyWizardResult(WizardResult{DataDir: dir, Theme: "auto", ComputeBackend: "local"}); err != nil {
		t.Fatalf("ApplyWizardResult: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("data dir not created: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run ApplyWizardResult -v`
Expected: FAIL — `ApplyWizardResult` undefined.

- [ ] **Step 3: Append the apply + run functions to `wizard.go`**

In `internal/tui/wizard.go`, add `"github.com/alvarogonjim/fova/internal/secrets"`
to the import block (`config` is already imported from Task 6). Append:

```go
// ApplyWizardResult writes the wizard's choices: config.toml fields, the API
// key into the OS keychain, and the data directory. A keychain failure is not
// fatal — the key is applied to the process environment so the current
// session still works.
func ApplyWizardResult(r WizardResult) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	if r.Theme != "" {
		cfg.UI.Theme = r.Theme
	}
	if r.Provider != "" {
		cfg.Defaults.Provider = r.Provider
	}
	if r.ComputeBackend != "" {
		cfg.Defaults.ComputeBackend = r.ComputeBackend
	}
	if r.DataDir != "" {
		cfg.Defaults.DataDir = r.DataDir
	}
	cfg.Knowledge.Mailto = r.KnowledgeEmail
	if r.BudgetUSD > 0 {
		cfg.Budget.SessionSoftLimitUSD = r.BudgetUSD
	}
	if err := config.SaveConfig(cfg); err != nil {
		return err
	}
	if r.APIKey != "" && r.APIKeyProvider != "" {
		if err := secrets.Set(secrets.APIKeyName(r.APIKeyProvider), r.APIKey); err != nil {
			// Keychain unavailable (headless box, no secret service): fall back
			// to the process environment so this session still works.
			if r.APIKeyEnv != "" {
				_ = os.Setenv(r.APIKeyEnv, r.APIKey)
			}
		}
	}
	if r.DataDir != "" {
		if err := os.MkdirAll(wizardExpandTilde(r.DataDir), 0o755); err != nil {
			return fmt.Errorf("create data dir: %w", err)
		}
	}
	return nil
}

// wizardExpandTilde expands a leading ~ to the user's home directory.
func wizardExpandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if uh, err := os.UserHomeDir(); err == nil {
			return uh + strings.TrimPrefix(p, "~")
		}
	}
	return p
}

// onboardingProgram wraps the wizard as a standalone Bubble Tea program: it
// turns the wizard's wizardDoneMsg into tea.Quit.
type onboardingProgram struct{ wizard *wizardModel }

func (o *onboardingProgram) Init() tea.Cmd { return o.wizard.Init() }

func (o *onboardingProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(wizardDoneMsg); ok {
		return o, tea.Quit
	}
	_, cmd := o.wizard.Update(msg)
	return o, cmd
}

func (o *onboardingProgram) View() string { return o.wizard.View() }

// RunOnboarding runs the first-run wizard as a standalone program. ok is true
// when the user finished it (false when skipped).
func RunOnboarding(cat config.Catalog) (result WizardResult, ok bool, err error) {
	w := newWizardModel(NewTheme(), cat, probeOllama("http://localhost:11434"))
	final, runErr := tea.NewProgram(&onboardingProgram{wizard: w}, tea.WithAltScreen()).Run()
	if runErr != nil {
		return WizardResult{}, false, runErr
	}
	op := final.(*onboardingProgram)
	return op.wizard.result, op.wizard.finished, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'Wizard|ApplyWizardResult' -v`
Expected: PASS.
Run: `go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/wizard.go internal/tui/wizard_test.go
git commit -m "$(cat <<'EOF'
feat(tui): apply wizard results and run the standalone wizard

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: First-run detection and harness

**Files:**
- Create: `cmd/fova/onboarding.go`
- Modify: `cmd/fova/main.go` (call the harness in `runTUI`)
- Test: `cmd/fova/onboarding_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/fova/onboarding_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsFirstRunTrueWhenConfigAbsent(t *testing.T) {
	t.Setenv("FOVA_CONFIG_DIR", t.TempDir())
	if !isFirstRun() {
		t.Error("isFirstRun should be true when config.toml is absent")
	}
}

func TestIsFirstRunFalseWhenConfigPresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[ui]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isFirstRun() {
		t.Error("isFirstRun should be false when config.toml exists")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/fova/ -run IsFirstRun -v`
Expected: FAIL — `isFirstRun` undefined.

- [ ] **Step 3: Implement `onboarding.go`**

Create `cmd/fova/onboarding.go`:

```go
package main

import (
	"os"
	"path/filepath"

	"golang.org/x/term"

	"github.com/alvarogonjim/fova/internal/config"
	"github.com/alvarogonjim/fova/internal/tui"
)

// isFirstRun reports whether fova has never been configured: config.toml does
// not yet exist in the config directory.
func isFirstRun() bool {
	_, err := os.Stat(filepath.Join(config.ConfigDir(), "config.toml"))
	return os.IsNotExist(err)
}

// interactive reports whether stdin and stdout are both a terminal — the
// wizard only runs in a real interactive session.
func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// maybeRunOnboarding runs the first-run wizard when fova has never been
// configured and the session is interactive. It applies the result before
// the main TUI starts. Skips silently on any non-first-run / non-interactive
// path so existing users and tests are unaffected.
func maybeRunOnboarding() error {
	if !isFirstRun() || !interactive() {
		return nil
	}
	cat, err := config.LoadModels()
	if err != nil {
		return err
	}
	result, ok, err := tui.RunOnboarding(cat)
	if err != nil {
		return err
	}
	if !ok {
		return nil // skipped — fall through to embedded defaults
	}
	return tui.ApplyWizardResult(result)
}
```

- [ ] **Step 4: Call the harness from `runTUI`**

In `cmd/fova/main.go`, in `runTUI`, add the onboarding call as the **first
statement**, before `config.LoadConfig()`:

```go
func runTUI() error {
	if err := maybeRunOnboarding(); err != nil {
		return err
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	resolvedHome = resolveFovaHome(cfg)
	// ... rest unchanged
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./cmd/fova/ -v`
Expected: PASS — `IsFirstRun` tests and the pre-existing `cmd/fova` tests. The
pre-existing tests run non-interactively, so `maybeRunOnboarding` is a no-op
for them.
Run: `go build ./...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/fova/onboarding.go cmd/fova/onboarding_test.go cmd/fova/main.go
git commit -m "$(cat <<'EOF'
feat(cmd): run the onboarding wizard on first launch

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: The `/onboarding` re-run overlay

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/commands.go`
- Test: `internal/tui/app_test.go`

- [ ] **Step 1: Write the failing tests**

In `internal/tui/app_test.go` add:

```go
func TestOnboardingCommandOpensWizard(t *testing.T) {
	t.Setenv("FOVA_CONFIG_DIR", t.TempDir()) // isolate config I/O
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.runSlashCommand("onboarding", "")
	if m.overlay != overlayWizard {
		t.Fatalf("/onboarding should open the wizard overlay, got %v", m.overlay)
	}
	if m.wizard == nil {
		t.Error("/onboarding should construct the wizard model")
	}
}

func TestWizardOverlaySkipCloses(t *testing.T) {
	t.Setenv("FOVA_CONFIG_DIR", t.TempDir()) // isolate config I/O
	m := newTestApp()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.runSlashCommand("onboarding", "")
	// Esc inside the overlay produces a command that yields a wizardDoneMsg;
	// run it and feed the message back so finishWizardOverlay runs.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc in the wizard overlay should produce a command")
	}
	m.Update(cmd())
	if m.overlay != overlayNone {
		t.Error("skipping the wizard overlay should close it")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'OnboardingCommand|WizardOverlay' -v`
Expected: FAIL — `overlayWizard`, `m.wizard` undefined.

- [ ] **Step 3: Add the overlay state**

In `internal/tui/app.go`, add `overlayWizard` to the overlay constant block:

```go
const (
	overlayNone overlay = iota
	overlayConfirm
	overlaySubmit
	overlayPicker
	overlayJobLog
	overlayKeys
	overlayWizard
)
```

Add a field to the `Model` struct (next to `keys`):

```go
	wizard *wizardModel // /onboarding wizard overlay; nil unless open
```

- [ ] **Step 4: Register the `/onboarding` command**

In `internal/tui/commands.go`, add to the `slashCommands` slice (after the
`keys` entry):

```go
	{Name: "onboarding", Description: "re-run the first-run setup wizard", Arguments: ArgsNone},
```

- [ ] **Step 5: Handle the command and the overlay**

In `internal/tui/app.go`, in `runSlashCommand`'s switch, add a case:

```go
	case "onboarding":
		return m.cmdOnboarding()
```

Add the command handler and the overlay-done handler (place them near the
other `cmd*` methods):

```go
// cmdOnboarding opens the onboarding wizard as an overlay.
func (m *Model) cmdOnboarding() (tea.Model, tea.Cmd) {
	cat := config.DefaultCatalog()
	if c, err := config.LoadModels(); err == nil {
		cat = c
	}
	m.wizard = newWizardModel(m.theme, cat, probeOllama("http://localhost:11434"))
	m.wizard.width, m.wizard.height = m.width, m.height
	m.overlay = overlayWizard
	return m, m.wizard.Init()
}

// finishWizardOverlay applies a completed wizard result, reloads config so the
// live-applicable settings take effect, and closes the overlay.
func (m *Model) finishWizardOverlay(msg wizardDoneMsg) (tea.Model, tea.Cmd) {
	m.overlay = overlayNone
	m.wizard = nil
	if msg.Skipped {
		return m, nil
	}
	if err := ApplyWizardResult(msg.Result); err != nil {
		m.chat.appendError("onboarding: " + err.Error())
		return m, nil
	}
	m.cmdReload() // re-read config.toml / models.toml; applies theme, provider, budget
	m.chat.appendAgentDeltaBlock("Setup saved.")
	if msg.Result.DataDir != "" {
		m.chat.appendAgentDeltaBlock(
			"The data folder change takes effect the next time you start fova.")
	}
	return m, nil
}
```

In `Update`, add a `wizardDoneMsg` case (before the final "forward to text
input" fallthrough):

```go
	case wizardDoneMsg:
		return m.finishWizardOverlay(msg)
```

In `handleKey`, add an `overlayWizard` branch to the overlay switch (alongside
`overlayKeys` etc.):

```go
	case overlayWizard:
		if m.wizard == nil {
			m.overlay = overlayNone
			return m, nil
		}
		_, cmd := m.wizard.Update(msg)
		return m, cmd
```

In `View`, add the `overlayWizard` case to the overlay switch:

```go
	case overlayWizard:
		if m.wizard != nil {
			return m.wizard.View()
		}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'OnboardingCommand|WizardOverlay' -v`
Expected: PASS.
Run: `go build ./... && go test ./...`
Expected: the full build and suite green.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/app.go internal/tui/commands.go internal/tui/app_test.go
git commit -m "$(cat <<'EOF'
feat(tui): /onboarding re-runs the setup wizard as an overlay

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Final verification

- [ ] **Run the full suite and build**

Run: `go build ./... && go test ./...`
Expected: all packages PASS.

- [ ] **Manual smoke test**

- `rm -rf ~/.config/fova` (or point `FOVA_CONFIG_DIR` at an empty dir), then
  `./bin/fova` after `make build` → the wizard appears; step through it; the
  chosen provider/theme/folder land in `~/.config/fova/config.toml`.
- A paid provider with no env var shows the API-key step; a local provider skips it.
- `Esc` skips; fova starts on defaults; the wizard does not reappear next launch.
- `/onboarding` re-opens the wizard inside a running session.

---

## Spec Coverage Check

- §4 wizard component (`wizardModel`, kinds, `WizardResult`) → Task 6.
- §5 the steps (welcome, folder, provider, conditional API key, theme, compute, email, budget, summary), navigation, skip → Task 6.
- §6 first-run detection + standalone harness → Tasks 7 (`RunOnboarding`) + 8.
- §7 `/onboarding` re-run overlay → Task 9.
- §8 applying the result → Task 7 (`ApplyWizardResult`).
- §9.1 env→keychain key resolution → Task 2; §9.2 shared keychain helper → Task 1; §9.3 persisted data-folder path → Tasks 3 + 4.
- §10 edge cases — non-TTY (Task 8 `interactive()`), keychain failure (Task 7 fallback), Ollama probe (Task 5), invalid input + skip (Task 6).
- §11 testing — tests embedded in every task.

> **Simplification noted vs spec §5.2:** the data-folder step is always editable
> rather than read-only when `$FOVA_HOME` is set. `resolveFovaHome` already gives
> `$FOVA_HOME` precedence, so an explicit environment override still wins at
> runtime; the wizard value is simply also recorded.
