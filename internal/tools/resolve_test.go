package tools

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveWorkspacePathRelative(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveWorkspacePath(root, "designs/d_0003.pdb")
	if err != nil {
		t.Fatalf("ResolveWorkspacePath: %v", err)
	}
	want := filepath.Join(root, "designs", "d_0003.pdb")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveWorkspacePathAbsoluteInside(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "designs", "d_0003.pdb")
	got, err := ResolveWorkspacePath(root, abs)
	if err != nil {
		t.Fatalf("ResolveWorkspacePath: %v", err)
	}
	if got != abs {
		t.Errorf("got %q, want %q", got, abs)
	}
}

func TestResolveWorkspacePathAbsoluteOutsideRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("absolute-path semantics differ on Windows")
	}
	root := t.TempDir()
	if _, err := ResolveWorkspacePath(root, "/etc/passwd"); err == nil {
		t.Fatal("expected an 'escapes the workspace' error")
	} else if !strings.Contains(err.Error(), "escapes the workspace") {
		t.Errorf("error %q must mention 'escapes the workspace'", err)
	}
}

func TestResolveWorkspacePathTraversalRejected(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveWorkspacePath(root, "../../etc/passwd"); err == nil {
		t.Fatal("expected an 'escapes the workspace' error")
	} else if !strings.Contains(err.Error(), "escapes the workspace") {
		t.Errorf("error %q must mention 'escapes the workspace'", err)
	}
}

func TestResolveWorkspacePathEmpty(t *testing.T) {
	got, err := ResolveWorkspacePath(t.TempDir(), "")
	if err != nil {
		t.Fatalf("empty path must not error, got %v", err)
	}
	if got != "" {
		t.Errorf("empty path must round-trip empty, got %q", got)
	}
}
