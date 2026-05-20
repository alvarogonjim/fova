package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigParses(t *testing.T) {
	c := DefaultConfig()
	if c.UI.Theme == "" || c.Defaults.ComputeBackend == "" {
		t.Fatalf("default config has empty fields: %+v", c)
	}
}

func TestParseConfigRejectsBadTheme(t *testing.T) {
	in := `
[ui]
theme = "neon"
inline_graphics = "auto"
[defaults]
compute_backend = "local"
`
	if _, err := parseConfig(in); err == nil {
		t.Fatal("expected an error for an invalid ui.theme")
	}
}

func TestParseConfigRejectsBadBackend(t *testing.T) {
	in := `
[ui]
theme = "auto"
inline_graphics = "auto"
[defaults]
compute_backend = "cloud"
`
	if _, err := parseConfig(in); err == nil {
		t.Fatal("expected an error for an invalid compute_backend")
	}
}

func TestLoadConfigMaterializesAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if _, err := LoadConfig(); err != nil { // first run materializes
		t.Fatalf("first LoadConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil {
		t.Errorf("config.toml not written on first run: %v", err)
	}
	c, err := LoadConfig() // second run reads the materialized file
	if err != nil {
		t.Fatalf("second LoadConfig: %v", err)
	}
	if c.Defaults.ComputeBackend == "" {
		t.Fatal("round-tripped config is empty")
	}
}

func TestLoadConfigRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("bad ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected an error for a malformed config.toml")
	}
}

func TestParseConfigRejectsBadPort(t *testing.T) {
	in := `
[ui]
theme = "auto"
inline_graphics = "auto"
[defaults]
compute_backend = "local"
[webhook]
enabled = true
port = 0
[budget]
session_soft_limit_usd = 5.0
wetlab_requires_confirmation = true
`
	if _, err := parseConfig(in); err == nil {
		t.Fatal("expected an error for an out-of-range webhook.port")
	}
}

func TestParseConfigRejectsNegativeBudget(t *testing.T) {
	in := `
[ui]
theme = "auto"
inline_graphics = "auto"
[defaults]
compute_backend = "local"
[webhook]
enabled = true
port = 9876
[budget]
session_soft_limit_usd = -1.0
wetlab_requires_confirmation = true
`
	if _, err := parseConfig(in); err == nil {
		t.Fatal("expected an error for a negative session_soft_limit_usd")
	}
}

func TestParseConfigRejectsDisabledWetlabConfirmation(t *testing.T) {
	in := `
[ui]
theme = "auto"
inline_graphics = "auto"
[defaults]
compute_backend = "local"
[webhook]
enabled = true
port = 9876
[budget]
session_soft_limit_usd = 5.0
wetlab_requires_confirmation = false
`
	if _, err := parseConfig(in); err == nil {
		t.Fatal("expected an error for wetlab_requires_confirmation = false")
	}
}

func TestWebhookEffectiveURL(t *testing.T) {
	def := WebhookConfig{Enabled: true, Port: 9876}
	if got := def.EffectiveURL(); got != "http://localhost:9876/webhooks/adaptyv" {
		t.Errorf("EffectiveURL() = %q", got)
	}
	pub := WebhookConfig{Enabled: true, Port: 9876, PublicURL: "https://x.ngrok.io/"}
	if got := pub.EffectiveURL(); got != "https://x.ngrok.io/webhooks/adaptyv" {
		t.Errorf("EffectiveURL() with public_url = %q", got)
	}
}

func TestDefaultConfigHasWebhookAndBudget(t *testing.T) {
	c := DefaultConfig()
	if c.Webhook.Port == 0 || c.Budget.SessionSoftLimitUSD == 0 {
		t.Fatalf("default config missing webhook/budget values: %+v", c)
	}
	if !c.Budget.WetlabRequiresConfirmation {
		t.Fatal("default wetlab_requires_confirmation must be true")
	}
}

func TestSaveConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)

	// First load materialises the embedded default.
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	// Mutate one field and persist.
	c.UI.Theme = "dark"
	if err := SaveConfig(c); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Reload from disk and confirm the change stuck.
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("re-LoadConfig: %v", err)
	}
	if got.UI.Theme != "dark" {
		t.Errorf("UI.Theme not persisted: got %q, want %q", got.UI.Theme, "dark")
	}
}

func TestSaveConfigPreservesOtherFields(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)

	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	// Snapshot every non-Theme field, mutate Theme only, save, reload,
	// and verify every snapshotted field still matches.
	snap := c
	c.UI.Theme = "light"
	if err := SaveConfig(c); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("re-LoadConfig: %v", err)
	}
	if got.UI.InlineGraphics != snap.UI.InlineGraphics {
		t.Errorf("UI.InlineGraphics lost: %q vs %q", got.UI.InlineGraphics, snap.UI.InlineGraphics)
	}
	if got.Defaults != snap.Defaults {
		t.Errorf("Defaults lost: %+v vs %+v", got.Defaults, snap.Defaults)
	}
	if got.Knowledge != snap.Knowledge {
		t.Errorf("Knowledge lost: %+v vs %+v", got.Knowledge, snap.Knowledge)
	}
	if got.Webhook != snap.Webhook {
		t.Errorf("Webhook lost: %+v vs %+v", got.Webhook, snap.Webhook)
	}
	if got.Budget != snap.Budget {
		t.Errorf("Budget lost: %+v vs %+v", got.Budget, snap.Budget)
	}
}

func TestSaveConfigRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)

	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	c.UI.Theme = "neon" // not in {auto, light, dark}
	if err := SaveConfig(c); err == nil {
		t.Fatal("SaveConfig accepted an invalid theme")
	}

	// The file on disk must still be the materialised default — SaveConfig
	// must never persist an invalid config.
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("re-LoadConfig: %v", err)
	}
	if got.UI.Theme == "neon" {
		t.Fatal("invalid SaveConfig left bad theme on disk")
	}
}

func TestSaveConfigCreatesConfigDir(t *testing.T) {
	// A nested path that does not yet exist — SaveConfig must MkdirAll.
	dir := filepath.Join(t.TempDir(), "nested", "fova")
	t.Setenv("FOVA_CONFIG_DIR", dir)

	c := DefaultConfig()
	if err := SaveConfig(c); err != nil {
		t.Fatalf("SaveConfig into a fresh dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil {
		t.Errorf("config.toml not written: %v", err)
	}
}

func TestKnowledgeConfigHasNewSPDFields(t *testing.T) {
	c := DefaultConfig()
	if c.Knowledge.LocalPDFsDir != "" {
		t.Errorf("Knowledge.LocalPDFsDir default = %q, want \"\"", c.Knowledge.LocalPDFsDir)
	}
	if c.Knowledge.PaperclipBaseURL != "https://api.paperclip.dev" {
		t.Errorf("Knowledge.PaperclipBaseURL default = %q, want https://api.paperclip.dev",
			c.Knowledge.PaperclipBaseURL)
	}
}

func TestKnowledgeConfigParsesOverrides(t *testing.T) {
	in := `
[ui]
theme = "auto"
inline_graphics = "auto"
[defaults]
compute_backend = "local"
[knowledge]
local_pdfs_dir = "/no/such/dir/that/exists"
paperclip_base_url = "https://example.test/mcp"
[webhook]
enabled = true
port = 9876
[budget]
session_soft_limit_usd = 5.0
wetlab_requires_confirmation = true
`
	c, err := parseConfig(in)
	if err != nil {
		t.Fatalf("parseConfig: %v (a non-existent local_pdfs_dir must NOT fail validation)", err)
	}
	if c.Knowledge.LocalPDFsDir != "/no/such/dir/that/exists" {
		t.Errorf("LocalPDFsDir = %q", c.Knowledge.LocalPDFsDir)
	}
	if c.Knowledge.PaperclipBaseURL != "https://example.test/mcp" {
		t.Errorf("PaperclipBaseURL = %q", c.Knowledge.PaperclipBaseURL)
	}
}
