package tui

import (
	"strings"
	"testing"
)

func TestChatAppendAndRender(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendUser("fold MAQ")
	c.appendAgentDelta("Folding ")
	c.appendAgentDelta("now.")
	out := c.renderEntries()
	if !strings.Contains(out, "fold MAQ") {
		t.Errorf("user message missing: %q", out)
	}
	if !strings.Contains(out, "Folding now.") {
		t.Errorf("agent deltas not merged: %q", out)
	}
}

func TestChatToolTrace(t *testing.T) {
	c := newChatModel(NewTheme(), 80, 20)
	c.appendToolStart("fold.esmfold")
	c.appendToolDone("fold.esmfold", "folded d_0001 (pLDDT mean 80.0)")
	out := c.renderEntries()
	if !strings.Contains(out, "fold.esmfold") || !strings.Contains(out, "pLDDT") {
		t.Errorf("tool trace not rendered: %q", out)
	}
}

func TestStatusBarRender(t *testing.T) {
	s := newStatusBarModel(NewTheme())
	s.width = 80
	s.provider = "anthropic"
	s.model = "claude-opus-4-7"
	out := s.View()
	if !strings.Contains(out, "claude-opus-4-7") {
		t.Errorf("status bar missing model: %q", out)
	}
}
