package main

import (
	"bytes"
	"path/filepath"
	"testing"

	jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/store"
)

func TestRootCommandHasSubcommands(t *testing.T) {
	root := newRootCmd()
	var names []string
	for _, c := range root.Commands() {
		names = append(names, c.Name())
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	if !have["version"] || !have["tui"] {
		t.Fatalf("missing version/tui subcommand; got %v", names)
	}
	for _, gone := range []string{"install", "uninstall", "list", "doctor", "modal"} {
		if have[gone] {
			t.Errorf("subcommand %q should have been removed; got %v", gone, names)
		}
	}
}

func TestRunTUIWiresJobTools(t *testing.T) {
	// buildRegistry wires every v0.2 tool a TUI session exposes, including the
	// four jobs.* tools backed by a job manager.
	t.Setenv("ADAPTYV_API_TOKEN", "test-token") // deterministic: skip the keychain
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil), llm.NewModelRegistry())
	for _, name := range []string{"jobs.list", "jobs.status", "jobs.cancel", "jobs.result"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("registry missing %s", name)
		}
	}
	// A non-jobs v0.2 tool is still present.
	if _, ok := reg.Get("fold.esmfold"); !ok {
		t.Error("registry missing fold.esmfold")
	}
}

func TestRunTUIWiresDesignAndScoreTools(t *testing.T) {
	t.Setenv("ADAPTYV_API_TOKEN", "test-token") // deterministic: skip the keychain
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil), llm.NewModelRegistry())
	for _, name := range []string{
		"design.bindcraft", "design.rfdiffusion", "design.proteinmpnn",
		"design.rfantibody", "design.chai2", "design.rfdiffusion2", "design.ligandmpnn",
		"fold.boltz2", "fold.chai1",
		"score.filter", "score.metrics", "score.ipsae",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("registry missing %s", name)
		}
	}
}

func TestRunTUIWiresKnowledgeAndPlanTools(t *testing.T) {
	// buildRegistry wires the v0.3 knowledge and planning tools.
	t.Setenv("ADAPTYV_API_TOKEN", "test-token") // deterministic: skip the keychain
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil), llm.NewModelRegistry())
	for _, name := range []string{
		"knowledge.europepmc", "knowledge.openalex", "knowledge.s2",
		"knowledge.biorxiv", "knowledge.crossref", "knowledge.uniprot",
		"knowledge.pdb", "knowledge.interpro", "knowledge.web_fetch",
		"knowledge.web_search", "knowledge.corpus", "plan.create",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("registry missing %s", name)
		}
	}
}

func TestRunTUIWiresLabTools(t *testing.T) {
	// buildRegistry wires the v0.4 Adaptyv wet-lab tools.
	t.Setenv("ADAPTYV_API_TOKEN", "test-token") // deterministic: skip the keychain
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil), llm.NewModelRegistry())
	for _, name := range []string{
		"lab.targets_search", "lab.cost_estimate", "lab.submit_experiment",
		"lab.experiment_status", "lab.results",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("registry missing %s", name)
		}
	}
}

func TestVersionCommandPrints(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("version command printed nothing")
	}
}
