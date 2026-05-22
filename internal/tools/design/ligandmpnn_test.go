package design

import (
	"encoding/json"
	"testing"
)

func TestLigandMPNNToolSchema(t *testing.T) {
	tool := NewLigandMPNNTool("/ws", nil, nil, nil)
	if tool.Name() != "design.ligandmpnn" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{"model_type", "pdb", "num_designs",
		"temperature", "redesigned_residues", "bias_AA", "pack_side_chains",
		"symmetry_residues", "global_transmembrane_label"} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestLigandMPNNToolRequiresConfirmation(t *testing.T) {
	if !NewLigandMPNNTool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.ligandmpnn must require confirmation — GPU design job")
	}
}
