package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/alvarogonjim/fova/internal/assets"
	"github.com/alvarogonjim/fova/internal/backends/local"
	jobmgr "github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/skills"
	"github.com/alvarogonjim/fova/internal/store"
)

// newTestInstaller builds a real local.Installer rooted at a temp dir so
// buildRegistry can wire plan.create with its installed-tool checker. The
// registry tests don't exercise plan.create's install path; the cross-check
// is verified in internal/tools/plan/plan_test.go.
func newTestInstaller(t *testing.T) *local.Installer {
	t.Helper()
	reg, err := local.LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	return local.NewInstaller(reg)
}

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
	reg := buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil), llm.NewModelRegistry(assets.DefaultCatalog()), assets.DefaultConfig(), newTestInstaller(t), skills.NewLoader(nil))
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
	reg := buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil), llm.NewModelRegistry(assets.DefaultCatalog()), assets.DefaultConfig(), newTestInstaller(t), skills.NewLoader(nil))
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
	reg := buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil), llm.NewModelRegistry(assets.DefaultCatalog()), assets.DefaultConfig(), newTestInstaller(t), skills.NewLoader(nil))
	for _, name := range []string{
		"knowledge.europepmc", "knowledge.openalex", "knowledge.s2",
		"knowledge.biorxiv", "knowledge.crossref", "knowledge.uniprot",
		"knowledge.pdb", "knowledge.interpro", "knowledge.web_fetch",
		"knowledge.web_search", "plan.create",
		// v0.7 Bug 3 — knowledge.corpus is now eight flat tools instead of
		// one umbrella with an action field. The agent calls each by name.
		"knowledge.corpus_add", "knowledge.corpus_add_from_search",
		"knowledge.corpus_search", "knowledge.corpus_grep",
		"knowledge.corpus_map", "knowledge.corpus_reduce",
		"knowledge.corpus_read", "knowledge.corpus_remove",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("registry missing %s", name)
		}
	}
	// The umbrella tool is fully removed — its absence guards against drift
	// reintroducing it. If a future commit re-registers it, this test fails
	// loudly.
	if _, ok := reg.Get("knowledge.corpus"); ok {
		t.Error("umbrella knowledge.corpus should not be registered post-v0.7 Bug 3")
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
	reg := buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil), llm.NewModelRegistry(assets.DefaultCatalog()), assets.DefaultConfig(), newTestInstaller(t), skills.NewLoader(nil))
	for _, name := range []string{
		"lab.targets_search", "lab.cost_estimate", "lab.submit_experiment",
		"lab.experiment_status", "lab.results",
	} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("registry missing %s", name)
		}
	}
}

func TestResolveFovaHomeEnvWins(t *testing.T) {
	t.Setenv("FOVA_HOME", "/tmp/env-home")
	cfg := assets.DefaultConfig()
	cfg.Defaults.DataDir = "/tmp/config-home"
	if got := resolveFovaHome(cfg); got != "/tmp/env-home" {
		t.Errorf("resolveFovaHome = %q, want /tmp/env-home", got)
	}
}

func TestResolveFovaHomeUsesConfig(t *testing.T) {
	t.Setenv("FOVA_HOME", "")
	cfg := assets.DefaultConfig()
	cfg.Defaults.DataDir = "/tmp/config-home"
	if got := resolveFovaHome(cfg); got != "/tmp/config-home" {
		t.Errorf("resolveFovaHome = %q, want /tmp/config-home", got)
	}
}

func TestResolveFovaHomeDefault(t *testing.T) {
	t.Setenv("FOVA_HOME", "")
	cfg := assets.DefaultConfig() // DataDir is ""
	got := resolveFovaHome(cfg)
	if got == "" || got == "/tmp/config-home" {
		t.Errorf("resolveFovaHome with no override = %q, want the ~/fova default", got)
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
