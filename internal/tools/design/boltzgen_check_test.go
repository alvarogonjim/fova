package design

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBoltzGenCheckToolNameAndSchema(t *testing.T) {
	_, _, backend, ws := newTestDeps(t, `{"valid":true,"errors":[],"visualization_path":""}`)
	tool := NewBoltzGenCheckTool(ws, backend)

	if tool.Name() != "design.boltzgen_check" {
		t.Errorf("Name() = %q, want design.boltzgen_check", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() must be non-empty")
	}
	// boltzgen check is cheap — no GPU, no weights — so no confirmation.
	if tool.RequiresConfirmation(nil) {
		t.Error("design.boltzgen_check must NOT require confirmation (cheap, no GPU)")
	}
	if tool.EstimatedCostUSD(nil) != 0 {
		t.Error("design.boltzgen_check should cost nothing")
	}

	schema := tool.InputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	if _, ok := props["spec_path"]; !ok {
		t.Error("InputSchema must advertise spec_path")
	}
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
}

func TestBoltzGenCheckRejectsMissingSpecPath(t *testing.T) {
	_, _, backend, ws := newTestDeps(t, `{"valid":true,"errors":[]}`)
	tool := NewBoltzGenCheckTool(ws, backend)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("Execute must reject a request with no spec_path")
	}
	if !contains(err.Error(), "spec_path") {
		t.Errorf("error %q should mention spec_path", err)
	}
}

func TestBoltzGenCheckRejectsEscapingSpecPath(t *testing.T) {
	_, _, backend, ws := newTestDeps(t, `{"valid":true,"errors":[]}`)
	tool := NewBoltzGenCheckTool(ws, backend)
	_, err := tool.Execute(context.Background(),
		json.RawMessage(`{"spec_path":"../../etc/passwd"}`))
	if err == nil {
		t.Fatal("Execute must reject a spec_path that escapes the workspace")
	}
}

func TestBoltzGenCheckValidSpec(t *testing.T) {
	out := `{"valid":true,"errors":[],"visualization_path":"/tmp/in_viz.cif"}`
	_, _, backend, ws := newTestDeps(t, out)
	specAbs := writeWorkspaceSpec(t, ws, "specs/binder.yaml")

	tool := NewBoltzGenCheckTool(ws, backend)
	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"spec_path":"specs/binder.yaml"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.JobID != "" {
		t.Error("design.boltzgen_check runs synchronously — it must NOT return a JobID")
	}
	// The result JSON must be the {valid, errors, visualization_path} contract.
	var result boltzGenCheckResult
	if err := json.Unmarshal(res.Output, &result); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}
	if !result.Valid {
		t.Error("a valid spec must yield valid:true")
	}
	if len(result.Errors) != 0 {
		t.Errorf("a valid spec must yield no errors, got %v", result.Errors)
	}
	if result.VisualizationPath != "/tmp/in_viz.cif" {
		t.Errorf("visualization_path = %q, want it carried through", result.VisualizationPath)
	}
	// The backend must have seen spec_path resolved to an absolute path.
	var seen boltzGenCheckInput
	if err := json.Unmarshal(backend.lastIn, &seen); err != nil {
		t.Fatalf("backend input not valid JSON: %v", err)
	}
	if seen.SpecPath != specAbs {
		t.Errorf("backend saw spec_path %q, want resolved absolute %q", seen.SpecPath, specAbs)
	}
}

func TestBoltzGenCheckInvalidSpec(t *testing.T) {
	out := `{"valid":false,"errors":["entity B: sequence is required"],"visualization_path":""}`
	_, _, backend, ws := newTestDeps(t, out)
	writeWorkspaceSpec(t, ws, "specs/bad.yaml")

	tool := NewBoltzGenCheckTool(ws, backend)
	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"spec_path":"specs/bad.yaml"}`))
	if err != nil {
		t.Fatalf("Execute must not error for an invalid spec — it reports valid:false: %v", err)
	}
	var result boltzGenCheckResult
	if err := json.Unmarshal(res.Output, &result); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}
	if result.Valid {
		t.Error("an invalid spec must yield valid:false")
	}
	if len(result.Errors) == 0 {
		t.Error("an invalid spec must populate errors")
	}
}

// writeWorkspaceSpec stages a minimal spec YAML at rel inside ws and returns
// its absolute path.
func writeWorkspaceSpec(t *testing.T, ws, rel string) string {
	t.Helper()
	abs := filepath.Join(ws, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("entities: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return abs
}
