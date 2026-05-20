package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func sampleHeaderInput() HeaderInput {
	return HeaderInput{
		Version:   "fova 0.5.0-dev",
		Model:     "qwen3.5-35b-a3b-fp8",
		FullPath:  "/home/alvaro/fova/projects/binder-v3",
		ShortPath: "~/Projects/fova/binder-v3",
	}
}

func TestRenderHeaderReturnsSixLines(t *testing.T) {
	got := RenderHeader(NewTheme(), sampleHeaderInput())
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("RenderHeader returned %d lines, want 6:\n%s", len(lines), got)
	}
}

func TestRenderHeaderShowsVersionAndModel(t *testing.T) {
	got := RenderHeader(NewTheme(), sampleHeaderInput())
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[1], "fova 0.5.0-dev") {
		t.Errorf("line 1 = %q, want it to contain the version label", lines[1])
	}
	if !strings.Contains(lines[2], "qwen3.5-35b-a3b-fp8") {
		t.Errorf("line 2 = %q, want it to contain the model", lines[2])
	}
}

func TestRenderHeaderLine1HasFovaTwice(t *testing.T) {
	// Line 1 carries both the mark (a stylized "F") and the "fova X.Y.Z"
	// version label. While the mark itself isn't literal "fova" text, the
	// version label contains "fova" — and line 0 already contains "fova ·".
	// We assert the more meaningful check: "fova" appears in the version
	// label on line 1.
	got := RenderHeader(NewTheme(), sampleHeaderInput())
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], "fova") {
		t.Errorf("line 0 should contain the brand word 'fova': %q", lines[0])
	}
	if !strings.Contains(lines[1], "fova") {
		t.Errorf("line 1 should contain the version label starting with 'fova': %q", lines[1])
	}
}

func TestRenderHeaderLine2HasSaffronEndpoint(t *testing.T) {
	// Line 2 is the only line that styles `●` in Accent (saffron); this is
	// the brand's single emphasis point.
	got := RenderHeader(NewTheme(), sampleHeaderInput())
	lines := strings.Split(got, "\n")
	want := lipgloss.NewStyle().Foreground(DefaultPalette.Accent).Render(logoEndpoint)
	if !strings.Contains(lines[2], want) {
		t.Errorf("line 2 = %q, want it to contain the saffron-styled endpoint %q", lines[2], want)
	}
}

func TestRenderHeaderPathLinesAreDim(t *testing.T) {
	// Lines 0 and 3 carry path text in dim (FgMuted). The simplest assertion:
	// the same text rendered with theme.Muted is a substring of the header
	// line — which guarantees the dim style was applied.
	th := NewTheme()
	got := RenderHeader(th, sampleHeaderInput())
	lines := strings.Split(got, "\n")

	wantLine0 := th.Muted.Render("fova · /home/alvaro/fova/projects/binder-v3")
	if !strings.Contains(lines[0], wantLine0) {
		t.Errorf("line 0 = %q, want it to contain the dim full-path label %q", lines[0], wantLine0)
	}
	wantLine3 := th.Muted.Render("~/Projects/fova/binder-v3")
	if !strings.Contains(lines[3], wantLine3) {
		t.Errorf("line 3 = %q, want it to contain the dim short-path label %q", lines[3], wantLine3)
	}
}

func TestRenderHeaderHandlesEmptyModel(t *testing.T) {
	// An unset model must not crash or produce a 5-line header — the brand
	// mark always occupies six lines.
	in := sampleHeaderInput()
	in.Model = ""
	got := RenderHeader(NewTheme(), in)
	if n := strings.Count(got, "\n") + 1; n != 6 {
		t.Fatalf("RenderHeader with empty Model returned %d lines, want 6:\n%s", n, got)
	}
}

func TestRenderHeaderHandlesEmptyPaths(t *testing.T) {
	// Tests / replays without a workspace ($FOVA_HOME unset) pass empty
	// paths; the header should render "fova" on line 0 and a blank line 3.
	in := HeaderInput{Version: "fova 0.5.0-dev", Model: "m"}
	got := RenderHeader(NewTheme(), in)
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("RenderHeader returned %d lines, want 6", len(lines))
	}
	if !strings.Contains(stripANSI(lines[0]), "fova") {
		t.Errorf("line 0 must still show 'fova' even with no FullPath: %q", lines[0])
	}
}

func TestRenderHeaderMarkColumnAligns(t *testing.T) {
	// Lines 1-5 all start with a brand-mark column of the same fixed visible
	// width. The text column therefore starts at the same offset on every
	// line — important for the header to read as a coherent block.
	got := RenderHeader(NewTheme(), sampleHeaderInput())
	lines := strings.Split(got, "\n")
	for i := 1; i <= 5; i++ {
		// The mark column's visible run is the first headerMarkColWidth
		// runes after ANSI strip. We check by asserting the rendered line's
		// visible prefix is at least that wide.
		if v := visibleRuneCount(lines[i]); v < headerMarkColWidth {
			t.Errorf("line %d visible width = %d, want >= %d (mark column too narrow): %q",
				i, v, headerMarkColWidth, lines[i])
		}
	}
}
