package design

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProteinMPNNToolSchema(t *testing.T) {
	tool := NewProteinMPNNTool("/ws", nil, nil, nil)
	if tool.Name() != "design.proteinmpnn" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{
		"pdb", "num_designs", "batch_size", "sampling_temp", "seed",
		"chains_to_design", "fixed_positions", "omit_AAs",
		"bias_AA", "bias_by_residue", "tied_positions", "save_score",
	} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestProteinMPNNToolRequiresConfirmation(t *testing.T) {
	if !NewProteinMPNNTool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.proteinmpnn must require confirmation")
	}
}

func TestProteinMPNNExecuteRejectsBadInput(t *testing.T) {
	tool := NewProteinMPNNTool(t.TempDir(), nil, nil, nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected a validation error when pdb is missing")
	}
}

// Path resolution coverage previously exercised on the shared designTool via
// the `target` field — now retargeted at the bespoke tool's typed `pdb` field.
func TestProteinMPNNResolvesRelativePDBAgainstWorkspace(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	if err := os.MkdirAll(filepath.Join(ws, "inputs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "inputs", "x.pdb"), []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewProteinMPNNTool(ws, mgr, backend, st)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"pdb":"inputs/x.pdb"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	waitJob(t, mgr, res.JobID)

	if backend.lastIn == nil {
		t.Fatal("backend.Run was not called")
	}
	var got map[string]any
	if err := json.Unmarshal(backend.lastIn, &got); err != nil {
		t.Fatalf("backend input is not valid JSON: %v", err)
	}
	want := filepath.Join(ws, "inputs", "x.pdb")
	if got["pdb"] != want {
		t.Errorf("backend saw pdb=%q, want %q", got["pdb"], want)
	}
}

func TestProteinMPNNRejectsAbsoluteOutsideWorkspace(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	outside := filepath.Join(t.TempDir(), "outside.pdb")
	if err := os.WriteFile(outside, []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewProteinMPNNTool(ws, mgr, backend, st)
	body, _ := json.Marshal(map[string]string{"pdb": outside})
	if _, err := tool.Execute(context.Background(), body); err == nil {
		t.Fatal("expected an 'escapes the workspace' error")
	} else if !strings.Contains(err.Error(), "escapes the workspace") {
		t.Errorf("error %q must mention 'escapes the workspace'", err)
	}
}

func TestProteinMPNNRejectsPathTraversal(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	tool := NewProteinMPNNTool(ws, mgr, backend, st)
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"pdb":"../../etc/passwd"}`)); err == nil {
		t.Fatal("expected an 'escapes the workspace' error")
	} else if !strings.Contains(err.Error(), "escapes the workspace") {
		t.Errorf("error %q must mention 'escapes the workspace'", err)
	}
}
