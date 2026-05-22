package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsFirstRunTrueWhenConfigAbsent(t *testing.T) {
	t.Setenv("FOVA_CONFIG_DIR", t.TempDir())
	if !isFirstRun() {
		t.Error("isFirstRun should be true when config.toml is absent")
	}
}

func TestIsFirstRunFalseWhenConfigPresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[ui]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isFirstRun() {
		t.Error("isFirstRun should be false when config.toml exists")
	}
}
