package llm

import (
	"sync"
	"testing"

	"github.com/alvarogonjim/fova/internal/config"
)

func TestModelRegistryListsModels(t *testing.T) {
	mr := NewModelRegistry(config.DefaultCatalog())
	if len(mr.Models()) == 0 {
		t.Fatal("model registry is empty")
	}
}

func TestModelRegistrySetModel(t *testing.T) {
	mr := NewModelRegistry(config.DefaultCatalog())
	models := mr.Models()
	target := models[len(models)-1]
	if err := mr.SetModel(target.ID); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	if mr.ActiveModel() != target.ID {
		t.Fatalf("ActiveModel = %q, want %q", mr.ActiveModel(), target.ID)
	}
	if err := mr.SetModel("no-such-model"); err == nil {
		t.Fatal("setting an unknown model should error")
	}
}

func TestModelRegistryReloadPreservesActive(t *testing.T) {
	cat := config.Catalog{
		Providers: []config.Provider{{Name: "local", Kind: "ollama"}},
		Models: []config.Model{
			{ID: "a", Provider: "local"},
			{ID: "b", Provider: "local"},
		},
	}
	mr := NewModelRegistry(cat)
	if err := mr.SetModel("b"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	mr.Reload(config.Catalog{
		Providers: []config.Provider{{Name: "local", Kind: "ollama"}},
		Models:    []config.Model{{ID: "a", Provider: "local"}, {ID: "b", Provider: "local"}, {ID: "c", Provider: "local"}},
	})
	if got := mr.ActiveModel(); got != "b" {
		t.Fatalf("Reload dropped active model; got %q, want %q", got, "b")
	}
	if len(mr.Models()) != 3 {
		t.Fatalf("Reload did not swap in the new catalog; len = %d, want 3", len(mr.Models()))
	}
}

func TestModelRegistryReloadFallsBackWhenActiveRemoved(t *testing.T) {
	cat := config.Catalog{
		Providers: []config.Provider{{Name: "local", Kind: "ollama"}},
		Models:    []config.Model{{ID: "a", Provider: "local"}, {ID: "gone", Provider: "local"}},
	}
	mr := NewModelRegistry(cat)
	if err := mr.SetModel("gone"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	mr.Reload(config.Catalog{
		Providers: []config.Provider{{Name: "local", Kind: "ollama"}},
		Models:    []config.Model{{ID: "a", Provider: "local"}},
	})
	if got := mr.ActiveModel(); got != "a" {
		t.Fatalf("Reload should fall back to default when active model removed; got %q, want %q", got, "a")
	}
}

func TestModelRegistryHasOllamaModel(t *testing.T) {
	mr := NewModelRegistry(config.DefaultCatalog())
	for _, m := range mr.Models() {
		if m.ProviderName == "ollama" {
			return
		}
	}
	t.Fatal("no Ollama model registered (acceptance criterion 3)")
}

// TestModelRegistryConcurrentAccess exercises Provider, SetModel and
// ActiveModel from many goroutines; it must pass under `go test -race`.
func TestModelRegistryConcurrentAccess(t *testing.T) {
	mr := NewModelRegistry(config.DefaultCatalog())
	models := mr.Models()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := mr.Provider(); err != nil {
				t.Errorf("Provider() failed: %v", err)
			}
			if i%4 == 0 {
				_ = mr.SetModel(models[i%len(models)].ID)
			}
			_ = mr.ActiveModel()
			_ = mr.ActiveProviderName()
		}(i)
	}
	wg.Wait()
}

func TestModelRegistryHasGoogleProvider(t *testing.T) {
	mr := NewModelRegistry(config.DefaultCatalog())
	var googleID string
	for _, m := range mr.Models() {
		if m.ProviderName == "google" {
			googleID = m.ID
			break
		}
	}
	if googleID == "" {
		t.Fatal("no Google model registered")
	}
	if err := mr.SetModel(googleID); err != nil {
		t.Fatalf("SetModel(%q) failed: %v", googleID, err)
	}
	p, err := mr.Provider()
	if err != nil {
		t.Fatalf("Provider() for Google model failed: %v", err)
	}
	if p.Name() != "google" {
		t.Fatalf("Provider().Name() = %q, want %q", p.Name(), "google")
	}
}

func TestModelRegistryHasVLLMProvider(t *testing.T) {
	mr := NewModelRegistry(config.DefaultCatalog())
	var vllmID string
	for _, m := range mr.Models() {
		if m.ProviderName == "vllm" {
			vllmID = m.ID
			break
		}
	}
	if vllmID == "" {
		t.Fatal("no vLLM model registered")
	}
	if err := mr.SetModel(vllmID); err != nil {
		t.Fatalf("SetModel(%q) failed: %v", vllmID, err)
	}
	p, err := mr.Provider()
	if err != nil {
		t.Fatalf("Provider() for vLLM model failed: %v", err)
	}
	if p.Name() != "vllm" {
		t.Fatalf("Provider().Name() = %q, want %q", p.Name(), "vllm")
	}
}

func TestModelRegistryBuildsAnthropicProvider(t *testing.T) {
	cat := config.Catalog{
		Providers: []config.Provider{
			{Name: "anthropic", Kind: "anthropic", APIKeyEnv: "ANTHROPIC_API_KEY"},
		},
		Models: []config.Model{
			{ID: "claude-x", Provider: "anthropic", SupportsTools: true},
		},
	}
	mr := NewModelRegistry(cat)
	p, err := mr.Provider()
	if err != nil {
		t.Fatalf("Provider() for an anthropic-kind provider failed: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Provider().Name() = %q, want anthropic", p.Name())
	}
}

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
