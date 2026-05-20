package tui

import (
	"fmt"
	"strings"

	"github.com/alvarogonjim/proteus/internal/domain"
)

// renderPlan formats a design plan as a readable multi-line checklist block.
func renderPlan(p domain.DesignPlan) string {
	var b strings.Builder

	status := "pending approval"
	if p.Approved {
		status = "approved"
		if p.ApprovedAt != nil {
			status += " (" + p.ApprovedAt.Format("2006-01-02 15:04 MST") + ")"
		}
	}
	fmt.Fprintf(&b, "Design plan %s — %s\n", p.ID, status)

	fmt.Fprintf(&b, "  Target:        %s\n", formatTarget(p.Target))
	fmt.Fprintf(&b, "  Application:   %s\n", p.Application)

	method := p.Method
	if p.FallbackMethod != "" {
		method += " (fallback: " + p.FallbackMethod + ")"
	}
	fmt.Fprintf(&b, "  Method:        %s\n", method)

	fmt.Fprintf(&b, "  Filters:       %s\n", formatFilters(p.Filters))
	fmt.Fprintf(&b, "  Shortlist:     %d\n", p.ShortlistSize)
	fmt.Fprintf(&b, "  Compute:       %s\n", p.ComputeBackend)
	fmt.Fprintf(&b, "  Estimate:      $%.2f USD · %s\n", p.EstimatedCost, p.EstimatedTime)

	if p.Rationale != "" {
		fmt.Fprintf(&b, "  Rationale:     %s\n", p.Rationale)
	}

	if len(p.EvidencePapers) > 0 {
		b.WriteString("  Evidence:\n")
		for _, paper := range p.EvidencePapers {
			line := fmt.Sprintf("    - %s (%d)", paper.Title, paper.Year)
			if paper.DOI != "" {
				line += " " + paper.DOI
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\nUse /plan approve to lock it in, or /plan cancel to discard it.")
	return b.String()
}

// formatTarget renders a PDB reference in a single line.
func formatTarget(t domain.PDBReference) string {
	ref := t.PDBID
	if ref == "" {
		ref = t.FilePath
	}
	if ref == "" {
		ref = "(unspecified)"
	}
	if t.Chain != "" {
		ref += " chain " + t.Chain
	}
	return ref
}

// formatFilters lists the non-zero filter thresholds.
func formatFilters(f domain.FilterConfig) string {
	var parts []string
	add := func(name string, v float64) {
		if v != 0 {
			parts = append(parts, fmt.Sprintf("%s %g", name, v))
		}
	}
	add("MinIPSAE", f.MinIPSAE)
	add("MinPLDDT", f.MinPLDDT)
	add("MinPLDDTMin", f.MinPLDDTMin)
	add("MaxPAEInterface", f.MaxPAEInterface)
	add("MinIPTM", f.MinIPTM)
	add("MinPDockQ", f.MinPDockQ)
	add("MaxRMSDtoModel", f.MaxRMSDtoModel)
	add("MaxMotifRMSD", f.MaxMotifRMSD)
	add("MinRosettaScore", f.MinRosettaScore)
	add("MaxESMPerplexity", f.MaxESMPerplexity)
	if len(parts) == 0 {
		return "(none set)"
	}
	return strings.Join(parts, ", ")
}

// renderNoPlan is shown when no design plan exists yet.
func renderNoPlan() string {
	return "No design plan yet.\n" +
		"Ask the agent to plan from a target, e.g. " +
		"\"design VHH binders against SARS-CoV-2 spike RBD\"."
}
