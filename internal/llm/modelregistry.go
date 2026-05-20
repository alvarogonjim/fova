package llm

import (
	"fmt"
	"os"
)

// ModelEntry is one selectable model plus the provider that serves it.
type ModelEntry struct {
	ModelDescriptor
	ProviderName string // anthropic | openai | ollama
}

// builtinModels is the hardcoded v0.1 model list (SPECS §6.3 / §6.4).
var builtinModels = []ModelEntry{
	{ModelDescriptor{"claude-opus-4-7", "Claude Opus 4.7", 200000, true, 15, 75}, "anthropic"},
	{ModelDescriptor{"claude-sonnet-4-6", "Claude Sonnet 4.6", 200000, true, 3, 15}, "anthropic"},
	{ModelDescriptor{"gpt-4o", "GPT-4o", 128000, true, 2.5, 10}, "openai"},
	{ModelDescriptor{"llama3.3:70b", "Llama 3.3 70B (local)", 128000, true, 0, 0}, "ollama"},
}

// ModelRegistry tracks available models and the active selection.
type ModelRegistry struct {
	models    []ModelEntry
	activeID  string
	providers map[string]Provider
}

// NewModelRegistry builds the registry and picks a sensible default model:
// the first Anthropic model if ANTHROPIC_API_KEY is set, else the first
// Ollama model, else the first model in the list.
func NewModelRegistry() *ModelRegistry {
	mr := &ModelRegistry{models: builtinModels, providers: map[string]Provider{}}
	mr.activeID = mr.models[0].ID
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		mr.activeID = firstWithProvider(mr.models, "anthropic")
	} else {
		mr.activeID = firstWithProvider(mr.models, "ollama")
	}
	return mr
}

func firstWithProvider(ms []ModelEntry, provider string) string {
	for _, m := range ms {
		if m.ProviderName == provider {
			return m.ID
		}
	}
	return ms[0].ID
}

// Models returns the registered models.
func (mr *ModelRegistry) Models() []ModelEntry { return mr.models }

// ActiveModel returns the active model ID.
func (mr *ModelRegistry) ActiveModel() string { return mr.activeID }

// ActiveProviderName returns the provider name for the active model.
func (mr *ModelRegistry) ActiveProviderName() string {
	for _, m := range mr.models {
		if m.ID == mr.activeID {
			return m.ProviderName
		}
	}
	return ""
}

// SetModel switches the active model by ID.
func (mr *ModelRegistry) SetModel(id string) error {
	for _, m := range mr.models {
		if m.ID == id {
			mr.activeID = id
			return nil
		}
	}
	return fmt.Errorf("unknown model %q", id)
}

// Provider returns the constructed provider for the active model, building it
// on first use.
func (mr *ModelRegistry) Provider() (Provider, error) {
	name := mr.ActiveProviderName()
	if p, ok := mr.providers[name]; ok {
		return p, nil
	}
	var p Provider
	switch name {
	case "anthropic":
		p = NewAnthropicProvider(os.Getenv("ANTHROPIC_API_KEY"))
	case "openai":
		p = NewOpenAIProvider("openai", "https://api.openai.com/v1", os.Getenv("OPENAI_API_KEY"))
	case "ollama":
		base := os.Getenv("OLLAMA_BASE_URL")
		if base == "" {
			base = "http://localhost:11434/v1"
		}
		p = NewOpenAIProvider("ollama", base, "ollama")
	default:
		return nil, fmt.Errorf("no provider for %q", name)
	}
	mr.providers[name] = p
	return p, nil
}
