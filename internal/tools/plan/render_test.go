package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/backends/local"
	"github.com/alvarogonjim/fova/internal/domain"
)

// TestRenderPlanLabelledRows asserts every labelled field lands on its own
// line so /plan never collapses into one paragraph (spec Bug 6).
func TestRenderPlanLabelledRows(t *testing.T) {
	approvedAt := time.Date(2026, 5, 18, 9, 30, 0, 0, time.UTC)
	p := domain.DesignPlan{
		ID:             "p_0001",
		ProjectID:      "proj_default",
		Application:    domain.AppBinder,
		Target:         domain.PDBReference{PDBID: "1LYZ", Chain: "A"},
		Method:         "BindCraft",
		FallbackMethod: "RFdiffusion+ProteinMPNN",
		Filters:        domain.FilterConfig{MinIPSAE: 0.5, MinPLDDT: 80},
		ShortlistSize:  50,
		ComputeBackend: "modal",
		EstimatedCost:  15.00,
		EstimatedTime:  "45 minutes",
		Rationale:      "BindCraft excels at de novo binders against well-defined epitopes.",
		Evidence: []domain.EvidenceEntry{
			{CorpusPaperID: "p1", Citation: "Pacesa et al. 2024. BindCraft: AI-driven binder design. 10.1038/s41586-024-x"},
			{CorpusPaperID: "p2", Citation: "Jones et al. 2023. A second relevant paper. 10.1126/science.y"},
		},
		Approved:   true,
		ApprovedAt: &approvedAt,
	}

	out := RenderPlan(p)

	// One labelled row per line; each label is its own line.
	for _, label := range []string{"Target:", "Application:", "Method:", "Filters:", "Shortlist:", "Compute:", "Estimate:", "Rationale:", "Evidence:"} {
		if !strings.Contains(out, label) {
			t.Errorf("missing label %q in:\n%s", label, out)
		}
	}

	// Each labelled row must sit on its own line — count newlines as a sanity floor.
	if got := strings.Count(out, "\n"); got < 8 {
		t.Errorf("expected >=8 newlines, got %d:\n%s", got, out)
	}

	// Specific labels appear at start-of-line (after leading indent).
	for _, line := range []string{"Target:", "Application:", "Method:"} {
		if !strings.Contains(out, "\n  "+line) && !strings.HasPrefix(out, "  "+line) {
			// Each label sits at column 2 (two-space indent).
			if !strings.Contains(out, "  "+line+" ") {
				t.Errorf("label %q not at start of an indented line:\n%s", line, out)
			}
		}
	}

	// Evidence entries each on their own indented line.
	if !strings.Contains(out, "    - ") {
		t.Errorf("evidence entries should be indented list items:\n%s", out)
	}
	if !strings.Contains(out, "BindCraft: AI-driven binder design") {
		t.Errorf("first evidence citation missing:\n%s", out)
	}
	if !strings.Contains(out, "A second relevant paper") {
		t.Errorf("second evidence citation missing:\n%s", out)
	}

	// Approved status renders on the header line.
	if !strings.Contains(out, "approved") {
		t.Errorf("approval status missing from header:\n%s", out)
	}
}

// TestRenderPlanPending: an unapproved plan reports pending approval.
func TestRenderPlanPending(t *testing.T) {
	p := domain.DesignPlan{ID: "p_x", Application: domain.AppBinder, Method: "BindCraft"}
	out := RenderPlan(p)
	if !strings.Contains(out, "pending approval") {
		t.Errorf("expected \"pending approval\" for an unapproved plan:\n%s", out)
	}
}

// TestRenderPlanNoEvidence: an evidence-free plan omits the Evidence block.
func TestRenderPlanNoEvidence(t *testing.T) {
	p := domain.DesignPlan{ID: "p_x", Application: domain.AppBinder, Method: "BindCraft"}
	out := RenderPlan(p)
	if strings.Contains(out, "Evidence:") {
		t.Errorf("Evidence label should be hidden when there are no papers:\n%s", out)
	}
}

