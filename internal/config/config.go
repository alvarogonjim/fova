// Package config loads fova's TOML configuration (SPECS §14).
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

// Catalog is the parsed models.toml — the providers and models fova knows.
type Catalog struct {
	Providers []Provider `toml:"provider"`
	Models    []Model    `toml:"model"`
}

// ConfigDir returns fova's config directory: $FOVA_CONFIG_DIR if set,
// otherwise ~/.config/fova (SPECS §14.1).
func ConfigDir() string {
	if d := os.Getenv("FOVA_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "fova")
	}
	return filepath.Join(home, ".config", "fova")
}

// validKinds are the provider kinds the model registry can construct.
var validKinds = map[string]bool{"anthropic": true, "google": true, "openai": true}

// validate checks the catalog is internally consistent.
func (c Catalog) validate() error {
	if len(c.Models) == 0 {
		return fmt.Errorf("no models defined")
	}
	known := map[string]bool{}
	for _, p := range c.Providers {
		if !validKinds[p.Kind] {
			return fmt.Errorf("provider %q has unknown kind %q (want anthropic|google|openai)", p.Name, p.Kind)
		}
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

// LoadModels loads the model catalog from <ConfigDir>/models.toml. If the file
// does not exist, the embedded default is written there (first-run
// materialization) and returned. A malformed or invalid file is an error.
func LoadModels() (Catalog, error) {
	dir := ConfigDir()
	path := filepath.Join(dir, "models.toml")
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Catalog{}, fmt.Errorf("create config dir %s: %w", dir, err)
		}
		if err := os.WriteFile(path, []byte(defaultModelsTOML), 0o644); err != nil {
			return Catalog{}, fmt.Errorf("write default %s: %w", path, err)
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
