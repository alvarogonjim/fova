package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModalDeployWritesFunctionsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PROTEUS_HOME", home)

	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"modal", "deploy"})
	// The command writes functions.py, then tries to run the `modal` CLI.
	// In CI the CLI is absent, so Execute returns an error — that is fine;
	// the functions.py file must still have been written.
	_ = root.Execute()

	written := filepath.Join(home, "modal", "functions.py")
	data, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("functions.py not written: %v", err)
	}
	if !strings.Contains(string(data), "proteus-tools") {
		t.Error("written functions.py has unexpected content")
	}
}

func TestRootHasModalCommand(t *testing.T) {
	root := newRootCmd()
	for _, c := range root.Commands() {
		if c.Name() == "modal" {
			return
		}
	}
	t.Error("root command missing `modal`")
}
