package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultCatalogParses(t *testing.T) {
	c := DefaultCatalog()
	if len(c.Providers) == 0 || len(c.Models) == 0 {
		t.Fatalf("default catalog empty: %d providers, %d models",
			len(c.Providers), len(c.Models))
	}
}

func TestParseCatalogRejectsUnknownProvider(t *testing.T) {
	in := `
[[provider]]
name = "openai"
kind = "openai"

[[model]]
id = "x"
provider = "nosuch"
`
	if _, err := parseCatalog(in); err == nil {
		t.Fatal("expected an error: model references an unknown provider")
	}
}

func TestParseCatalogRejectsEmpty(t *testing.T) {
	in := `
[[provider]]
name = "openai"
kind = "openai"
`
	if _, err := parseCatalog(in); err == nil {
		t.Fatal("expected an error: catalog has no models")
	}
}

func TestParseCatalogRejectsUnknownKind(t *testing.T) {
	in := `
[[provider]]
name = "x"
kind = "opneai"

[[model]]
id = "m"
provider = "x"
`
	if _, err := parseCatalog(in); err == nil {
		t.Fatal("expected an error: provider has an unknown kind")
	}
}

func TestConfigDirHonoursEnv(t *testing.T) {
	t.Setenv("FOVA_CONFIG_DIR", "/tmp/fova-cfg-xyz")
	if got := Dir(); got != "/tmp/fova-cfg-xyz" {
		t.Errorf("Dir = %q, want the env override", got)
	}
}

func TestLoadModelsMaterializesDefaultOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)

	c, err := LoadModels()
	if err != nil {
		t.Fatalf("LoadModels: %v", err)
	}
	if len(c.Models) == 0 {
		t.Fatal("materialized catalog is empty")
	}
	if _, err := os.Stat(filepath.Join(dir, "models.toml")); err != nil {
		t.Errorf("models.toml was not written on first run: %v", err)
	}
}

func TestLoadModelsReadsUserFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	custom := `
[[provider]]
name = "vllm"
kind = "openai"
base_url = "http://localhost:9999/v1"

[[model]]
id = "my-model"
display_name = "My Model"
provider = "vllm"
context_tokens = 8192
supports_tools = true
`
	if err := os.WriteFile(filepath.Join(dir, "models.toml"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadModels()
	if err != nil {
		t.Fatalf("LoadModels: %v", err)
	}
	if len(c.Models) != 1 || c.Models[0].ID != "my-model" {
		t.Fatalf("user file not used: %+v", c.Models)
	}
}

func TestLoadModelsRoundTripsOnSecondRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if _, err := LoadModels(); err != nil { // first run materializes the default
		t.Fatalf("first LoadModels: %v", err)
	}
	c, err := LoadModels() // second run reads the materialized file
	if err != nil {
		t.Fatalf("second LoadModels: %v", err)
	}
	if len(c.Models) == 0 {
		t.Fatal("second run returned an empty catalog")
	}
}

func TestLoadModelsRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "models.toml"),
		[]byte("not valid toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadModels(); err == nil {
		t.Fatal("expected an error for a malformed models.toml")
	}
}
