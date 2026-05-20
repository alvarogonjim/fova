package llm

import "testing"

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
