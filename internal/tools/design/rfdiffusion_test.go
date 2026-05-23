package design

import (
	"encoding/json"
	"testing"
)

func TestRFdiffusionToolSchema(t *testing.T) {
	tool := NewRFdiffusionTool("/ws", nil, nil, nil)
	if tool.Name() != "design.rfdiffusion" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{
		"target", "hotspots", "contigs", "num_designs", "deterministic",
		"symmetric", "symmetry_kind", "n_chains", "partial_t",
		"noise_scale_ca", "guiding_potentials", "guide_scale",
	} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
}

func TestRFdiffusionToolRequiresConfirmation(t *testing.T) {
	if !NewRFdiffusionTool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.rfdiffusion must require confirmation — GPU design job")
	}
}
