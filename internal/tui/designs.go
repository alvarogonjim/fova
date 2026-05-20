package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/alvarogonjim/fova/internal/domain"
)

// ShortlistIpSAE is the ipSAE threshold above which a design is considered a
// wet-lab shortlist candidate (rebrand spec §3.5). Rows at or above this score
// render with moss ID + saffron ipSAE; rows below render fully in sand.
const ShortlistIpSAE = 0.70

// designsModel renders the DESIGNS panel — a table with the ID / pLDDT / ipSAE
// / ipTM / Lab columns (SPECS §10.2). Top-N rows (those with ipSAE >=
// ShortlistIpSAE) are highlighted per rebrand spec §3.5.
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

// isShortlisted reports whether a design clears the wet-lab shortlist
// threshold (rebrand spec §3.5): ipSAE present and >= ShortlistIpSAE.
func isShortlisted(d domain.Design) bool {
	v, ok := d.Scores["ipsae"]
	return ok && v >= ShortlistIpSAE
}

// RenderSectionRule renders a panel header in the rebrand pattern
// `<name> ─────────` (spec §3.3). The label and rule render in FgMuted (dim);
// when attention is true a saffron `▸ ` prefix is prepended so the panel
// signals it needs the user.
func RenderSectionRule(theme Theme, label string, width int, attention bool) string {
	label = strings.ToLower(label)
	prefix := ""
	prefixWidth := 0
	if attention {
		prefix = lipgloss.NewStyle().Foreground(theme.Palette.Accent).Render(MarkerAttentionGlyph + " ")
		prefixWidth = 2 // glyph + space
	}
	line := label + " "
	if pad := width - prefixWidth - len([]rune(line)); pad > 0 {
		line += strings.Repeat("─", pad)
	}
	muted := lipgloss.NewStyle().Foreground(theme.Palette.FgMuted)
	return prefix + muted.Render(clipLine(line, width-prefixWidth))
}

// View renders the designs panel. The header uses RenderSectionRule (rebrand
// §3.3); rows route through one of three style branches based on whether the
// design is in the wet-lab shortlist (spec §3.5).
func (m designsModel) View() string {
	var b strings.Builder
	b.WriteString(RenderSectionRule(m.theme,
		fmt.Sprintf("designs · %d", len(m.designs)), m.width, false))
	b.WriteString("\n")
	header := fmt.Sprintf("  %-11s %6s %6s %6s %3s", "ID", "pLDDT", "ipSAE", "ipTM", "Lab")
	b.WriteString(m.theme.ToolTrace.Render(clipLine(header, m.width)))
	b.WriteString("\n")
	if len(m.designs) == 0 {
		b.WriteString(m.theme.Subtle.Render(wrapText(
			"no designs yet · ask the agent to design binders", m.width)))
		return b.String()
	}
	mossStyle := lipgloss.NewStyle().Foreground(m.theme.Palette.Primary)
	saffronStyle := lipgloss.NewStyle().Foreground(m.theme.Palette.Accent)
	sandStyle := lipgloss.NewStyle().Foreground(m.theme.Palette.Fg)
	for _, d := range m.designs {
		// Lab results arrive with the Adaptyv integration (v0.4); show a dash for now.
		id := shortID(string(d.ID))
		plddt := score(d, "plddt_mean")
		ipsae := score(d, "ipsae")
		iptm := score(d, "iptm")
		lab := "—"

		if isShortlisted(d) {
			// Spec §3.5: ID in moss (Primary), ipSAE in saffron (Accent),
			// other columns in sand (Fg). Padding is applied to the styled
			// segments individually so column widths still line up.
			line := "  " +
				mossStyle.Render(fmt.Sprintf("%-11s", id)) + " " +
				sandStyle.Render(fmt.Sprintf("%6s", plddt)) + " " +
				saffronStyle.Render(fmt.Sprintf("%6s", ipsae)) + " " +
				sandStyle.Render(fmt.Sprintf("%6s", iptm)) + " " +
				sandStyle.Render(fmt.Sprintf("%3s", lab))
			b.WriteString(line)
		} else {
			line := fmt.Sprintf("  %-11s %6s %6s %6s %3s", id, plddt, ipsae, iptm, lab)
			b.WriteString(m.theme.AgentText.Render(clipLine(line, m.width)))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
