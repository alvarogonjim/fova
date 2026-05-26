package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeWritesMissingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := materializeAssets(dir); err != nil {
		t.Fatalf("materializeAssets: %v", err)
	}
	for _, rel := range []string{"config.toml", "models.toml", "system.md", "skills/design-binder.md"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected %s materialized: %v", rel, err)
		}
	}
}

func TestMaterializeNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(skills, "design-binder.md")
	if err := os.WriteFile(custom, []byte("EDITED BY USER"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := materializeAssets(dir); err != nil {
		t.Fatalf("materializeAssets: %v", err)
	}
	body, _ := os.ReadFile(custom)
	if string(body) != "EDITED BY USER" {
		t.Fatalf("materialize overwrote a user-edited file: %q", body)
	}
}

func TestMaterializeSecondRunIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := materializeAssets(dir); err != nil {
		t.Fatal(err)
	}
	if err := materializeAssets(dir); err != nil {
		t.Fatalf("second materializeAssets: %v", err)
	}
}
