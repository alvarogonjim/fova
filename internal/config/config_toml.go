package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed config.toml
var defaultConfigTOML string

// UIConfig is the [ui] section of config.toml. Its consumers (theming, inline
// graphics) are v0.5 deliverables; SP2 only parses and validates these values.
type UIConfig struct {
	Theme          string `toml:"theme"`
	InlineGraphics string `toml:"inline_graphics"`
}

// DefaultsConfig is the [defaults] section of config.toml.
type DefaultsConfig struct {
	Provider       string `toml:"provider"`
	Model          string `toml:"model"`
	ComputeBackend string `toml:"compute_backend"`
}

// KnowledgeConfig is the [knowledge] section of config.toml.
type KnowledgeConfig struct {
	Mailto                 string `toml:"mailto"`
	BiorxivRecentDays      int    `toml:"biorxiv_recent_days"`
	CorpusDefaultMaxPapers int    `toml:"corpus_default_max_papers"`
	// LocalPDFsDir is the default directory the knowledge.local_pdfs tool
	// scans when an "add" call omits its own dir. An empty value means
	// "must be supplied per call". The path is not required to exist at
	// startup — a missing dir only fails the affected call.
	LocalPDFsDir string `toml:"local_pdfs_dir"`
	// PaperclipBaseURL is the base URL the optional knowledge.paperclip tool
	// posts to. Defaults to the public Paperclip endpoint.
	PaperclipBaseURL string `toml:"paperclip_base_url"`
}

// WebhookConfig is the [webhook] section of config.toml. It configures the
// Adaptyv results webhook receiver and the callback URL sent to Adaptyv.
type WebhookConfig struct {
	Enabled   bool   `toml:"enabled"`
	Port      int    `toml:"port"`
	PublicURL string `toml:"public_url"`
}

// EffectiveURL is the URL Adaptyv should call back: public_url (with the
// webhook path appended) when set, else a localhost URL on the configured port.
func (w WebhookConfig) EffectiveURL() string {
	if w.PublicURL != "" {
		return strings.TrimRight(w.PublicURL, "/") + "/webhooks/adaptyv"
	}
	return fmt.Sprintf("http://localhost:%d/webhooks/adaptyv", w.Port)
}

// BudgetConfig is the [budget] section of config.toml.
type BudgetConfig struct {
	SessionSoftLimitUSD        float64 `toml:"session_soft_limit_usd"`
	WetlabRequiresConfirmation bool    `toml:"wetlab_requires_confirmation"`
}

// Config is the parsed config.toml (SPECS §14.2).
type Config struct {
	UI        UIConfig        `toml:"ui"`
	Defaults  DefaultsConfig  `toml:"defaults"`
	Knowledge KnowledgeConfig `toml:"knowledge"`
	Webhook   WebhookConfig   `toml:"webhook"`
	Budget    BudgetConfig    `toml:"budget"`
}

var (
	validThemes   = map[string]bool{"auto": true, "light": true, "dark": true}
	validGraphics = map[string]bool{"auto": true, "kitty": true, "sixel": true, "iterm2": true, "off": true}
	validBackends = map[string]bool{"local": true, "modal": true}
)

// validate checks config.toml's enum and integer fields.
func (c Config) validate() error {
	if !validThemes[c.UI.Theme] {
		return fmt.Errorf("ui.theme %q must be auto|light|dark", c.UI.Theme)
	}
	if !validGraphics[c.UI.InlineGraphics] {
		return fmt.Errorf("ui.inline_graphics %q must be auto|kitty|sixel|iterm2|off", c.UI.InlineGraphics)
	}
	if c.Defaults.ComputeBackend != "" && !validBackends[c.Defaults.ComputeBackend] {
		return fmt.Errorf("defaults.compute_backend %q must be local|modal", c.Defaults.ComputeBackend)
	}
	if c.Knowledge.BiorxivRecentDays < 0 {
		return fmt.Errorf("knowledge.biorxiv_recent_days must not be negative")
	}
	if c.Knowledge.CorpusDefaultMaxPapers < 0 {
		return fmt.Errorf("knowledge.corpus_default_max_papers must not be negative")
	}
	if c.Webhook.Port < 1 || c.Webhook.Port > 65535 {
		return fmt.Errorf("webhook.port %d must be between 1 and 65535", c.Webhook.Port)
	}
	if c.Budget.SessionSoftLimitUSD < 0 {
		return fmt.Errorf("budget.session_soft_limit_usd must not be negative")
	}
	if !c.Budget.WetlabRequiresConfirmation {
		return fmt.Errorf("budget.wetlab_requires_confirmation must be true (never disable)")
	}
	return nil
}

// parseConfig decodes config.toml text and validates it.
func parseConfig(text string) (Config, error) {
	var c Config
	if _, err := toml.Decode(text, &c); err != nil {
		return Config{}, fmt.Errorf("parse config.toml: %w", err)
	}
	if err := c.validate(); err != nil {
		return Config{}, fmt.Errorf("config.toml: %w", err)
	}
	return c, nil
}

// DefaultConfig returns the config embedded in the binary (no disk access).
func DefaultConfig() Config {
	c, err := parseConfig(defaultConfigTOML)
	if err != nil {
		panic("embedded config.toml is invalid: " + err.Error())
	}
	return c
}

// SaveConfig validates c and writes it to <ConfigDir>/config.toml, atomically
// via a temp file + rename. Every field of Config is encoded, so callers that
// want to mutate one value should load → mutate → save (the unchanged fields
// are preserved verbatim). SaveConfig never touches models.toml.
func SaveConfig(c Config) error {
	if err := c.validate(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "config.toml")
	tmp, err := os.CreateTemp(dir, "config.toml.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(c); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("encode config.toml: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}

// LoadConfig loads config.toml from <ConfigDir>. If the file does not exist,
// the embedded default is written there (first-run materialization) and
// returned. A malformed or invalid file is an error.
func LoadConfig() (Config, error) {
	dir := ConfigDir()
	path := filepath.Join(dir, "config.toml")
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Config{}, fmt.Errorf("create config dir %s: %w", dir, err)
		}
		if err := os.WriteFile(path, []byte(defaultConfigTOML), 0o644); err != nil {
			return Config{}, fmt.Errorf("write default %s: %w", path, err)
		}
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	c, err := parseConfig(string(body))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}
