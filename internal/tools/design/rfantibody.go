package design

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// RFantibodyParams is the agent-facing RFantibody run configuration. It is an
// alias of domain.RFantibodyParams — the type lives in internal/domain so a
// DesignPlan can carry it without an import cycle, and design tools reference
// it here under the friendlier package-local name.
type RFantibodyParams = domain.RFantibodyParams

// rfantibodyFrameworks is the closed set of bundled framework presets,
// advertised as the framework enum.
var rfantibodyFrameworks = []string{"nanobody", "scfv"}

// rfantibodyTool is the bespoke design.rfantibody tool. Unlike the shared
// designTool wrapper, it advertises RFantibody's full 3-stage run-config
// surface — the framework choice, per-CDR loop specs, and the rfdiffusion /
// proteinmpnn / rf2 parameters that drive the antibody-design pipeline.
type rfantibodyTool struct {
	mgr           *jobs.Manager
	backend       backends.Backend
	store         *store.Store
	workspaceRoot string
}

// NewRFAntibodyTool builds the design.rfantibody tool — structure-based de novo
// antibody / nanobody design against a target with RFantibody. workspaceRoot
// scopes the relative path inputs (target, framework_pdb).
//
// The signature is held stable so cmd/fova/main.go's registration line is
// unchanged across the bespoke-tool rework.
func NewRFAntibodyTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *rfantibodyTool {
	return &rfantibodyTool{
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}

func (*rfantibodyTool) Name() string { return "design.rfantibody" }

func (*rfantibodyTool) Description() string {
	return "Design de novo antibodies / nanobodies against a target with " +
		"RFantibody — structure-based design driving the full 3-stage pipeline " +
		"(rfdiffusion backbone generation → proteinmpnn CDR-loop sequence design " +
		"→ rf2 structure prediction and confidence scoring). Runs as an async " +
		"GPU job. Supports the nanobody / scFv framework choice, a user " +
		"HLT-format framework PDB, per-CDR loop-length specs, and the per-stage " +
		"design parameters."
}

// InputSchema advertises every RFantibodyParams field, with the framework enum
// and minimums on the bounded numerics.
func (*rfantibodyTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "Workspace path to the antigen .pdb — ideally truncated to ~50-60 residues around the epitope",
			},
			"hotspots": map[string]any{
				"type":        "string",
				"description": "Comma-separated epitope residues as chain+number, e.g. 'T305,T456' — RFantibody is sensitive to hotspot choice",
			},
			"framework": map[string]any{
				"type":        "string",
				"description": "Bundled in-container framework preset — nanobody (single VHH domain) or scfv (paired heavy/light Fv)",
				"enum":        rfantibodyFrameworks,
				"default":     "nanobody",
			},
			"framework_pdb": map[string]any{
				"type":        "string",
				"description": "Workspace path to a user HLT-format framework PDB (chains H, L, T with CDR remarks); overrides framework when set",
			},
			"design_loops": map[string]any{
				"type":        "string",
				"description": "Per-CDR loop-length spec, comma-separated <CDR>:<spec> where spec is a length or <min>-<max> range, e.g. 'H1:7,H3:5-13,L3:9-11'; empty uses RFantibody defaults",
			},
			"num_designs": map[string]any{
				"type":        "integer",
				"description": "Number of antibody-target backbones to generate (rfdiffusion -n)",
				"minimum":     1,
			},
			"deterministic": map[string]any{
				"type":        "boolean",
				"description": "Run rfdiffusion and proteinmpnn deterministically for reproducible designs",
			},
			"seqs_per_struct": map[string]any{
				"type":        "integer",
				"description": "Number of CDR-loop sequences proteinmpnn designs per backbone (proteinmpnn -n)",
				"minimum":     1,
			},
			"temperature": map[string]any{
				"type":        "number",
				"description": "proteinmpnn sampling temperature — lower is more conservative (proteinmpnn -t)",
				"minimum":     0,
			},
			"num_recycles": map[string]any{
				"type":        "integer",
				"description": "rf2 structure-prediction recycle iterations (rf2 -r)",
				"minimum":     1,
			},
			"seed": map[string]any{
				"type":        "integer",
				"description": "rf2 random seed for reproducible structure prediction (rf2 -s)",
				"minimum":     0,
			},
			"hotspot_show_prop": map[string]any{
				"type":        "number",
				"description": "Proportion of hotspot residues revealed to rf2 during prediction, in [0,1] (rf2 --hotspot-show-prop)",
				"minimum":     0,
				"maximum":     1,
			},
		},
		"required": []string{"target", "hotspots"},
	}
}

// Design jobs are long and GPU-bound — always require user approval.
func (*rfantibodyTool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*rfantibodyTool) EstimatedCostUSD(json.RawMessage) float64        { return 5.0 }
func (*rfantibodyTool) EstimatedDuration(json.RawMessage) time.Duration { return 60 * time.Minute }

// Execute is the design.rfantibody entry point. Task A2 fills in validation,
// workspace-path resolution, job submission, and design persistence.
func (t *rfantibodyTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	return tools.Result{}, nil
}
