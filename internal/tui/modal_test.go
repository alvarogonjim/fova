package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// withTrueColor forces lipgloss to emit ANSI colour codes for the duration of
// a test, so foreground-style substrings survive in non-TTY runs. Without it
// lipgloss strips colour and every styled segment becomes the plain rune,
// making colour assertions vacuous.
func withTrueColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// TestModalBoxUsesAccentBorder locks in rebrand spec §3.7: the modal's
// rounded box is saffron-bordered. The Theme.ModalBox style must hand back a
// border whose foreground equals the palette Accent.
func TestModalBoxUsesAccentBorder(t *testing.T) {
	th := NewTheme()
	wantHex := "#EF9F27" // saffron / Accent.Dark
	// BorderForeground returns the colour we set during NewThemeFromPalette.
	// AdaptiveColor.value picks Light/Dark per terminal; the dark hex is what
	// the rebrand pins for both branches.
	gotFG := th.ModalBox.GetBorderTopForeground()
	if c, ok := gotFG.(lipgloss.AdaptiveColor); ok {
		if c.Dark != wantHex {
			t.Errorf("ModalBox dark border = %q, want %q (Accent / saffron)", c.Dark, wantHex)
		}
	} else {
		t.Fatalf("ModalBox border foreground is not lipgloss.AdaptiveColor (got %T)", gotFG)
	}
}

// TestRenderKeyRowFormat locks in rebrand spec §3.7: `[y] yes  [n] no` with
// saffron keys, sand labels, and a double-space separator.
func TestRenderKeyRowFormat(t *testing.T) {
	withTrueColor(t)
	th := NewTheme()
	out := RenderKeyRow(th,
		KeyRowEntry{Key: "y", Label: "yes"},
		KeyRowEntry{Key: "n", Label: "no"},
	)
	if !strings.Contains(out, "[y]") || !strings.Contains(out, "yes") {
		t.Errorf("key row missing `[y] yes`: %q", out)
	}
	if !strings.Contains(out, "[n]") || !strings.Contains(out, "no") {
		t.Errorf("key row missing `[n] no`: %q", out)
	}

	// The bracketed key must carry the Accent foreground (saffron).
	wantKey := lipgloss.NewStyle().Foreground(th.Palette.Accent).Render("[y]")
	if !strings.Contains(out, wantKey) {
		t.Errorf("`[y]` should be rendered in Accent (saffron); not found in %q", out)
	}
	// The label must carry the Fg foreground (sand).
	wantLabel := lipgloss.NewStyle().Foreground(th.Palette.Fg).Render("yes")
	if !strings.Contains(out, wantLabel) {
		t.Errorf("label `yes` should be rendered in Fg (sand); not found in %q", out)
	}
}

// TestRenderKeyRowFourKeys covers the wet-lab `[y][n][r][s]` row.
func TestRenderKeyRowFourKeys(t *testing.T) {
	th := NewTheme()
	out := RenderKeyRow(th,
		KeyRowEntry{Key: "y", Label: "yes"},
		KeyRowEntry{Key: "n", Label: "no"},
		KeyRowEntry{Key: "r", Label: "review"},
		KeyRowEntry{Key: "s", Label: "save for later"},
	)
	for _, want := range []string{"[y]", "[n]", "[r]", "[s]", "yes", "no", "review", "save for later"} {
		if !strings.Contains(out, want) {
			t.Errorf("key row missing %q: %q", want, out)
		}
	}
}

// TestModalViewRendersKeyRow verifies the y/n confirmation modal routes its
// action row through RenderKeyRow so the keys get the saffron treatment.
func TestModalViewRendersKeyRow(t *testing.T) {
	m := modalModel{prompt: "Continue?"}
	out := m.view(NewTheme(), 80)
	if !strings.Contains(out, "Continue?") {
		t.Errorf("modal missing prompt text: %q", out)
	}
	if !strings.Contains(out, "[y]") || !strings.Contains(out, "[n]") {
		t.Errorf("modal action row should use [y] / [n] bracket format: %q", out)
	}
}

// TestRenderJSONModal_short locks in the spec §3.5 baseline: a small input
// renders the `Run <name>?` header, the four-key editable row, and no
// `(edited)` hint or truncation tail.
func TestRenderJSONModal_short(t *testing.T) {
	th := NewTheme()
	input := json.RawMessage(`{"sequence":"MAQVQL"}`)
	out := renderJSONModal("fold.boltz2", input, false, th, 80, 15)
	for _, want := range []string{
		"Run fold.boltz2?",
		"sequence",
		"MAQVQL",
		"[y]", "[e]", "[n]", "[esc]",
		"accept", "edit", "decline", "cancel turn",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderJSONModal output missing %q in:\n%s", want, out)
		}
	}
	for _, never := range []string{"(edited)", "… [e] to edit"} {
		if strings.Contains(out, never) {
			t.Errorf("renderJSONModal output should not contain %q in:\n%s", never, out)
		}
	}
}

// TestRenderJSONModal_truncated covers the spec §3.5 truncation tail.
// A body that exceeds maxLines rows must show the `[e] to edit` hint.
func TestRenderJSONModal_truncated(t *testing.T) {
	th := NewTheme()
	// Build an object with enough keys to push past five JSON lines once
	// pretty-printed (each `"kN": N,` is one line plus the outer braces).
	parts := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		parts = append(parts, `"k`+string(rune('A'+i))+`":`+string(rune('0'+(i%10))))
	}
	input := json.RawMessage("{" + strings.Join(parts, ",") + "}")
	out := renderJSONModal("design.bindcraft", input, false, th, 80, 5)
	if !strings.Contains(out, "… [e] to edit") {
		t.Errorf("truncated render missing the tail hint:\n%s", out)
	}
}

// TestRenderJSONModal_edited verifies the `(edited)` hint flips on when the
// caller passes edited=true. This is what tells the user "you're about to
// submit your bytes, not the agent's".
func TestRenderJSONModal_edited(t *testing.T) {
	th := NewTheme()
	out := renderJSONModal("fold.chai1", json.RawMessage(`{"a":1}`), true, th, 80, 15)
	if !strings.Contains(out, "(edited)") {
		t.Errorf("edited=true render missing `(edited)` hint:\n%s", out)
	}
	if !strings.Contains(out, "Run fold.chai1?") {
		t.Errorf("edited render missing the header:\n%s", out)
	}
}
