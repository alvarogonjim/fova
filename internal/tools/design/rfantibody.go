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

// Execute validates the request, resolves the workspace path inputs, submits
// a background job, and returns its ID immediately. The job runs the backend,
// parses the designs, and persists them.
func (t *rfantibodyTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var params RFantibodyParams
	if err := json.Unmarshal(input, &params); err != nil {
		return tools.Result{}, fmt.Errorf("invalid design.rfantibody request: %w", err)
	}
	if err := params.Validate(); err != nil {
		return tools.Result{}, err
	}
	// Resolve every workspace-relative path input against the workspace root.
	if t.workspaceRoot != "" {
		for _, ref := range []*string{&params.Target, &params.FrameworkPDB} {
			if *ref == "" {
				continue
			}
			resolved, err := tools.ResolveWorkspacePath(t.workspaceRoot, *ref)
			if err != nil {
				return tools.Result{}, fmt.Errorf("design.rfantibody: %w", err)
			}
			if resolved != "" {
				*ref = resolved
			}
		}
	}
	resolved, err := json.Marshal(params)
	if err != nil {
		return tools.Result{}, fmt.Errorf("design.rfantibody: %w", err)
	}
	jobID, err := t.mgr.Submit(jobs.Spec{
		Kind:    domain.JobCompute,
		Tool:    "design.rfantibody",
		Backend: t.backend.Name(),
		Input:   resolved,
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			out, err := t.backend.Run(ctx, "design.rfantibody", resolved, log, progress)
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
		Display: fmt.Sprintf("started design.rfantibody job %s — poll jobs.result for the designs",
			jobID),
		Provenance: domain.NewToolCallRef("design.rfantibody", input),
	}, nil
}

// persist parses the backend's design-list output and writes each design to
// the store. A response with no "designs" array persists nothing.
func (t *rfantibodyTool) persist(out []byte) (int, error) {
	var bo backendOutput
	if err := json.Unmarshal(out, &bo); err != nil {
		return 0, fmt.Errorf("design.rfantibody output is not valid JSON: %w", err)
	}
	for _, d := range bo.Designs {
		design := domain.Design{
			ID:            domain.DesignID("d_" + uuid.NewString()),
			ProjectID:     store.DefaultProjectID,
			Created:       time.Now().UTC(),
			Origin:        domain.OriginRFAntibody,
			Application:   domain.AppAntibody,
			Sequence:      domain.Sequence{Chains: d.Sequence},
			StructureFile: d.StructureFile,
			Scores:        d.Scores,
			Provenance:    []domain.ToolCallRef{domain.NewToolCallRef("design.rfantibody", nil)},
		}
		if err := t.store.InsertDesign(design); err != nil {
			return 0, err
		}
	}
	return len(bo.Designs), nil
}
