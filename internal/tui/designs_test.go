package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/alvarogonjim/fova/internal/domain"
)

func TestDesignsPanelRendersTable(t *testing.T) {
	m := newDesignsModel(NewTheme())
	m.setDesigns([]domain.Design{
		{ID: "d_00000001", Scores: map[string]float64{"plddt_mean": 91.3, "ipsae": 0.78, "iptm": 0.89}},
		{ID: "d_00000002", Scores: map[string]float64{"plddt_mean": 84.0, "ipsae": 0.55}},
	})
	out := m.View()
	if !strings.Contains(out, "designs") {
		t.Errorf("panel missing header: %q", out)
	}
	// Header row has an ipSAE column (SPECS §10.2).
	if !strings.Contains(out, "ipSAE") {
		t.Errorf("designs table missing the ipSAE column: %q", out)
	}
	// The count is shown.
	if !strings.Contains(out, "2") {
		t.Errorf("panel should show the design count: %q", out)
	}
	// An ipSAE value is rendered.
	if !strings.Contains(out, "0.78") {
		t.Errorf("panel missing an ipSAE value: %q", out)
	}
}

func TestDesignsPanelEmpty(t *testing.T) {
	m := newDesignsModel(NewTheme())
	out := m.View()
	if !strings.Contains(out, "designs") {
		t.Errorf("empty panel still shows the header: %q", out)
	}
}

// TestDesignsSectionRuleHeader checks the header is a dim label-plus-rule with
// the design count.
func TestDesignsSectionRuleHeader(t *testing.T) {
	m := newDesignsModel(NewTheme())
	m.setDesigns([]domain.Design{{ID: "d_1"}, {ID: "d_2"}, {ID: "d_3"}})
	out := m.View()
	if !strings.Contains(out, "designs · 3") {
		t.Errorf("header should show the label and count: %q", out)
	}
	if !strings.Contains(out, "─") {
		t.Errorf("header should include the rule rune: %q", out)
	}
}

// TestDesignsEmptyStateActionable checks the empty state nudges the user toward
// an action (SPECS §10.7.8).
func TestDesignsEmptyStateActionable(t *testing.T) {
	m := newDesignsModel(NewTheme())
	m.setWidth(80)
	out := m.View()
	if !strings.Contains(out, "no designs yet") {
		t.Errorf("empty state missing the headline: %q", out)
	}
	if !strings.Contains(out, "ask the agent to design binders") {
		t.Errorf("empty state should be actionable: %q", out)
	}
}

// TestDesignsShortlistHighlighting locks in rebrand spec §3.5: a row whose
// ipSAE >= ShortlistIpSAE renders the ID in moss (Primary) and the ipSAE in
// saffron (Accent); below-threshold rows render entirely in sand (Fg).
//
// Tests run without a TTY, so lipgloss strips colour from Render(); we force
// TrueColor on the default renderer for the duration of this test so ANSI
// codes survive and the moss / saffron / sand probes can be substring-matched.
func TestDesignsShortlistHighlighting(t *testing.T) {
	prev := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	th := NewTheme()
	m := newDesignsModel(th)
	m.setWidth(60)
	m.setDesigns([]domain.Design{
		{ID: "d_top0001", Scores: map[string]float64{"plddt_mean": 91.3, "ipsae": 0.84, "iptm": 0.89}},
		{ID: "d_mid0002", Scores: map[string]float64{"plddt_mean": 84.0, "ipsae": 0.65}},
	})
	out := m.View()

	// The top row's ID should carry the Primary (moss) foreground.
	wantTopID := lipgloss.NewStyle().Foreground(th.Palette.Primary).Render("d_top0001  ")
	if !strings.Contains(out, wantTopID) {
		t.Errorf("shortlist row ID should be moss (Primary). Out:\n%s", out)
	}
	// The top row's ipSAE should carry the Accent (saffron) foreground.
	wantTopIpSAE := lipgloss.NewStyle().Foreground(th.Palette.Accent).Render("  0.84")
	if !strings.Contains(out, wantTopIpSAE) {
		t.Errorf("shortlist row ipSAE should be saffron (Accent). Out:\n%s", out)
	}

	// The below-threshold row must not appear in moss or saffron — it stays
	// fully in sand.
	notWantMidID := lipgloss.NewStyle().Foreground(th.Palette.Primary).Render("d_mid0002")
	if strings.Contains(out, notWantMidID) {
		t.Errorf("below-threshold row ID must not be moss-coloured. Out:\n%s", out)
	}
	notWantMidIpSAE := lipgloss.NewStyle().Foreground(th.Palette.Accent).Render("  0.65")
	if strings.Contains(out, notWantMidIpSAE) {
		t.Errorf("below-threshold row ipSAE must not be saffron-coloured. Out:\n%s", out)
	}
}

// TestIsShortlistedThreshold guards the boundary: 0.70 qualifies, 0.69 does
// not, and a missing ipsae score does not.
func TestIsShortlistedThreshold(t *testing.T) {
	at := domain.Design{Scores: map[string]float64{"ipsae": 0.70}}
	below := domain.Design{Scores: map[string]float64{"ipsae": 0.69}}
	missing := domain.Design{Scores: map[string]float64{"plddt_mean": 90}}

	if !isShortlisted(at) {
		t.Error("ipSAE == 0.70 should qualify (inclusive threshold)")
	}
	if isShortlisted(below) {
		t.Error("ipSAE == 0.69 should not qualify")
	}
	if isShortlisted(missing) {
		t.Error("missing ipSAE should not qualify")
	}
}

// TestRenderSectionRuleAttentionPrefix checks the saffron ▸ prefix shows up
// when attention is set, and is absent otherwise. Forces TrueColor so the
// coloured-glyph substring survives in test mode.
func TestRenderSectionRuleAttentionPrefix(t *testing.T) {
	prev := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	th := NewTheme()
	with := RenderSectionRule(th, "wet-lab", 40, true)
	without := RenderSectionRule(th, "wet-lab", 40, false)

	if !strings.Contains(with, MarkerAttentionGlyph) {
		t.Errorf("attention=true should include the ▸ glyph: %q", with)
	}
	if strings.Contains(without, MarkerAttentionGlyph) {
		t.Errorf("attention=false should omit the ▸ glyph: %q", without)
	}
	// The attention glyph should carry the Accent foreground.
	wantAcc := lipgloss.NewStyle().Foreground(th.Palette.Accent).Render(MarkerAttentionGlyph + " ")
	if !strings.Contains(with, wantAcc) {
		t.Errorf("attention glyph should be saffron-coloured: %q", with)
	}
}
