package llm

import (
	"sync"
	"testing"
)

func TestModelRegistryListsModels(t *testing.T) {
	mr := NewModelRegistry()
	if len(mr.Models()) == 0 {
		t.Fatal("model registry is empty")
	}
}

func TestModelRegistrySetModel(t *testing.T) {
	mr := NewModelRegistry()
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

func TestModelRegistryHasOllamaModel(t *testing.T) {
	mr := NewModelRegistry()
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
	mr := NewModelRegistry()
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
	mr := NewModelRegistry()
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
	mr := NewModelRegistry()
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
