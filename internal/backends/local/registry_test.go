package local

import (
	"strings"
	"testing"
)

func TestLoadRegistryParsesTools(t *testing.T) {
	r, err := LoadRegistry("/home/u/proteus")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	for _, name := range []string{"ipsae", "proteinmpnn", "rfdiffusion", "bindcraft"} {
		if _, ok := r.Tool(name); !ok {
			t.Errorf("tool %q missing", name)
		}
	}
	if len(r.Tools()) < 8 {
		t.Errorf("expected >=8 tools, got %d", len(r.Tools()))
	}
}

func TestRecipePlaceholdersExpanded(t *testing.T) {
	r, _ := LoadRegistry("/home/u/proteus")
	rec, ok := r.Tool("proteinmpnn")
	if !ok {
		t.Fatal("proteinmpnn missing")
	}
	// ${PROTEUS_HOME} expanded.
	if rec.VenvDir != "/home/u/proteus/tools/proteinmpnn/.venv" {
		t.Errorf("venv_dir = %q", rec.VenvDir)
	}
	// install_dir defaulted and expanded.
	if rec.InstallDir != "/home/u/proteus/tools/proteinmpnn" {
		t.Errorf("install_dir = %q", rec.InstallDir)
	}
	// {{ }} placeholders expanded in install_steps — no braces should remain.
	for _, s := range rec.InstallSteps {
		if strings.Contains(s, "{{") {
			t.Errorf("unexpanded placeholder in step: %q", s)
		}
		if strings.Contains(s, "${PROTEUS_HOME}") {
			t.Errorf("unexpanded ${PROTEUS_HOME} in step: %q", s)
		}
	}
	// git_ref defaults to "main".
	if rec.GitRef != "main" {
		t.Errorf("git_ref = %q, want main", rec.GitRef)
	}
}

func TestRunCommandKeepsRuntimePlaceholders(t *testing.T) {
	r, _ := LoadRegistry("/home/u/proteus")
	rec, _ := r.Tool("bindcraft")
	// run_command keeps runtime placeholders (filled by the runner, not here).
	if !strings.Contains(rec.RunCommand, "{{ input_json }}") {
		t.Errorf("run_command lost its runtime placeholder: %q", rec.RunCommand)
	}
	// but recipe-field placeholders ARE expanded.
	if strings.Contains(rec.RunCommand, "{{ venv_dir }}") {
		t.Errorf("run_command venv_dir not expanded: %q", rec.RunCommand)
	}
}

func TestDataAssets(t *testing.T) {
	r, _ := LoadRegistry("/home/u/proteus")
	a, ok := r.DataAsset("alphafold_params")
	if !ok {
		t.Fatal("alphafold_params missing")
	}
	if a.ExtractTo != "/home/u/proteus/data/alphafold_params" {
		t.Errorf("extract_to = %q", a.ExtractTo)
	}
}
