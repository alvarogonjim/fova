package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/alvarogonjim/proteus/internal/store"
)

func TestRootCommandHasSubcommands(t *testing.T) {
	root := newRootCmd()
	var names []string
	for _, c := range root.Commands() {
		names = append(names, c.Name())
	}
	wantVersion, wantTui := false, false
	for _, n := range names {
		if n == "version" {
			wantVersion = true
		}
		if n == "tui" {
			wantTui = true
		}
	}
	if !wantVersion || !wantTui {
		t.Fatalf("missing subcommands; got %v", names)
	}
}

func TestRunTUIWiresJobTools(t *testing.T) {
	// buildRegistry wires every v0.2 tool a TUI session exposes, including the
	// four jobs.* tools backed by a job manager.
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := buildRegistry(t.TempDir(), st)
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
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	reg := buildRegistry(t.TempDir(), st)
	for _, name := range []string{
		"design.bindcraft", "design.rfdiffusion", "design.proteinmpnn",
		"score.filter", "score.metrics", "score.ipsae",
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
