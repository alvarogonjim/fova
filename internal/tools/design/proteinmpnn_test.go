package design

import (
	"encoding/json"
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
