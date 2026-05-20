package tui

import (
	"strings"
	"testing"

	"github.com/alvarogonjim/proteus/internal/domain"
)

func TestDesignsPanelRendersTable(t *testing.T) {
	m := newDesignsModel(NewTheme())
	m.setDesigns([]domain.Design{
		{ID: "d_00000001", Scores: map[string]float64{"plddt_mean": 91.3, "ipsae": 0.78, "iptm": 0.89}},
		{ID: "d_00000002", Scores: map[string]float64{"plddt_mean": 84.0, "ipsae": 0.55}},
	})
	out := m.View()
	if !strings.Contains(out, "DESIGNS") {
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
	if !strings.Contains(out, "DESIGNS") {
		t.Errorf("empty panel still shows the header: %q", out)
	}
}
