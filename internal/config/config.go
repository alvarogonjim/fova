// Package config is a backward-compatibility shim. Every symbol re-exports
// internal/assets; new code must import internal/assets directly. This
// package is deleted once all importers are migrated (configurable-assets
// plan, Task 16).
package config

import "github.com/alvarogonjim/fova/internal/assets"

type (
	Config          = assets.Config
	UIConfig        = assets.UIConfig
	DefaultsConfig  = assets.DefaultsConfig
	KnowledgeConfig = assets.KnowledgeConfig
	WebhookConfig   = assets.WebhookConfig
	BudgetConfig    = assets.BudgetConfig
	Catalog         = assets.Catalog
	Provider        = assets.Provider
	Model           = assets.Model
)

// ConfigDir is the pre-rename name of assets.Dir.
func ConfigDir() string { return assets.Dir() }

var (
	LoadConfig     = assets.LoadConfig
	SaveConfig     = assets.SaveConfig
	DefaultConfig  = assets.DefaultConfig
	LoadModels     = assets.LoadModels
	DefaultCatalog = assets.DefaultCatalog
)
