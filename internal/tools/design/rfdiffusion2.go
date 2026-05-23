package design

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// (The "io" import is added in A2 when Execute's job-Run callback needs io.Writer.)

// RFdiffusion2Params is the agent-facing RFdiffusion2 run configuration. It is
// an alias of domain.RFdiffusion2Params — the type lives in internal/domain so
// a DesignPlan can carry it without an import cycle, and design tools
// reference it here under the friendlier package-local name.
type RFdiffusion2Params = domain.RFdiffusion2Params

// rfdiffusion2Benchmarks is the closed set of bundled active-site sweeps,
// advertised as the benchmark enum.
var rfdiffusion2Benchmarks = []string{"active_site_demo", "enzyme_bench_n41"}

// rfdiffusion2StopSteps is the closed set of pipeline stop points, advertised
// as the stop_step enum.
var rfdiffusion2StopSteps = []string{"design", "end"}

// rfdiffusion2Tool is the bespoke design.rfdiffusion2 tool. Unlike the shared
// designTool wrapper, it advertises RFdiffusion2's Hydra-driven pipeline
// surface — the benchmark choice (or a user motif PDB + contig string
// override), the inference toggles, and the stop-step switch.
type rfdiffusion2Tool struct {
	mgr           *jobs.Manager
	backend       backends.Backend
	store         *store.Store
	workspaceRoot string
}

// NewRFdiffusion2Tool builds the design.rfdiffusion2 tool — atom-level enzyme
// active-site scaffolding with RFdiffusion2. workspaceRoot scopes the relative
// path inputs (motif_pdb).
//
// The signature is held stable so cmd/fova/main.go's registration line is
// unchanged across the bespoke-tool rework.
func NewRFdiffusion2Tool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *rfdiffusion2Tool {
	return &rfdiffusion2Tool{
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}

func (*rfdiffusion2Tool) Name() string { return "design.rfdiffusion2" }

func (*rfdiffusion2Tool) Description() string {
	return "Scaffold enzyme backbones around a catalytic motif with " +
		"RFdiffusion2 — atom-level flow-matching active-site scaffolding " +
		"that runs the full Hydra-driven pipeline (backbone diffusion → " +
		"idealization → inline LigandMPNN sequence design → inline Chai-1 " +
		"fold → metrics). Runs as an async GPU job. Supports the bundled " +
		"benchmark presets (active_site_demo, enzyme_bench_n41), a user " +
		"catalytic motif PDB + contig string override, and the documented " +
		"inference toggles."
}

// InputSchema advertises every RFdiffusion2Params field, with the benchmark
// and stop_step enums and minimums on the bounded numerics. No required keys:
// every field has a default or is a conditional override; Validate enforces
// the conditional shape (motif_pdb ⇒ contigs).
func (*rfdiffusion2Tool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"benchmark": map[string]any{
				"type":        "string",
				"description": "Bundled in-image active-site sweep — active_site_demo (the open-source active-site demo, default) or enzyme_bench_n41 (the AME-41 enzyme benchmark)",
				"enum":        rfdiffusion2Benchmarks,
				"default":     "active_site_demo",
			},
			"motif_pdb": map[string]any{
				"type":        "string",
				"description": "Workspace path to a user catalytic motif .pdb; when set, overrides the benchmark's bundled motif via Hydra +inference.input_pdb and requires contigs",
			},
			"contigs": map[string]any{
				"type":        "string",
				"description": "Hydra-style contig string, e.g. '5-15,A10-30,5-15'; required when motif_pdb is set, ignored otherwise",
			},
			"num_designs": map[string]any{
				"type":        "integer",
				"description": "Number of backbones to generate (inference.num_designs)",
				"minimum":     1,
			},
			"seed": map[string]any{
				"type":        "integer",
				"description": "Random seed for reproducible runs",
				"minimum":     0,
			},
			"guidepost_xyz_as_design_bb": map[string]any{
				"type":        "boolean",
				"description": "Whether unindexed motif XYZ coordinates overwrite the matched backbone (inference.guidepost_xyz_as_design_bb)",
			},
			"idealize_sidechain_outputs": map[string]any{
				"type":        "boolean",
				"description": "Run the PyRosetta idealization pass on atomized sidechains post-diffusion (inference.idealize_sidechain_outputs)",
			},
			"stop_step": map[string]any{
				"type":        "string",
				"description": "Where to stop the pipeline — design (backbone + motif only) or end (full pipeline through Chai-1 fold; default)",
				"enum":        rfdiffusion2StopSteps,
				"default":     "end",
			},
		},
	}
}

// Design jobs are long and GPU-bound — always require user approval.
func (*rfdiffusion2Tool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*rfdiffusion2Tool) EstimatedCostUSD(json.RawMessage) float64        { return 5.0 }
func (*rfdiffusion2Tool) EstimatedDuration(json.RawMessage) time.Duration { return 60 * time.Minute }

// Execute is the A2 implementation — stubbed in A1 so the tool compiles
// against tools.Tool. A2 adds validation, path resolution, job submission, and
// the persist callback.
func (t *rfdiffusion2Tool) Execute(_ context.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{}, nil
}

// persist parses the backend's design-list output and writes each design to
// the store. A response with no "designs" array persists nothing. (Wired up by
// A2.)
func (t *rfdiffusion2Tool) persist(out []byte) (int, error) {
	var bo backendOutput
	if err := json.Unmarshal(out, &bo); err != nil {
		return 0, fmt.Errorf("design.rfdiffusion2 output is not valid JSON: %w", err)
	}
	for _, d := range bo.Designs {
		design := domain.Design{
			ID:            domain.DesignID("d_" + uuid.NewString()),
			ProjectID:     store.DefaultProjectID,
			Created:       time.Now().UTC(),
			Origin:        domain.OriginRFDiff2MPNN,
			Application:   domain.AppEnzyme,
			Sequence:      domain.Sequence{Chains: d.Sequence},
			StructureFile: d.StructureFile,
			Scores:        d.Scores,
			Provenance:    []domain.ToolCallRef{domain.NewToolCallRef("design.rfdiffusion2", nil)},
		}
		if err := t.store.InsertDesign(design); err != nil {
			return 0, err
		}
	}
	return len(bo.Designs), nil
}
