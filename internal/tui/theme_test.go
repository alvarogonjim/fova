package tui

import "testing"

func TestThemeStylesExist(t *testing.T) {
	th := NewTheme()
	// Render must not panic and must produce non-empty output.
	if th.StatusBar.Render("x") == "" {
		t.Error("StatusBar style produced empty output")
	}
	if th.UserText.Render("x") == "" {
		t.Error("UserText style produced empty output")
	}
	if th.ToolTrace.Render("x") == "" {
		t.Error("ToolTrace style produced empty output")
	}
}
