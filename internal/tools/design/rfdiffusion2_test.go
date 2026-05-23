package design

import (
	"encoding/json"
	"testing"
)

func TestRFdiffusion2ToolSchema(t *testing.T) {
	tool := NewRFdiffusion2Tool("/ws", nil, nil, nil)
	if tool.Name() != "design.rfdiffusion2" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{
		"benchmark", "motif_pdb", "contigs", "num_designs", "seed",
		"guidepost_xyz_as_design_bb", "idealize_sidechain_outputs", "stop_step",
	} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestRFdiffusion2ToolRequiresConfirmation(t *testing.T) {
	if !NewRFdiffusion2Tool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.rfdiffusion2 must require confirmation — GPU design job")
	}
}
