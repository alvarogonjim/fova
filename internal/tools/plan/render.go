package plan

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alvarogonjim/fova/internal/backends/local"
	"github.com/alvarogonjim/fova/internal/domain"
)

// boltzGenSpecPreviewLines caps how many lines of the spec YAML the /plan
// view shows — enough to recognise the design without flooding the chat.
const boltzGenSpecPreviewLines = 15

// labelRow writes a single "  Label:       value\n" row with the label column
// padded to 13 characters (the longest label is "Application:" at 12 chars,
// so 13 gives every non-Application label one extra trailing space — same
// shape as the table emitted by plan.create's pre-v0.7 Display).
func labelRow(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "  %-13s %s\n", label+":", value)
}

// RenderPlanOpts carries the context the BoltzGen section needs that is not
// on the plan itself: the workspace root (to resolve the spec's absolute
// path) and the most recent design.boltzgen_check result, if one is
// available. A zero RenderPlanOpts renders the plan without the BoltzGen
// extras — RenderPlan uses exactly that.
type RenderPlanOpts struct {
	// WorkspaceRoot is the absolute path of the active project workspace. The
	// spec path stored on the plan is workspace-relative; joining it with the
	// root gives the absolute path the user opens in their editor. Empty falls
	// back to showing the workspace-relative path.
	WorkspaceRoot string
	// Check is the latest design.boltzgen_check result for the plan's spec.
	// Nil means no check has been run for this render (the section then omits
	// the result line).
	Check *BoltzGenCheckResult
}

// RenderPlan formats a DesignPlan as a labelled multi-line block. Both
// /plan (TUI view handler) and plan.create (tool Result.Display, once wired)
// route through this function so the two surfaces never drift apart. A
// BoltzGen plan is rendered without the spec preview / check section — call
// RenderPlanWithOpts to include those.
func RenderPlan(p domain.DesignPlan) string {
	return RenderPlanWithOpts(p, RenderPlanOpts{})
}

