package design

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBindCraftToolSchema(t *testing.T) {
	tool := NewBindCraftTool("/ws", nil, nil, nil)
	if tool.Name() != "design.bindcraft" {
		t.Errorf("Name = %q", tool.Name())
	}
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema has no properties map")
	}
	for _, key := range []string{
		"binder_name", "starting_pdb", "chains", "target_hotspot_residues",
		"length_min", "length_max", "number_of_final_designs", "binder_chain",
		"design_runs", "protocol_name", "template_pdb", "omit_aas",
	} {
		if _, present := props[key]; !present {
			t.Errorf("schema missing %q", key)
		}
	}
	// Opaque `settings` is GONE after bespoke.
	if _, present := props["settings"]; present {
		t.Error("typed BindCraft schema must not advertise an opaque settings field")
	}
}

func TestBindCraftToolRequiresConfirmation(t *testing.T) {
	if !NewBindCraftTool("/ws", nil, nil, nil).RequiresConfirmation(json.RawMessage(`{}`)) {
		t.Error("design.bindcraft must require confirmation")
	}
}

func TestBindCraftExecuteRejectsBadInput(t *testing.T) {
	tool := NewBindCraftTool(t.TempDir(), nil, nil, nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected a validation error when required fields are missing")
	}
}
