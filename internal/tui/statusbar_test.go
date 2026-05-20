package tui

import (
	"strings"
	"testing"
)

// Post-rebrand the status bar is the bottom hint line only — the title role
// moved to RenderHeader (see header_test.go). These tests cover what's left:
// the static hint text, replay-mode suffix, and width clipping.

func TestStatusFooterHint(t *testing.T) {
	s := newStatusBarModel(NewTheme())
	got := s.footerView()
	for _, want := range []string{"type a message", "/keys", "ctrl+x", "$EDITOR"} {
		if !strings.Contains(got, want) {
			t.Errorf("footerView() = %q, want it to contain %q", got, want)
		}
	}
}

func TestStatusFooterReplay(t *testing.T) {
	s := newStatusBarModel(NewTheme())
	s.replay = "replay 12/42"
	got := s.footerView()
	if !strings.Contains(got, "replay 12/42") {
		t.Fatalf("footerView() = %q, want it to contain %q", got, "replay 12/42")
	}
}

func TestStatusFooterNoReplayWhenEmpty(t *testing.T) {
	s := newStatusBarModel(NewTheme())
	if strings.Contains(s.footerView(), "replay") {
		t.Fatalf("footerView() must not mention replay when the field is empty: %q", s.footerView())
	}
}

func TestStatusFooterWidthClip(t *testing.T) {
	s := newStatusBarModel(NewTheme())
	s.width = 12
	got := s.footerView()
	if w := visibleWidth(got); w > s.width {
		t.Fatalf("footerView() visible width = %d, want <= %d (got %q)", w, s.width, got)
	}
}

func TestStatusViewDelegatesToFooter(t *testing.T) {
	// The post-rebrand View() is the footer hint — there is no separate
	// header view on the status bar; that role moved to RenderHeader.
	s := newStatusBarModel(NewTheme())
	if s.View() != s.footerView() {
		t.Errorf("View() = %q, want it to equal footerView() = %q", s.View(), s.footerView())
	}
}

func TestStatusSettersAreNoopsForFooter(t *testing.T) {
	// setProject and setContextPercent are retained for API compatibility
	// (app.go still calls them) but no longer affect the rendered footer —
	// the hint line is static. This test fences the contract.
	a := newStatusBarModel(NewTheme())
	b := newStatusBarModel(NewTheme())
	b.setProject("/home/me/fova/projects/x")
	b.setContextPercent(75)
	if a.footerView() != b.footerView() {
		t.Errorf("footerView() changed after setProject/setContextPercent — those setters should not affect the hint line.\na: %q\nb: %q",
			a.footerView(), b.footerView())
	}
}

// visibleWidth counts runes outside ANSI escape sequences. Retained from the
// v0.4 statusbar test because header_test.go and footer-clip tests both need
// to assert visible widths against rendered (styled) output.
func visibleWidth(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			n++
		}
	}
	return n
}
