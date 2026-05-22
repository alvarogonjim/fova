package design

import (
	"encoding/json"
	"testing"
)

func TestRFantibodyToolSchema(t *testing.T) {
	tool := NewRFAntibodyTool("/ws", nil, nil, nil)
	if tool.Name() != "design.rfantibody" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{"target", "hotspots", "framework",
		"framework_pdb", "design_loops", "num_designs", "seqs_per_struct",
		"temperature", "num_recycles", "seed", "hotspot_show_prop"} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestRFantibodyToolRequiresConfirmation(t *testing.T) {
	if !NewRFAntibodyTool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.rfantibody must require confirmation — GPU design job")
	}
}
