package tui

import (
	"fmt"
	"strings"

	"github.com/alvarogonjim/proteus/internal/domain"
)

// designsModel renders the DESIGNS panel — a table with the ID / pLDDT / ipSAE
// / ipTM / Lab columns (SPECS §10.2).
type designsModel struct {
	theme   Theme
	designs []domain.Design
	width   int
}

func newDesignsModel(th Theme) designsModel { return designsModel{theme: th, width: 36} }

// setDesigns replaces the panel's designs.
func (m *designsModel) setDesigns(designs []domain.Design) { m.designs = designs }

// setWidth sets the panel's render width.
func (m *designsModel) setWidth(w int) { m.width = w }

// score formats a design's named score, or "—" when it is absent.
func score(d domain.Design, key string) string {
	if v, ok := d.Scores[key]; ok {
		return fmt.Sprintf("%.2f", v)
	}
	return "—"
}

// View renders the designs panel.
func (m designsModel) View() string {
	var b strings.Builder
	b.WriteString(m.theme.StatusBar.Render(
		clipLine(fmt.Sprintf("DESIGNS (%d)", len(m.designs)), m.width)))
	b.WriteString("\n")
	header := fmt.Sprintf("  %-11s %6s %6s %6s %3s", "ID", "pLDDT", "ipSAE", "ipTM", "Lab")
	b.WriteString(m.theme.ToolTrace.Render(clipLine(header, m.width)))
	b.WriteString("\n")
	if len(m.designs) == 0 {
		b.WriteString(m.theme.ToolTrace.Render(clipLine("  no designs yet", m.width)))
		return b.String()
	}
	for _, d := range m.designs {
		// Lab results arrive with the Adaptyv integration (v0.4); show a dash for now.
		line := fmt.Sprintf("  %-11s %6s %6s %6s %3s",
			shortID(string(d.ID)),
			score(d, "plddt_mean"), score(d, "ipsae"), score(d, "iptm"), "—")
		b.WriteString(m.theme.AgentText.Render(clipLine(line, m.width)))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
