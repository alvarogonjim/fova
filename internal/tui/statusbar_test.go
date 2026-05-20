package tui

import (
	"strings"
	"testing"
)

func TestStatusHeaderView(t *testing.T) {
	s := newStatusBarModel(NewTheme())

	if got := s.headerView(); !strings.Contains(got, "proteus") {
		t.Fatalf("headerView() = %q, want it to contain %q", got, "proteus")
	}

	s.setProject("binder-v3")
	if got := s.headerView(); !strings.Contains(got, "binder-v3") {
		t.Fatalf("headerView() after setProject = %q, want it to contain %q", got, "binder-v3")
	}
}

func TestStatusFooterView(t *testing.T) {
	s := newStatusBarModel(NewTheme())
	s.model = "claude-opus"
	s.cost = 1.5

	got := s.footerView()
	for _, want := range []string{"claude-opus", "$", "% context", "/"} {
		if !strings.Contains(got, want) {
			t.Fatalf("footerView() = %q, want it to contain %q", got, want)
		}
	}
}

func TestStatusFooterContextWarning(t *testing.T) {
	low := newStatusBarModel(NewTheme())
	low.model = "claude-opus"
	low.setContextPercent(10)

	high := newStatusBarModel(NewTheme())
	high.model = "claude-opus"
	high.setContextPercent(90)

	if low.footerView() == high.footerView() {
		t.Fatalf("footerView() at 90%% context should differ from 10%% (warning styling), got identical output")
	}
}

func TestStatusFooterWidthClip(t *testing.T) {
	s := newStatusBarModel(NewTheme())
	s.model = "claude-opus"
	s.cost = 3.25
	s.setContextPercent(42)
	s.width = 12

	got := s.footerView()
	if w := visibleWidth(got); w > s.width {
		t.Fatalf("footerView() visible width = %d, want <= %d (got %q)", w, s.width, got)
	}
}

// visibleWidth counts runes outside ANSI escape sequences.
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
