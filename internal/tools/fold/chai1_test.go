package fold

import (
	"encoding/json"
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
