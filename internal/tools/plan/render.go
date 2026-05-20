package plan

import (
	"fmt"
	"strings"

	"github.com/alvarogonjim/fova/internal/backends/local"
	"github.com/alvarogonjim/fova/internal/domain"
)

// labelRow writes a single "  Label:       value\n" row with the label column
// padded to 13 characters (the longest label is "Application:" at 12 chars,
// so 13 gives every non-Application label one extra trailing space — same
// shape as the table emitted by plan.create's pre-v0.7 Display).
func labelRow(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "  %-13s %s\n", label+":", value)
}

// RenderPlan formats a DesignPlan as a labelled multi-line block. Both
// /plan (TUI view handler) and plan.create (tool Result.Display, once wired)
// route through this function so the two surfaces never drift apart.
func RenderPlan(p domain.DesignPlan) string {
	var b strings.Builder

	status := "pending approval"
	if p.Approved {
		status = "approved"
		if p.ApprovedAt != nil {
			status += " (" + p.ApprovedAt.Format("2006-01-02 15:04 MST") + ")"
		}
	}
	fmt.Fprintf(&b, "Design plan %s — %s\n", p.ID, status)

	labelRow(&b, "Target", formatTarget(p.Target))
	labelRow(&b, "Application", string(p.Application))

	method := p.Method
	if p.FallbackMethod != "" {
		method += " (fallback: " + p.FallbackMethod + ")"
	}
	labelRow(&b, "Method", method)
	labelRow(&b, "Filters", formatFilters(p.Filters))
	labelRow(&b, "Shortlist", fmt.Sprintf("%d", p.ShortlistSize))
	labelRow(&b, "Compute", p.ComputeBackend)
	labelRow(&b, "Estimate", fmt.Sprintf("$%.2f USD · %s", p.EstimatedCost, p.EstimatedTime))

	if p.Rationale != "" {
		labelRow(&b, "Rationale", p.Rationale)
	}

	if len(p.Evidence) > 0 {
		b.WriteString("  Evidence:\n")
		for _, ev := range p.Evidence {
			line := "    - " + ev.Citation
			if ev.Excerpt != "" {
				line += " — " + ev.Excerpt
			}
			b.WriteString(line + "\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
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

// RenderDoctor formats a local.Report as a labelled multi-line block. Delegates
// to Report.String() so the Container runtime / System / Local protein tools
// sections stay in sync with the source of truth in internal/backends/local.
func RenderDoctor(r local.Report) string {
	return strings.TrimRight(r.String(), "\n")
}
