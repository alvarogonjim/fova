package viz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPyMolRenderMissingBinary(t *testing.T) {
	// Force LookPath to report "not found" regardless of the host PyMOL.
	prev := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	defer func() { lookPath = prev }()

	tool := NewPyMolRender(t.TempDir())
	pdb := writePDB(t, twoChainPDB)
	in, _ := json.Marshal(map[string]any{"pdb": pdb})
	_, err := tool.Execute(context.Background(), in)
	if err == nil {
		t.Fatal("expected an error when pymol is not on PATH")
	}
	if !strings.Contains(err.Error(), "pymol not found") {
		t.Errorf("error = %v, want one mentioning 'pymol not found'", err)
	}
}

func TestPyMolRenderMissingPDB(t *testing.T) {
	prev := lookPath
	lookPath = func(string) (string, error) { return "/usr/bin/true", nil }
	defer func() { lookPath = prev }()

	tool := NewPyMolRender(t.TempDir())
	in, _ := json.Marshal(map[string]any{"pdb": "/no/such.pdb"})
	if _, err := tool.Execute(context.Background(), in); err == nil {
		t.Fatal("expected an error when the pdb does not exist")
	}
}

// TestPyMolRenderE2E exercises the real PyMOL binary. It runs only when
// FOVA_E2E_PYMOL=1 is set in the environment AND pymol is on PATH.
func TestPyMolRenderE2E(t *testing.T) {
	if os.Getenv("FOVA_E2E_PYMOL") != "1" {
		t.Skip("set FOVA_E2E_PYMOL=1 to exercise the real pymol binary")
	}
	if _, err := exec.LookPath("pymol"); err != nil {
		t.Skipf("pymol not on PATH: %v", err)
	}
	ws := t.TempDir()
	tool := NewPyMolRender(ws)
	pdb := writePDB(t, twoChainPDB)
	in, _ := json.Marshal(map[string]any{"pdb": pdb})
	res, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(res.Output, &out)
	body, err := os.ReadFile(out.Path)
	if err != nil {
		t.Fatalf("read png: %v", err)
	}
	if !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("file is not a PNG (header = %q)", body[:8])
	}
}
