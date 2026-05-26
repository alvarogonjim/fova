package fold

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestChai1ToolSchema(t *testing.T) {
	tool := NewChai1("/ws", nil, nil)
	if tool.Name() != "fold.chai1" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{"entities", "msa", "restraints", "templates",
		"num_trunk_recycles", "num_diffn_timesteps", "num_diffn_samples",
		"num_trunk_samples", "recycle_msa_subsample", "seed", "save_as"} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestChai1ToolRequiresConfirmation(t *testing.T) {
	if !NewChai1("/ws", nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("fold.chai1 must require confirmation — the agent's spec goes through the gate")
	}
}

func TestPreflightChai1(t *testing.T) {
	cases := []struct {
		name string
		req  chai1Request
		ok   bool
	}{
		{"valid protein", chai1Request{Entities: []chai1Entity{
			{Type: "protein", ID: "A", Sequence: "MKQ"}}}, true},
		{"valid ligand", chai1Request{Entities: []chai1Entity{
			{Type: "ligand", ID: "L", SMILES: "CCO"}}}, true},
		{"valid glycan", chai1Request{Entities: []chai1Entity{
			{Type: "glycan", ID: "G", Glycan: "NAG"}}}, true},
		{"no entities", chai1Request{}, false},
		{"bad type", chai1Request{Entities: []chai1Entity{
			{Type: "peptide", ID: "A", Sequence: "MKQ"}}}, false},
		{"empty protein sequence", chai1Request{Entities: []chai1Entity{
			{Type: "protein", ID: "A"}}}, false},
		{"ligand without smiles", chai1Request{Entities: []chai1Entity{
			{Type: "ligand", ID: "L"}}}, false},
		{"duplicate id", chai1Request{Entities: []chai1Entity{
			{Type: "protein", ID: "A", Sequence: "MKQ"},
			{Type: "ligand", ID: "A", SMILES: "CCO"}}}, false},
		{"restraint bad connection_type", chai1Request{
			Entities:   []chai1Entity{{Type: "protein", ID: "A", Sequence: "MKQ"}},
			Restraints: []chai1Restraint{{ConnectionType: "bond", ChainA: "A", ChainB: "A"}}}, false},
		{"restraint unknown chain", chai1Request{
			Entities:   []chai1Entity{{Type: "protein", ID: "A", Sequence: "MKQ"}},
			Restraints: []chai1Restraint{{ConnectionType: "contact", ChainA: "A", ChainB: "Z"}}}, false},
		{"recycles non-positive", chai1Request{
			Entities:         []chai1Entity{{Type: "protein", ID: "A", Sequence: "MKQ"}},
			NumTrunkRecycles: ptrInt(0)}, false},
	}
	for _, c := range cases {
		err := preflightChai1(c.req)
		if c.ok && err != nil {
			t.Errorf("%s: want valid, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: want invalid, got nil", c.name)
		}
	}
}

func ptrInt(v int) *int { return &v }

func TestChai1ExecuteRejectsBadInput(t *testing.T) {
	tool := NewChai1(t.TempDir(), nil, nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"entities":[]}`)); err == nil {
		t.Fatal("expected a preflight error for empty entities")
	}
}

func TestChai1ExecuteSubmitsJob(t *testing.T) {
	mgr, backend := newFoldTestDeps(t, `{"designs":[]}`)
	tool := NewChai1(t.TempDir(), mgr, backend)
	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"entities":[{"type":"protein","id":"A","sequence":"MKQ"}]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.JobID == "" {
		t.Fatal("Execute must return a job id")
	}
	// Wait for the submitted job to finish before the test returns —
	// otherwise t.Cleanup closing the store races the job's goroutine.
	waitJob(t, mgr, res.JobID)
}

// TestChai1ValidateRejectsInvalidJSON exercises the malformed-bytes branch of
// Validate: the editable gate must surface a clear "invalid JSON" error so the
// user sees an editor reopen with a fix-it hint rather than a stack trace.
func TestChai1ValidateRejectsInvalidJSON(t *testing.T) {
	tool := NewChai1("/ws", nil, nil)
	err := tool.Validate(json.RawMessage(`not json at all`))
	if err == nil {
		t.Fatal("Validate must reject malformed JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error %q must mention \"invalid JSON\"", err)
	}
}

// TestChai1ValidatePassesGoodInput is the happy path: a minimal valid
// single-protein request must return nil so the editable gate accepts the
// user's edit without re-opening the editor.
func TestChai1ValidatePassesGoodInput(t *testing.T) {
	tool := NewChai1("/ws", nil, nil)
	input := json.RawMessage(`{"entities":[{"type":"protein","id":"A","sequence":"MKQ"}]}`)
	if err := tool.Validate(input); err != nil {
		t.Fatalf("Validate(valid request) = %v, want nil", err)
	}
}

// TestChai1ValidateSurfacesPreflightError feeds a request that parses cleanly
// but trips preflight (duplicate chain id). The Validator must return the
// preflight error verbatim so the editable gate pins the same diagnostic the
// user would see from Execute.
func TestChai1ValidateSurfacesPreflightError(t *testing.T) {
	tool := NewChai1("/ws", nil, nil)
	// Two entities sharing chain id "A" — preflightChai1 emits a
	// "used more than once" diagnostic that the editable gate must surface.
	input := json.RawMessage(`{"entities":[` +
		`{"type":"protein","id":"A","sequence":"MKQ"},` +
		`{"type":"ligand","id":"A","smiles":"CCO"}]}`)
	err := tool.Validate(input)
	if err == nil {
		t.Fatal("Validate must surface preflight errors")
	}
	if !strings.Contains(err.Error(), "used more than once") {
		t.Errorf("error %q must contain the preflight diagnostic \"used more than once\"", err)
	}
}