// boltzGenTestPlan builds a BoltzGen plan whose MethodConfig points at a spec
// written into workspaceRoot/spec.yaml.
func boltzGenTestPlan(t *testing.T, workspaceRoot string) domain.DesignPlan {
	t.Helper()
	specBody := "version: 1\nentities:\n  - protein:\n      id: A\n      sequence: 80..140\n" +
		"  - protein:\n      id: B\n      sequence: MKT..\nconstraints:\n  total_len:\n    min: 90\n"
	if err := os.WriteFile(filepath.Join(workspaceRoot, "spec.yaml"), []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return domain.DesignPlan{
		ID:          "p_bg",
		Application: domain.AppBinder,
		Method:      "BoltzGen",
		MethodConfig: &domain.MethodConfig{
			SpecPath: "spec.yaml",
			BoltzGen: &domain.BoltzGenParams{
				Protocol:   "protein-anything",
				NumDesigns: 5000,
				Budget:     20,
				Steps:      []string{"design", "folding"},
			},
		},
	}
}

// TestRenderPlanBoltzGenSection: a plan with a MethodConfig renders the
// BoltzGen block — protocol, num designs, budget, the spec absolute path, and
// a preview of the spec file.
func TestRenderPlanBoltzGenSection(t *testing.T) {
	root := t.TempDir()
	p := boltzGenTestPlan(t, root)

	out := RenderPlanWithOpts(p, RenderPlanOpts{WorkspaceRoot: root})

	for _, want := range []string{
		"BoltzGen design specification",
		"protein-anything",
		"5000",                           // num designs
		"20",                             // budget
		"design, folding",                // steps
		filepath.Join(root, "spec.yaml"), // absolute spec path
		"Spec preview:",
		"entities:", // a line from the spec
	} {
		if !strings.Contains(out, want) {
			t.Errorf("BoltzGen section missing %q in:\n%s", want, out)
		}
	}
}

// TestRenderPlanBoltzGenCheckValid: a valid check result renders the tick and
// the visualization path.
func TestRenderPlanBoltzGenCheckValid(t *testing.T) {
	root := t.TempDir()
	p := boltzGenTestPlan(t, root)

	out := RenderPlanWithOpts(p, RenderPlanOpts{
		WorkspaceRoot: root,
		Check:         &BoltzGenCheckResult{Valid: true, VisualizationPath: "out/viz.cif"},
	})
	if !strings.Contains(out, "✓ valid") {
		t.Errorf("a valid check should render a tick:\n%s", out)
	}
	if !strings.Contains(out, "out/viz.cif") {
		t.Errorf("a valid check should render the visualization path:\n%s", out)
	}
}

// TestRenderPlanBoltzGenCheckInvalid: an invalid check result renders the
// errors.
func TestRenderPlanBoltzGenCheckInvalid(t *testing.T) {
	root := t.TempDir()
	p := boltzGenTestPlan(t, root)

	out := RenderPlanWithOpts(p, RenderPlanOpts{
		WorkspaceRoot: root,
		Check:         &BoltzGenCheckResult{Valid: false, Errors: []string{"unknown chain Z"}},
	})
	if !strings.Contains(out, "unknown chain Z") {
		t.Errorf("an invalid check should render its errors:\n%s", out)
	}
	if !strings.Contains(out, "invalid") {
		t.Errorf("an invalid check should be marked invalid:\n%s", out)
	}
}

// TestRenderPlanNoBoltzGenSectionWithoutMethodConfig: a plain plan has no
// BoltzGen block.
func TestRenderPlanNoBoltzGenSectionWithoutMethodConfig(t *testing.T) {
	p := domain.DesignPlan{ID: "p_x", Application: domain.AppBinder, Method: "BindCraft"}
	out := RenderPlan(p)
	if strings.Contains(out, "BoltzGen design specification") {
		t.Errorf("a plan with no MethodConfig must not render the BoltzGen section:\n%s", out)
	}
}

// TestRenderLigandMPNNSection: a plan carrying a LigandMPNN method-config
// renders the LigandMPNN block — the model type and the input PDB.
func TestRenderLigandMPNNSection(t *testing.T) {
	p := domain.DesignPlan{
		ID: "p_x", Method: "LigandMPNN",
		MethodConfig: &domain.MethodConfig{LigandMPNN: &domain.LigandMPNNParams{
			ModelType: "ligand_mpnn", PDB: "bb.pdb", NumDesigns: 8,
		}},
	}
	out := RenderPlan(p)
	for _, want := range []string{"LigandMPNN", "ligand_mpnn", "bb.pdb"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered plan missing %q:\n%s", want, out)
		}
	}
}

// TestRenderRFantibodySection: a plan carrying an RFantibody method-config
// renders the RFantibody block — the framework, target, and hotspots.
func TestRenderRFantibodySection(t *testing.T) {
	p := &domain.DesignPlan{
		ID: "p_x", Method: "RFantibody",
		MethodConfig: &domain.MethodConfig{RFantibody: &domain.RFantibodyParams{
			Framework: "nanobody", Target: "ag.pdb", Hotspots: "T10,T12", NumDesigns: 20,
		}},
	}
	out := RenderPlan(*p)
	for _, want := range []string{"RFantibody", "nanobody", "ag.pdb", "T10,T12"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered plan missing %q:\n%s", want, out)
		}
	}
}

// TestRenderRFdiffusionSection: a plan carrying an RFdiffusion method-config
// renders the RFdiffusion block — the target (or unconditional marker) and
// the contig string.
func TestRenderRFdiffusionSection(t *testing.T) {
	p := domain.DesignPlan{
		ID: "p_x", Method: "RFdiffusion",
		MethodConfig: &domain.MethodConfig{RFdiffusion: &domain.RFdiffusionParams{
			Target: "target.pdb", Hotspots: "A30,A33",
			Contigs: "A1-100/0 60-80", NumDesigns: 8,
		}},
	}
	out := RenderPlan(p)
	for _, want := range []string{"RFdiffusion", "target.pdb", "A30,A33", "A1-100/0 60-80"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered plan missing %q:\n%s", want, out)
		}
	}
	// An unconditional plan (no target) renders the explicit marker.
	pu := domain.DesignPlan{
		ID: "p_u", Method: "RFdiffusion",
		MethodConfig: &domain.MethodConfig{RFdiffusion: &domain.RFdiffusionParams{
			Contigs: "50-100",
		}},
	}
	if !strings.Contains(RenderPlan(pu), "unconditional") {
		t.Error("an RFdiffusion plan with no target must render the unconditional marker")
	}
}

