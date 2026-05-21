package fold

import (
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
