package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/config"
	"github.com/alvarogonjim/fova/internal/secrets"
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

func TestWizardCtrlSDefersAPIKey(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), false)
	w.gotoStep("provider")
	w.Update(key(tea.KeyDown))  // move to anthropic (paid)
	w.Update(key(tea.KeyEnter)) // commit -> lands on apikey
	if w.steps[w.idx].id != "apikey" {
		t.Fatalf("setup: expected the apikey step, got %q", w.steps[w.idx].id)
	}
	w.Update(key(tea.KeyCtrlS)) // defer the key
	if w.steps[w.idx].id == "apikey" {
		t.Error("Ctrl+S should advance past the API-key step")
	}
	if w.result.APIKey != "" {
		t.Error("deferring the API-key step should leave the key empty")
	}
}

func TestWizardBackSkipsInactiveAPIKey(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), true)
	w.gotoStep("provider")
	w.Update(key(tea.KeyEnter)) // ollama (local) -> apikey is inactive -> theme
	if w.steps[w.idx].id != "theme" {
		t.Fatalf("setup: expected the theme step, got %q", w.steps[w.idx].id)
	}
	w.Update(key(tea.KeyShiftTab)) // back
	if w.steps[w.idx].id != "provider" {
		t.Errorf("back from theme should skip the inactive apikey step and land on provider, got %q", w.steps[w.idx].id)
	}
}

func TestWizardFolderAcceptsTypedValue(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), false)
	w.gotoStep("folder")
	w.input.SetValue("")
	w.Update(runes("/tmp/fova-data"))
	w.Update(key(tea.KeyEnter))
	if w.result.DataDir != "/tmp/fova-data" {
		t.Errorf("the folder step should store the typed path, got %q", w.result.DataDir)
	}
}

func TestWizardFolderRejectsEmpty(t *testing.T) {
	w := newWizardModel(NewTheme(), testCatalog(), false)
	w.gotoStep("folder")
	w.input.SetValue("")
	w.Update(key(tea.KeyEnter))
	if w.errMsg == "" || w.steps[w.idx].id != "folder" {
		t.Error("an empty folder path should be rejected with an inline error and not advance")
	}
}

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
