package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestListToolsCommand(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"list", "tools"})
	if err := root.Execute(); err != nil {
		t.Fatalf("list tools: %v", err)
	}
	out := buf.String()
	for _, name := range []string{"ipsae", "bindcraft", "proteinmpnn"} {
		if !strings.Contains(out, name) {
			t.Errorf("list tools output missing %q:\n%s", name, out)
		}
	}
}

func TestInstallDryRunCommand(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"install", "ipsae", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("install --dry-run: %v", err)
	}
	if !strings.Contains(buf.String(), "git clone") {
		t.Errorf("dry-run did not print the install steps:\n%s", buf.String())
	}
}

func TestDoctorCommand(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"doctor"})
	if err := root.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(buf.String(), "Local protein tools") {
		t.Errorf("doctor output missing the tools section:\n%s", buf.String())
	}
}

func TestRootHasInstallCommands(t *testing.T) {
	root := newRootCmd()
	var names []string
	for _, c := range root.Commands() {
		names = append(names, c.Name())
	}
	for _, want := range []string{"install", "uninstall", "list", "doctor"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("root command missing %q; got %v", want, names)
		}
	}
}
