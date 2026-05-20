package main

import (
	"bytes"
	"testing"
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
