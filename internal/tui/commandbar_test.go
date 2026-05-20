package tui

import "testing"

func TestParseSlashCommand(t *testing.T) {
	cmd, arg, isSlash := parseSlashCommand("/model claude-opus-4-7")
	if !isSlash || cmd != "model" || arg != "claude-opus-4-7" {
		t.Fatalf("got cmd=%q arg=%q isSlash=%v", cmd, arg, isSlash)
	}
	if _, _, isSlash := parseSlashCommand("fold MAQ"); isSlash {
		t.Fatal("plain text misclassified as slash command")
	}
	cmd, arg, isSlash = parseSlashCommand("/help")
	if !isSlash || cmd != "help" || arg != "" {
		t.Fatalf("got cmd=%q arg=%q isSlash=%v", cmd, arg, isSlash)
	}
}

func TestPickerNavigation(t *testing.T) {
	p := newPicker("Model", []pickerItem{
		{id: "a", label: "A"}, {id: "b", label: "B"}, {id: "c", label: "C"},
	})
	p.next()
	p.next()
	if p.selected().id != "c" {
		t.Errorf("after two next(), selected = %q, want c", p.selected().id)
	}
	p.prev()
	if p.selected().id != "b" {
		t.Errorf("after prev(), selected = %q, want b", p.selected().id)
	}
	// Clamp at edges.
	p.prev()
	p.prev()
	if p.selected().id != "a" {
		t.Errorf("selection should clamp at top, got %q", p.selected().id)
	}
}
