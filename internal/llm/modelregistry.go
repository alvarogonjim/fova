package llm

import (
	"fmt"
	"os"
	"sync"
)

// ModelEntry is one selectable model plus the provider that serves it.
type ModelEntry struct {
	ModelDescriptor
	ProviderName string // anthropic | openai | google | ollama
}

// builtinModels is the hardcoded v0.1 model list (SPECS §6.3 / §6.4).
var builtinModels = []ModelEntry{
	{ModelDescriptor{"claude-opus-4-7", "Claude Opus 4.7", 200000, true, 15, 75}, "anthropic"},
	{ModelDescriptor{"claude-sonnet-4-6", "Claude Sonnet 4.6", 200000, true, 3, 15}, "anthropic"},
	{ModelDescriptor{"gpt-4o", "GPT-4o", 128000, true, 2.5, 10}, "openai"},
	{ModelDescriptor{"gemini-2.0-flash", "Gemini 2.0 Flash", 1000000, true, 0.1, 0.4}, "google"},
	{ModelDescriptor{"llama3.3:70b", "Llama 3.3 70B (local)", 128000, true, 0, 0}, "ollama"},
	{ModelDescriptor{"Qwen/Qwen3.6-27B", "Qwen3.6 27B (local vLLM)", 32768, true, 0, 0}, "vllm"},
}

// ModelRegistry tracks available models and the active selection. All methods
// are safe for concurrent use; mu guards activeID and the providers map.
type ModelRegistry struct {
	models    []ModelEntry
	mu        sync.Mutex
	activeID  string
	providers map[string]Provider
}

// NewModelRegistry builds the registry and picks a sensible default model:
// the first Anthropic model if ANTHROPIC_API_KEY is set, else the first vLLM
// model if VLLM_BASE_URL is set, else the first Ollama model.
func NewModelRegistry() *ModelRegistry {
	mr := &ModelRegistry{models: builtinModels, providers: map[string]Provider{}}
	mr.activeID = mr.models[0].ID
	switch {
	case os.Getenv("ANTHROPIC_API_KEY") != "":
		mr.activeID = firstWithProvider(mr.models, "anthropic")
	case os.Getenv("VLLM_BASE_URL") != "":
		mr.activeID = firstWithProvider(mr.models, "vllm")
	default:
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

// Provider returns the constructed provider for the active model, building it
// on first use.
func (mr *ModelRegistry) Provider() (Provider, error) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	name := mr.providerNameLocked()
	if p, ok := mr.providers[name]; ok {
		return p, nil
	}
	var p Provider
	switch name {
	case "anthropic":
		p = NewAnthropicProvider(os.Getenv("ANTHROPIC_API_KEY"))
	case "openai":
		p = NewOpenAIProvider("openai", "https://api.openai.com/v1", os.Getenv("OPENAI_API_KEY"))
	case "google":
		p = NewGoogleProvider(os.Getenv("GOOGLE_API_KEY"))
	case "ollama":
		base := os.Getenv("OLLAMA_BASE_URL")
		if base == "" {
			base = "http://localhost:11434/v1"
		}
		p = NewOpenAIProvider("ollama", base, "ollama")
	case "vllm":
		base := os.Getenv("VLLM_BASE_URL")
		if base == "" {
			base = "http://localhost:8000/v1"
		}
		apiKey := os.Getenv("VLLM_API_KEY")
		if apiKey == "" {
			apiKey = "vllm" // vLLM ignores the key unless started with --api-key
		}
		p = NewOpenAIProvider("vllm", base, apiKey)
	default:
		return nil, fmt.Errorf("no provider for %q", name)
	}
	mr.providers[name] = p
	return p, nil
}