// TestRenderProteinMPNNSection: a plan carrying a ProteinMPNN method-config
// renders the ProteinMPNN block — the input PDB and any set knobs.
func TestRenderProteinMPNNSection(t *testing.T) {
	temp := 0.2
	p := domain.DesignPlan{
		ID: "p_x", Method: "ProteinMPNN",
		MethodConfig: &domain.MethodConfig{ProteinMPNN: &domain.ProteinMPNNParams{
			PDB: "bb.pdb", NumDesigns: 4, SamplingTemp: &temp, ChainsToDesign: "A,B",
		}},
	}
	out := RenderPlan(p)
	for _, want := range []string{"ProteinMPNN", "bb.pdb", "0.2", "A,B"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered plan missing %q:\n%s", want, out)
		}
	}
}

// TestRenderBindCraftSection: a plan carrying a BindCraft method-config
// renders the BindCraft block — the target file + chains + epitope + length.
func TestRenderBindCraftSection(t *testing.T) {
	p := domain.DesignPlan{
		ID: "p_x", Method: "BindCraft",
		MethodConfig: &domain.MethodConfig{BindCraft: &domain.BindCraftParams{
			StartingPDB: "t.pdb", Chains: "A",
			TargetHotspotResidues: "A30,A33",
			LengthMin:             80, LengthMax: 120, NumberOfFinalDesigns: 10,
			ProtocolName: "beta_only",
		}},
	}
	out := RenderPlan(p)
	for _, want := range []string{"BindCraft", "t.pdb", "A30,A33", "80..120", "beta_only"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered plan missing %q:\n%s", want, out)
		}
	}
}

// TestRenderDoctorLabelledRows asserts /doctor output is one row per tool with
// aligned columns and at least the System + Local protein tools sections
// (spec Bug 7).
func TestRenderDoctorLabelledRows(t *testing.T) {
	rep := local.Report{
		UVFound: true,
		UVPath:  "/home/gonjim/.local/bin/uv",
		Tools: []local.ToolLine{
			{Name: "ipsae", Installed: true, Version: "1.0.0"},
			{Name: "proteinmpnn", Installed: true, Version: "1.0.1"},
			{Name: "bindcraft", Installed: false},
			{Name: "boltz2", Installed: false},
			{Name: "chai1", Installed: false},
		},
	}

	out := RenderDoctor(rep)

	// Section headers each on their own line.
	if !strings.Contains(out, "System\n") {
		t.Errorf("System header missing or not on its own line:\n%s", out)
	}
	if !strings.Contains(out, "Local protein tools\n") {
		t.Errorf("Local protein tools header missing or not on its own line:\n%s", out)
	}

	// Each tool sits on its own indented line; ipsae shows ok + version.
	if !strings.Contains(out, "ipsae") {
		t.Errorf("ipsae line missing:\n%s", out)
	}
	if !strings.Contains(out, "1.0.0") {
		t.Errorf("ipsae version missing:\n%s", out)
	}

	// Uninstalled tools show the install hint.
	if !strings.Contains(out, "/install bindcraft") {
		t.Errorf("missing install hint for bindcraft:\n%s", out)
	}

	// At least 3 newlines between "System" and the last tool line — guards
	// the "one labelled row per line" contract from spec AC §5.
	systemIdx := strings.Index(out, "System")
	lastToolIdx := strings.LastIndex(out, "chai1")
	if systemIdx == -1 || lastToolIdx == -1 || lastToolIdx <= systemIdx {
		t.Fatalf("could not locate System and chai1 in output:\n%s", out)
	}
	between := out[systemIdx:lastToolIdx]
	if n := strings.Count(between, "\n"); n < 3 {
		t.Errorf("expected >=3 newlines between System and chai1, got %d:\n%s", n, out)
	}

	// Status markers ("ok", "──") align in a column — both prefixed by two
	// spaces and followed by two spaces before the tool name.
	if !strings.Contains(out, "  ok  ipsae") {
		t.Errorf("ipsae row should align as \"  ok  ipsae\":\n%s", out)
	}
	if !strings.Contains(out, "  ──  bindcraft") {
		t.Errorf("bindcraft row should align as \"  ──  bindcraft\":\n%s", out)
	}
}

// TestRenderDoctorMissingUV reports a clear marker when uv isn't installed.
func TestRenderDoctorMissingUV(t *testing.T) {
	rep := local.Report{UVFound: false}
	out := RenderDoctor(rep)
	if !strings.Contains(out, "uv:") {
		t.Errorf("uv line missing:\n%s", out)
	}
	if !strings.Contains(out, "not installed") {
		t.Errorf("uv should be marked as not installed:\n%s", out)
	}
}
