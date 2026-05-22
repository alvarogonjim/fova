package llm

import (
	"fmt"
	"os"
	"sync"

	"github.com/alvarogonjim/fova/internal/config"
	"github.com/alvarogonjim/fova/internal/secrets"
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

// providerReady reports whether a provider can be used: it needs no API key,
// or its key env var is set.
func (mr *ModelRegistry) providerReady(name string) bool {
	p, ok := mr.providers[name]
	if !ok {
		return false
	}
	return p.APIKeyEnv == "" || resolveKey(p) != ""
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

// Reload swaps in a fresh catalog without rebuilding the registry. The
// active model ID survives the swap if the new catalog still defines it;
// otherwise the registry re-picks a default via the same ready-provider
// rule used at construction. Cached provider clients are dropped so
// changes to API keys, endpoints, or provider Kinds take effect on the
// next Provider() call. Safe for concurrent use.
func (mr *ModelRegistry) Reload(cat config.Catalog) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	providers := map[string]config.Provider{}
	for _, p := range cat.Providers {
		providers[p.Name] = p
	}
	models := make([]ModelEntry, 0, len(cat.Models))
	for _, m := range cat.Models {
		models = append(models, ModelEntry{
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
	prevActive := mr.activeID
	mr.providers = providers
	mr.models = models
	mr.clients = map[string]Provider{}
	for _, m := range models {
		if m.ID == prevActive {
			mr.activeID = prevActive
			return
		}
	}
	mr.activeID = mr.defaultModelID()
}

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

// CostUSD prices a token-usage record with the active model's per-1M prices.
// Local models carry zero prices, so their cost is 0; an unset active model
// also yields 0.
func (mr *ModelRegistry) CostUSD(u Usage) float64 {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	for _, m := range mr.models {
		if m.ID == mr.activeID {
			return float64(u.InputTokens)/1e6*m.InputPricePer1M +
				float64(u.OutputTokens)/1e6*m.OutputPricePer1M
		}
	}
	return 0
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
		p = NewAnthropicProvider(resolveKey(def))
	case "google":
		p = NewGoogleProvider(resolveKey(def))
	case "openai":
		key := resolveKey(def)
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
