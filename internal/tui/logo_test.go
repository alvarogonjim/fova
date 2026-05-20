package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestLogoLinesGlyphs(t *testing.T) {
	// The unrendered constant is the source of truth for the brand mark;
	// drift here would silently change every consumer (header, /about, …).
	want := [5]string{
		" ┌─╮",
		" │ ╰──●",
		" ├─╮",
		" │ ╰─",
		" │",
	}
	if LogoLines != want {
		t.Fatalf("LogoLines = %#v, want %#v", LogoLines, want)
	}
}

func TestRenderLogoReturnsFiveLines(t *testing.T) {
	got := RenderLogo(DefaultPalette)
	if len(got) != 5 {
		t.Fatalf("RenderLogo returned %d lines, want 5", len(got))
	}
}

func TestRenderLogoSaffronEndpoint(t *testing.T) {
	// Line 1 must contain the saffron-styled `●` so the brand identity reads
	// correctly: moss strokes with a single accent endpoint.
	got := RenderLogo(DefaultPalette)
	wantAccent := lipgloss.NewStyle().Foreground(DefaultPalette.Accent).Render(logoEndpoint)
	if !strings.Contains(got[1], wantAccent) {
		t.Fatalf("RenderLogo line 1 = %q, want it to contain the saffron-styled %q (%q)",
			got[1], logoEndpoint, wantAccent)
	}
}

func TestRenderLogoMossStrokes(t *testing.T) {
	// Lines without the endpoint render entirely in moss. We check by
	// stripping ANSI and comparing against the unrendered constant.
	got := RenderLogo(DefaultPalette)
	for i, line := range got {
		if i == 1 {
			continue // already covered above; mixed-style line
		}
		if stripped := stripANSI(line); stripped != LogoLines[i] {
			t.Errorf("RenderLogo line %d stripped = %q, want %q", i, stripped, LogoLines[i])
		}
	}
}

// stripANSI is shared with lab_test.go (same package). Both tests rely on it
// to compare styled output against a plain reference.