// RenderPlanWithOpts is RenderPlan plus the BoltzGen method-config section
// (spec path + preview + check result). The /plan TUI view supplies the
// workspace root and a freshly-run check result via opts.
func RenderPlanWithOpts(p domain.DesignPlan, opts RenderPlanOpts) string {
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

	if mc := p.MethodConfig; mc != nil {
		switch {
		case mc.BoltzGen != nil || mc.SpecPath != "":
			renderBoltzGenSection(&b, mc, opts)
		case mc.LigandMPNN != nil:
			renderLigandMPNNSection(&b, mc.LigandMPNN)
		case mc.RFantibody != nil:
			renderRFantibodySection(&b, mc.RFantibody)
		case mc.RFdiffusion2 != nil:
			renderRFdiffusion2Section(&b, mc.RFdiffusion2)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderBoltzGenSection appends the BoltzGen method-config block to b: the run
// params, the spec's absolute path, a short spec preview, and the most recent
// design.boltzgen_check result. It is emitted for any plan that carries a
// MethodConfig (currently BoltzGen-only).
func renderBoltzGenSection(b *strings.Builder, mc *domain.MethodConfig, opts RenderPlanOpts) {
	b.WriteString("\n  BoltzGen design specification\n")

	if bg := mc.BoltzGen; bg != nil {
		protocol := bg.Protocol
		if protocol == "" {
			protocol = "protein-anything (default)"
		}
		labelRow(b, "Protocol", protocol)
		labelRow(b, "Num designs", fmt.Sprintf("%d", bg.NumDesigns))
		labelRow(b, "Budget", fmt.Sprintf("%d", bg.Budget))
		if len(bg.Steps) > 0 {
			labelRow(b, "Steps", strings.Join(bg.Steps, ", "))
		}
	}

	specAbs := boltzGenSpecAbsPath(opts.WorkspaceRoot, mc.SpecPath)
	labelRow(b, "Spec file", specAbs)

	preview, perr := boltzGenSpecPreview(specAbs)
	switch {
	case perr != nil:
		labelRow(b, "Spec preview", "(could not read: "+perr.Error()+")")
	case preview != "":
		b.WriteString("  Spec preview:\n")
		for _, line := range strings.Split(preview, "\n") {
			b.WriteString("    " + line + "\n")
		}
	}

	if opts.Check != nil {
		b.WriteString("  Check: " + formatBoltzGenCheck(*opts.Check) + "\n")
	}
}

// renderLigandMPNNSection appends the LigandMPNN method-config block to b: the
// model type, the input backbone PDB, and the run parameters that are set. It
// is emitted for a plan whose MethodConfig carries LigandMPNN params. Unlike
// the BoltzGen section there is no spec file or check result to fold in.
func renderLigandMPNNSection(b *strings.Builder, lm *domain.LigandMPNNParams) {
	b.WriteString("\n  LigandMPNN design configuration\n")

	modelType := lm.ModelType
	if modelType == "" {
		modelType = "ligand_mpnn (default)"
	}
	labelRow(b, "Model type", modelType)
	labelRow(b, "Input PDB", lm.PDB)
	labelRow(b, "Num designs", fmt.Sprintf("%d", lm.NumDesigns))

	if lm.Temperature != nil {
		labelRow(b, "Temperature", fmt.Sprintf("%g", *lm.Temperature))
	}
	if lm.RedesignedResidues != "" {
		labelRow(b, "Redesigned", lm.RedesignedResidues)
	}
	if lm.FixedResidues != "" {
		labelRow(b, "Fixed", lm.FixedResidues)
	}
	if lm.PackSideChains != nil {
		packing := "off"
		if *lm.PackSideChains {
			packing = "on"
		}
		labelRow(b, "Side chains", packing)
	}
}

// renderRFantibodySection appends the RFantibody method-config block to b: the
// framework choice, the target antigen, the epitope hotspots, the backbone
// count, and the per-CDR design-loop spec when one is set. It is emitted for a
// plan whose MethodConfig carries RFantibody params. Like the LigandMPNN
// section there is no spec file or check result to fold in.
func renderRFantibodySection(b *strings.Builder, ra *domain.RFantibodyParams) {
	b.WriteString("\n  RFantibody design configuration\n")

	framework := ra.Framework
	switch {
	case ra.FrameworkPDB != "":
		framework = ra.FrameworkPDB
	case framework == "":
		framework = "nanobody (default)"
	}
	labelRow(b, "Framework", framework)
	labelRow(b, "Target", ra.Target)
	labelRow(b, "Hotspots", ra.Hotspots)
	labelRow(b, "Num designs", fmt.Sprintf("%d", ra.NumDesigns))

	if ra.DesignLoops != "" {
		labelRow(b, "Design loops", ra.DesignLoops)
	}
}

// renderRFdiffusion2Section appends the RFdiffusion2 method-config block to b:
// the benchmark choice, the motif PDB and contigs when a user motif is set,
// the design count, and the stop step (with the "end (default)" fallback when
// unset, so the user sees what will actually happen). It is emitted for a
// plan whose MethodConfig carries RFdiffusion2 params.
func renderRFdiffusion2Section(b *strings.Builder, rd *domain.RFdiffusion2Params) {
	b.WriteString("\n  RFdiffusion2 design configuration\n")

	benchmark := rd.Benchmark
	if benchmark == "" {
		benchmark = "active_site_demo (default)"
	}
	labelRow(b, "Benchmark", benchmark)

	if rd.MotifPDB != "" {
		labelRow(b, "Motif PDB", rd.MotifPDB)
		labelRow(b, "Contigs", rd.Contigs)
	}

	labelRow(b, "Num designs", fmt.Sprintf("%d", rd.NumDesigns))

	stop := rd.StopStep
	if stop == "" {
		stop = "end (default)"
	}
	labelRow(b, "Stop step", stop)
}

// boltzGenSpecAbsPath joins the workspace-relative spec path with the
// workspace root so the user sees the absolute path their editor opens. An
// empty root (the plan.create Display path, where the root is not known)
// falls back to the relative path.
func boltzGenSpecAbsPath(root, rel string) string {
	if root == "" || rel == "" {
		return rel
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(root, rel)
}

// boltzGenSpecPreview reads up to boltzGenSpecPreviewLines lines of the spec
// file at path. A missing file returns ("", nil) so a plan whose spec has not
// landed yet still renders — only a genuine read error is surfaced.
func boltzGenSpecPreview(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() && len(lines) < boltzGenSpecPreviewLines {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

// formatBoltzGenCheck renders a design.boltzgen_check result as a single line:
// a tick + the visualization path when valid, or the joined errors when not.
func formatBoltzGenCheck(c BoltzGenCheckResult) string {
	if c.Valid {
		s := "✓ valid"
		if c.VisualizationPath != "" {
			s += " — visualization: " + c.VisualizationPath
		}
		return s
	}
	errs := strings.Join(c.Errors, "; ")
	if errs == "" {
		errs = "(no detail reported)"
	}
	return "✗ invalid — " + errs
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
