package design

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// BoltzGenParams is the agent-facing BoltzGen run configuration. It is an
// alias of domain.BoltzGenParams — the type lives in internal/domain so a
// DesignPlan can carry it without an import cycle, and design tools reference
// it here under the friendlier package-local name.
type BoltzGenParams = domain.BoltzGenParams

// boltzGenInput is the full design.boltzgen tool input: the workspace-relative
// path to the agent-authored spec YAML plus the run-config parameters. It is
// what the adapter receives (with spec_path resolved to an absolute path).
type boltzGenInput struct {
	SpecPath string `json:"spec_path"` // required: workspace-relative spec YAML
	BoltzGenParams
}

// boltzGenProtocols is the closed set of BoltzGen protocols (--protocol).
var boltzGenProtocols = []string{
	"protein-anything", "peptide-anything", "protein-small_molecule",
	"antibody-anything", "nanobody-anything", "protein-redesign",
}

// boltzGenSteps is the closed set of BoltzGen pipeline steps (--steps).
var boltzGenSteps = []string{
	"design", "inverse_folding", "design_folding", "folding",
	"affinity", "analysis", "filtering",
}

// boltzGenTool is the bespoke design.boltzgen tool. Unlike the shared
// designTool wrapper, it advertises BoltzGen's full run-config surface and
// consumes an agent-authored spec YAML rather than a generic target/hotspots
// request.
type boltzGenTool struct {
	mgr           *jobs.Manager
	backend       backends.Backend
	store         *store.Store
	workspaceRoot string
}

// NewBoltzGenTool builds the design.boltzgen tool — the SPECS-blessed binder
// method that runs on aarch64/Grace, where BindCraft (PyRosetta) is
// unavailable. workspaceRoot scopes the relative spec_path input.
//
// The signature is held stable so cmd/fova/main.go's registration line is
// unchanged across the bespoke-tool rework.
func NewBoltzGenTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *boltzGenTool {
	return &boltzGenTool{
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}

func (*boltzGenTool) Name() string { return "design.boltzgen" }

func (*boltzGenTool) Description() string {
	return "Design de novo protein binders against a target with BoltzGen " +
		"(runs as an async GPU job). Primary binder method on aarch64/Grace, " +
		"where BindCraft is unavailable. Requires a BoltzGen specification " +
		"YAML the agent authors first — see the boltzgen-spec skill — and " +
		"validates with design.boltzgen_check. Pass its workspace path as " +
		"spec_path."
}

// InputSchema advertises spec_path plus every BoltzGenParams field, with enum
// constraints on protocol and the steps entries.
func (*boltzGenTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spec_path": map[string]any{
				"type":        "string",
				"description": "Workspace-relative path to the BoltzGen specification YAML the agent authored and validated with design.boltzgen_check",
			},
			"protocol": map[string]any{
				"type":        "string",
				"description": "BoltzGen protocol — sets defaults and which pipeline steps run",
				"enum":        boltzGenProtocols,
				"default":     "protein-anything",
			},
			"num_designs": map[string]any{
				"type":        "integer",
				"description": "Number of intermediate designs to generate (10k-60k in practice)",
				"minimum":     1,
			},
			"budget": map[string]any{
				"type":        "integer",
				"description": "Size of the final diversity-optimized design set the filter step keeps",
				"minimum":     1,
			},
			"diffusion_batch_size": map[string]any{
				"type":        "integer",
				"description": "Diffusion samples per trunk run (optional; BoltzGen picks a default)",
				"minimum":     1,
			},
			"steps": map[string]any{
				"type":        "array",
				"description": "Optional subset of pipeline steps to run (default: all)",
				"items": map[string]any{
					"type": "string",
					"enum": boltzGenSteps,
				},
			},
			"alpha": map[string]any{
				"type":        "number",
				"description": "Sequence-diversity trade-off 0..1 (0=quality-only, 1=diversity-only)",
				"minimum":     0,
				"maximum":     1,
			},
			"filter_biased": map[string]any{
				"type":        "boolean",
				"description": "Remove amino-acid composition outliers in the filter step",
			},
			"additional_filters": map[string]any{
				"type":        "array",
				"description": "Extra hard filters in feature>threshold / feature<threshold form",
				"items":       map[string]any{"type": "string"},
			},
			"refolding_rmsd_threshold": map[string]any{
				"type":        "number",
				"description": "RMSD threshold for the refolding-based filters (lower is better)",
			},
			"inverse_fold_num_sequences": map[string]any{
				"type":        "integer",
				"description": "Sequences generated per backbone in the inverse-folding step",
				"minimum":     1,
			},
			"inverse_fold_avoid": map[string]any{
				"type":        "string",
				"description": "One-letter amino-acid codes disallowed during inverse folding (e.g. 'KEC')",
			},
			"step_scale": map[string]any{
				"type":        "number",
				"description": "Advanced: fixed diffusion step scale (default: a schedule)",
			},
			"noise_scale": map[string]any{
				"type":        "number",
				"description": "Advanced: fixed diffusion noise scale (default: a schedule)",
			},
			"reuse": map[string]any{
				"type":        "boolean",
				"description": "Resume an interrupted run, reusing existing per-step results",
			},
		},
		"required": []string{"spec_path"},
	}
}

