package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSystemPromptHasMarkerAndPreamble(t *testing.T) {
	p := DefaultSystemPrompt()
	if !strings.Contains(p, "{{COMMAND_CATALOGUE}}") {
		t.Error("embedded system.md is missing the {{COMMAND_CATALOGUE}} marker")
	}
	if !strings.Contains(p, "You are fova") {
		t.Error("embedded system.md is missing the preamble")
	}
	if !strings.Contains(p, "Do not invoke `jobs.cancel`") {
		t.Error("embedded system.md is missing the long-running-job rule")
	}
	// Snapshot guard for the v0.6 "Bug 3" cancel-threshold wording — a reword
	// of either phrase must go through the spec owner.
	if !strings.Contains(p, "`elapsed < estimated`") {
		t.Error("embedded system.md is missing the `elapsed < estimated` rule")
	}
	if !strings.Contains(p, "2 × estimated") {
		t.Error("embedded system.md is missing the 2 × estimated cancel threshold")
	}
}

func TestLoadSystemPromptValidFile(t *testing.T) {
	dir := t.TempDir()
	good := "You are fova.\n## Refusals\nno\n## Tone\nbrief\n{{COMMAND_CATALOGUE}}\n"
	if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, rep := loadSystemPrompt(dir)
	if prompt != good {
		t.Fatalf("prompt = %q, want the on-disk file", prompt)
	}
	if !rep.OK() || len(rep.Warnings) != 0 {
		t.Fatalf("a valid system.md should produce a clean report: %+v", rep)
	}
}

func TestLoadSystemPromptMissingMarkerFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("no marker here"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, rep := loadSystemPrompt(dir)
	if prompt != DefaultSystemPrompt() {
		t.Fatal("a system.md missing the marker must fall back to the embedded default")
	}
	if rep.OK() {
		t.Fatal("expected an error for the missing marker")
	}
}

func TestLoadSystemPromptMissingFileFallsBack(t *testing.T) {
	prompt, rep := loadSystemPrompt(t.TempDir())
	if prompt != DefaultSystemPrompt() {
		t.Fatal("a missing system.md must fall back to the embedded default")
	}
	if rep.OK() {
		t.Fatal("expected an error for the missing file")
	}
}

func TestLoadSystemPromptWarnsOnMissingRefusals(t *testing.T) {
	dir := t.TempDir()
	body := "You are fova.\n{{COMMAND_CATALOGUE}}\n"
	if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, rep := loadSystemPrompt(dir)
	if !rep.OK() || len(rep.Warnings) == 0 {
		t.Fatalf("expected a warning (not an error) for the missing Refusals/Tone section: %+v", rep)
	}
}
