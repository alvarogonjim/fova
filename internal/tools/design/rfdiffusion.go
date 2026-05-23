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

// RFdiffusionParams is the agent-facing RFdiffusion run configuration. It is
// an alias of domain.RFdiffusionParams — the type lives in internal/domain so
// a DesignPlan's MethodConfig can carry it without an import cycle, and design
// tools reference it here under the friendlier package-local name.
type RFdiffusionParams = domain.RFdiffusionParams

// rfdiffusionSymmetryKindsEnum is the closed set of RFdiffusion symmetry
// kinds, advertised as the symmetry_kind enum.
var rfdiffusionSymmetryKindsEnum = []string{
	"cyclic", "dihedral", "tetrahedral", "octahedral", "icosahedral",
}

// rfdiffusionTool is the bespoke design.rfdiffusion tool. Unlike the shared
// designTool wrapper, it advertises RFdiffusion's full Hydra-override surface
// — the target/hotspots, contigs, deterministic + symmetric flags, the
// partial-diffusion start step, noise scales, and guiding potentials.
type rfdiffusionTool struct {
	mgr           *jobs.Manager
	backend       backends.Backend
	store         *store.Store
	workspaceRoot string
}

// NewRFdiffusionTool builds the design.rfdiffusion tool — RFdiffusion v1
// backbone generation against a target or unconditionally. workspaceRoot
// scopes the relative path inputs (the target PDB).
//
// The signature is held stable so cmd/fova/main.go's registration line is
// unchanged across the bespoke-tool rework.
func NewRFdiffusionTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *rfdiffusionTool {
	return &rfdiffusionTool{
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}

func (*rfdiffusionTool) Name() string { return "design.rfdiffusion" }

func (*rfdiffusionTool) Description() string {
	return "Generate de novo protein backbones with RFdiffusion against a target or unconditionally; runs as an async GPU job."
}

// InputSchema advertises every RFdiffusionParams field, with the symmetry_kind
// enum and minimums on the bounded numerics. RFdiffusion emits backbones only
// — designs come back with empty scores, so the agent must refold (e.g.
// fold.boltz2) to rank them.
func (*rfdiffusionTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "Workspace path to the target .pdb (binder-against-target mode). Leave empty for unconditional backbone generation.",
			},
			"hotspots": map[string]any{
				"type":        "string",
				"description": "Comma-separated target hotspot residues, e.g. 'A30,A33' — chain id + residue number per token.",
			},
			"contigs": map[string]any{
				"type":        "string",
				"description": "RFdiffusion contig map (required). Examples: 'A1-100/0 60-80' for a 60-80 residue binder against chain A of the target; '50-100' for an unconditional 50-100 residue chain.",
			},
			"num_designs": map[string]any{
				"type":        "integer",
				"description": "Number of backbones to sample (inference.num_designs).",
				"minimum":     1,
			},
			"deterministic": map[string]any{
				"type":        "boolean",
				"description": "Make sampling deterministic (inference.deterministic).",
			},
			"symmetric": map[string]any{
				"type":        "boolean",
				"description": "Generate a symmetric oligomer (inference.symmetric). Requires symmetry_kind and n_chains.",
			},
			"symmetry_kind": map[string]any{
				"type":        "string",
				"description": "Symmetry to enforce when symmetric is true.",
				"enum":        rfdiffusionSymmetryKindsEnum,
			},
			"n_chains": map[string]any{
				"type":        "integer",
				"description": "Number of chains in the symmetric assembly (symmetry.n_chains).",
				"minimum":     1,
			},
			"partial_t": map[string]any{
				"type":        "integer",
				"description": "Partial-diffusion start step (diffuser.partial_T) — re-diffuse from a starting motif instead of pure noise.",
				"minimum":     0,
			},
			"noise_scale_ca": map[string]any{
				"type":        "number",
				"description": "Cα noise scale (diffuser.noise_scale_ca). Lower → tighter samples.",
				"minimum":     0,
			},
			"noise_scale_frame": map[string]any{
				"type":        "number",
				"description": "Frame noise scale (diffuser.noise_scale_frame). Lower → tighter samples.",
				"minimum":     0,
			},
			"guiding_potentials": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Guiding-potential expressions (potentials.guiding_potentials), e.g. ['binder_ROG', 'interface_ncontacts'].",
			},
			"guide_scale": map[string]any{
				"type":        "number",
				"description": "Strength applied to the guiding potentials (potentials.guide_scale).",
				"minimum":     0,
			},
		},
		"required": []string{"contigs"},
	}
}

// Design jobs are long and GPU-bound — always require user approval.
func (*rfdiffusionTool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*rfdiffusionTool) EstimatedCostUSD(json.RawMessage) float64        { return 2.0 }
func (*rfdiffusionTool) EstimatedDuration(json.RawMessage) time.Duration { return 20 * time.Minute }

// Execute is a stub at A1 — the real validate/resolve/submit/persist body
// lands in A2.
func (*rfdiffusionTool) Execute(_ context.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{}, nil
}
