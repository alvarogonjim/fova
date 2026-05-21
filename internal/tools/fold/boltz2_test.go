package fold

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBoltz2ToolSchema(t *testing.T) {
	tool := NewBoltz2("/ws", nil, nil)
	if tool.Name() != "fold.boltz2" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{"entities", "recycling_steps", "sampling_steps",
		"diffusion_samples", "step_scale", "output_format", "save_as"} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestBoltz2ToolRequiresConfirmation(t *testing.T) {
	tool := NewBoltz2("/ws", nil, nil)
	if !tool.RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("fold.boltz2 must require confirmation — the agent's spec goes through the gate")
	}
}

func TestPreflightBoltz2(t *testing.T) {
	cases := []struct {
		name string
		req  boltz2Request
		ok   bool
	}{
		{"valid protein", boltz2Request{Entities: []boltz2Entity{
			{Type: "protein", ID: chainIDs{"A"}, Sequence: "MKQ", MSA: "empty"}}}, true},
		{"valid ligand", boltz2Request{Entities: []boltz2Entity{
			{Type: "ligand", ID: chainIDs{"L"}, SMILES: "CCO"}}}, true},
		{"no entities", boltz2Request{}, false},
		{"bad type", boltz2Request{Entities: []boltz2Entity{
			{Type: "peptide", ID: chainIDs{"A"}, Sequence: "MKQ"}}}, false},
		{"empty protein sequence", boltz2Request{Entities: []boltz2Entity{
			{Type: "protein", ID: chainIDs{"A"}}}}, false},
		{"bad amino acid", boltz2Request{Entities: []boltz2Entity{
			{Type: "protein", ID: chainIDs{"A"}, Sequence: "MKQ1"}}}, false},
		{"ligand without smiles or ccd", boltz2Request{Entities: []boltz2Entity{
			{Type: "ligand", ID: chainIDs{"L"}}}}, false},
		{"ligand with both", boltz2Request{Entities: []boltz2Entity{
			{Type: "ligand", ID: chainIDs{"L"}, SMILES: "CCO", CCD: "ATP"}}}, false},
		{"duplicate chain id", boltz2Request{Entities: []boltz2Entity{
			{Type: "protein", ID: chainIDs{"A"}, Sequence: "MKQ"},
			{Type: "protein", ID: chainIDs{"A"}, Sequence: "MKQ"}}}, false},
		{"step_scale out of range", boltz2Request{Entities: []boltz2Entity{
			{Type: "protein", ID: chainIDs{"A"}, Sequence: "MKQ"}}, StepScale: ptr(3.0)}, false},
		{"bad output_format", boltz2Request{Entities: []boltz2Entity{
			{Type: "protein", ID: chainIDs{"A"}, Sequence: "MKQ"}}, OutputFormat: "xyz"}, false},
	}
	for _, c := range cases {
		err := preflightBoltz2(c.req)
		if c.ok && err != nil {
			t.Errorf("%s: want valid, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: want invalid, got nil", c.name)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestBoltz2ExecuteRejectsBadInput(t *testing.T) {
	tool := NewBoltz2(t.TempDir(), nil, nil)
	// Invalid: no entities. Must error WITHOUT panicking on the nil manager —
	// preflight rejects it before any job submit.
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"entities":[]}`))
	if err == nil {
		t.Fatal("expected a preflight error for empty entities")
	}
}

func TestBoltz2ExecuteSubmitsJob(t *testing.T) {
	// newFoldTestDeps is the existing helper in foldjob_test.go — it returns a
	// real *jobs.Manager (SQLite store under t.TempDir()) and a stubBackend.
	mgr, backend := newFoldTestDeps(t, `{"designs":[]}`)
	tool := NewBoltz2(t.TempDir(), mgr, backend)
	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"entities":[{"type":"protein","id":"A","sequence":"MKQ"}]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.JobID == "" {
		t.Error("Execute must return a job id")
	}
}
