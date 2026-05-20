package tui

import (
	"strings"
	"testing"
)

func TestWrapTextShortReturnsUnchanged(t *testing.T) {
	if got := wrapText("hello world", 40); got != "hello world" {
		t.Errorf("wrapText short = %q, want unchanged", got)
	}
}

func TestWrapTextZeroWidthReturnsUnchanged(t *testing.T) {
	if got := wrapText("hello world", 0); got != "hello world" {
		t.Errorf("wrapText w=0 = %q, want unchanged", got)
	}
}

func TestWrapTextWrapsAtSpaces(t *testing.T) {
	in := "no experiments yet · ask the agent to submit designs to Adaptyv"
	got := wrapText(in, 36)
	for _, line := range strings.Split(got, "\n") {
		if n := len([]rune(line)); n > 36 {
			t.Errorf("line %q has %d runes, want <= 36", line, n)
		}
	}
	// At least two lines for a 64-char input wrapped to 36.
	if strings.Count(got, "\n") < 1 {
		t.Errorf("wrapText produced one line for a long input: %q", got)
	}
}

func TestWrapTextHardBreaksOversizedWord(t *testing.T) {
	in := "a verylongwordthatcannotfit"
	got := wrapText(in, 10)
	for _, line := range strings.Split(got, "\n") {
		if n := len([]rune(line)); n > 10 {
			t.Errorf("line %q has %d runes, want <= 10", line, n)
		}
	}
}

func TestWrapTextEmptyInput(t *testing.T) {
	if got := wrapText("", 20); got != "" {
		t.Errorf("wrapText \"\" = %q, want \"\"", got)
	}
}
