package design

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/store"
)

func TestBoltzGenToolNameAndConfirmation(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	_ = st
	tool := NewBoltzGenTool(ws, mgr, backend, st)
	if tool.Name() != "design.boltzgen" {
		t.Errorf("Name() = %q, want design.boltzgen", tool.Name())
	}
	if !tool.RequiresConfirmation(nil) {
		t.Error("design.boltzgen must require confirmation (long GPU job)")
	}
	if tool.Description() == "" {
		t.Error("Description() must be non-empty")
	}
}

func TestBoltzGenToolInputSchema(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	tool := NewBoltzGenTool(ws, mgr, backend, st)
	schema := tool.InputSchema()

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	// spec_path plus every BoltzGenParams field must be advertised.
	want := []string{
		"spec_path", "protocol", "num_designs", "budget", "diffusion_batch_size",
		"steps", "alpha", "filter_biased", "additional_filters",
		"refolding_rmsd_threshold", "inverse_fold_num_sequences",
		"inverse_fold_avoid", "step_scale", "noise_scale", "reuse",
	}
	for _, k := range want {
		if _, ok := props[k]; !ok {
			t.Errorf("InputSchema must advertise %q", k)
		}
	}
	// spec_path is required.
	req, _ := schema["required"].([]string)
	foundSpec := false
	for _, r := range req {
		if r == "spec_path" {
			foundSpec = true
		}
	}
	if !foundSpec {
		t.Error("spec_path must be in the required list")
	}
	// protocol carries an enum.
	protocol, _ := props["protocol"].(map[string]any)
	if _, ok := protocol["enum"]; !ok {
		t.Error("protocol property must advertise an enum")
	}
	// steps items carry an enum.
	steps, _ := props["steps"].(map[string]any)
	items, _ := steps["items"].(map[string]any)
	if _, ok := items["enum"]; !ok {
		t.Error("steps items must advertise an enum")
	}
}

func TestBoltzGenExecuteRejectsMissingSpecPath(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	tool := NewBoltzGenTool(ws, mgr, backend, st)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"num_designs":4}`))
	if err == nil {
		t.Fatal("Execute must reject a request with no spec_path")
	}
	if !contains(err.Error(), "spec_path") {
		t.Errorf("error %q should mention spec_path", err)
	}
}

func TestBoltzGenExecuteSubmitsJobAndResolvesSpecPath(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t,
		`{"designs":[{"sequence":{"A":"ACDE"},"structure_file":"/tmp/d.cif","scores":{"iptm":0.8}}]}`)
	// Stage a spec file inside the workspace so resolution succeeds.
	specRel := "specs/binder.yaml"
	specAbs := filepath.Join(ws, specRel)
	if err := os.MkdirAll(filepath.Dir(specAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specAbs, []byte("entities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewBoltzGenTool(ws, mgr, backend, st)
	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"spec_path":"specs/binder.yaml","protocol":"peptide-anything","num_designs":4,"budget":2}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.JobID == "" {
		t.Fatal("Execute must return a JobID")
	}
	job := waitJob(t, mgr, res.JobID)
	if job.Status != domain.JobSucceeded {
		t.Fatalf("job did not succeed: status=%s err=%s", job.Status, job.Error)
	}
	// The backend must have seen spec_path resolved to an absolute path.
	var seen boltzGenInput
	if err := json.Unmarshal(backend.lastIn, &seen); err != nil {
		t.Fatalf("backend input not valid JSON: %v", err)
	}
	if seen.SpecPath != specAbs {
		t.Errorf("backend saw spec_path %q, want resolved absolute %q", seen.SpecPath, specAbs)
	}
	if seen.Protocol != "peptide-anything" {
		t.Errorf("params must reach the backend: protocol = %q", seen.Protocol)
	}
	// The design must have been persisted.
	designs, err := st.ListDesigns(store.DefaultProjectID)
	if err != nil {
		t.Fatalf("ListDesigns: %v", err)
	}
	if len(designs) != 1 {
		t.Fatalf("want 1 persisted design, got %d", len(designs))
	}
	if designs[0].Origin != domain.OriginBoltzGen {
		t.Errorf("persisted design origin = %q, want boltzgen", designs[0].Origin)
	}
}

func TestBoltzGenExecuteRejectsEscapingSpecPath(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	tool := NewBoltzGenTool(ws, mgr, backend, st)
	_, err := tool.Execute(context.Background(),
		json.RawMessage(`{"spec_path":"../../etc/passwd"}`))
	if err == nil {
		t.Fatal("Execute must reject a spec_path that escapes the workspace")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
