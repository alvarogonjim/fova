package design

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

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

// Execute validates the request, resolves the workspace path inputs, submits
// a background job, and returns its ID immediately. The job runs the backend,
// parses the backbones, and persists them (with empty scores — RFdiffusion
// emits no native scores, so the agent must refold to rank).
func (t *rfdiffusionTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var params RFdiffusionParams
	if err := json.Unmarshal(input, &params); err != nil {
		return tools.Result{}, fmt.Errorf("invalid design.rfdiffusion request: %w", err)
	}
	if err := params.Validate(); err != nil {
		return tools.Result{}, err
	}
	// Resolve every workspace-relative path input against the workspace root.
	if t.workspaceRoot != "" && params.Target != "" {
		resolved, err := tools.ResolveWorkspacePath(t.workspaceRoot, params.Target)
		if err != nil {
			return tools.Result{}, fmt.Errorf("design.rfdiffusion: %w", err)
		}
		if resolved != "" {
			params.Target = resolved
		}
	}
	resolved, err := json.Marshal(params)
	if err != nil {
		return tools.Result{}, fmt.Errorf("design.rfdiffusion: %w", err)
	}
	jobID, err := t.mgr.Submit(jobs.Spec{
		Kind:    domain.JobCompute,
		Tool:    "design.rfdiffusion",
		Backend: t.backend.Name(),
		Input:   resolved,
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			out, err := t.backend.Run(ctx, "design.rfdiffusion", resolved, log, progress)
			if err != nil {
				return nil, err
			}
			progress(0.95)
			if _, perr := t.persist(out); perr != nil {
				return out, perr
			}
			return out, nil
		},
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		JobID: jobID,
		Display: fmt.Sprintf("started design.rfdiffusion job %s — poll jobs.result for the backbones",
			jobID),
		Provenance: domain.NewToolCallRef("design.rfdiffusion", input),
	}, nil
}

// persist parses the backend's design-list output and writes each backbone to
// the store. RFdiffusion designs land with empty Scores — the grounding skill
// tells the agent to refold (fold.boltz2 / fold.chai1) for ranking.
func (t *rfdiffusionTool) persist(out []byte) (int, error) {
	var bo backendOutput
	if err := json.Unmarshal(out, &bo); err != nil {
		return 0, fmt.Errorf("design.rfdiffusion output is not valid JSON: %w", err)
	}
	for _, d := range bo.Designs {
		design := domain.Design{
			ID:            domain.DesignID("d_" + uuid.NewString()),
			ProjectID:     store.DefaultProjectID,
			Created:       time.Now().UTC(),
			Origin:        domain.OriginRFDiffMPNN,
			Application:   domain.AppBinder,
			Sequence:      domain.Sequence{Chains: d.Sequence},
			StructureFile: d.StructureFile,
			Scores:        d.Scores,
			Provenance:    []domain.ToolCallRef{domain.NewToolCallRef("design.rfdiffusion", nil)},
		}
		if err := t.store.InsertDesign(design); err != nil {
			return 0, err
		}
	}
	return len(bo.Designs), nil
}