// Design jobs are long and GPU-bound — always require user approval.
func (*boltzGenTool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*boltzGenTool) EstimatedCostUSD(json.RawMessage) float64        { return 5.0 }
func (*boltzGenTool) EstimatedDuration(json.RawMessage) time.Duration { return 45 * time.Minute }

// Execute validates the request, resolves spec_path against the workspace,
// submits a background job, and returns its ID immediately. The job runs the
// backend, parses the designs, and persists them.
func (t *boltzGenTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in boltzGenInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, fmt.Errorf("invalid design.boltzgen request: %w", err)
	}
	if strings.TrimSpace(in.SpecPath) == "" {
		return tools.Result{}, fmt.Errorf(
			"design.boltzgen: spec_path is required — author a BoltzGen specification " +
				"YAML (see the boltzgen-spec skill), validate it with design.boltzgen_check, " +
				"and pass its workspace path")
	}
	resolvedSpec, err := tools.ResolveWorkspacePath(t.workspaceRoot, in.SpecPath)
	if err != nil {
		return tools.Result{}, fmt.Errorf("design.boltzgen: spec_path: %w", err)
	}
	if resolvedSpec != "" {
		in.SpecPath = resolvedSpec
	}
	resolved, err := json.Marshal(in)
	if err != nil {
		return tools.Result{}, fmt.Errorf("design.boltzgen: %w", err)
	}
	jobID, err := t.mgr.Submit(jobs.Spec{
		Kind:    domain.JobCompute,
		Tool:    "design.boltzgen",
		Backend: t.backend.Name(),
		Input:   resolved,
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			out, err := t.backend.Run(ctx, "design.boltzgen", resolved, log, progress)
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
		Display: fmt.Sprintf("started design.boltzgen job %s — poll jobs.result for the designs",
			jobID),
		Provenance: domain.NewToolCallRef("design.boltzgen", input),
	}, nil
}

// persist parses the backend's design-list output and writes each design to
// the store. A response with no "designs" array persists nothing.
func (t *boltzGenTool) persist(out []byte) (int, error) {
	var bo backendOutput
	if err := json.Unmarshal(out, &bo); err != nil {
		return 0, fmt.Errorf("design.boltzgen output is not valid JSON: %w", err)
	}
	for _, d := range bo.Designs {
		design := domain.Design{
			ID:            domain.DesignID("d_" + uuid.NewString()),
			ProjectID:     store.DefaultProjectID,
			Created:       time.Now().UTC(),
			Origin:        domain.OriginBoltzGen,
			Application:   domain.AppBinder,
			Sequence:      domain.Sequence{Chains: d.Sequence},
			StructureFile: d.StructureFile,
			Scores:        d.Scores,
			Provenance:    []domain.ToolCallRef{domain.NewToolCallRef("design.boltzgen", nil)},
		}
		if err := t.store.InsertDesign(design); err != nil {
			return 0, err
		}
	}
	return len(bo.Designs), nil
}
