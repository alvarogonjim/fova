package design

import (
	"context"
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

func TestRFantibodyExecuteRejectsBadInput(t *testing.T) {
	tool := NewRFAntibodyTool(t.TempDir(), nil, nil, nil)
	// No target/hotspots — Validate rejects before any job/store access.
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected a validation error when target/hotspots are missing")
	}
}
