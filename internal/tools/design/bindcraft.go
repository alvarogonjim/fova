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

// BindCraftParams is the agent-facing BindCraft run configuration. It is an
// alias of domain.BindCraftParams — the type lives in internal/domain so a
// DesignPlan can carry it without an import cycle, and design tools reference
// it here under the friendlier package-local name.
type BindCraftParams = domain.BindCraftParams

// bindCraftProtocolNames is the closed set of BindCraft protocol names fova
// advertises in the protocol_name enum.
var bindCraftProtocolNames = []string{"beta_only", "ss_only", "fixed_seq"}

// bindcraftTool is the bespoke design.bindcraft tool. Unlike the shared
// designTool wrapper, it advertises BindCraft's typed target-settings surface
// — the starting target, chain selection, hotspot epitope, binder length
// range, protocol choice, and the optional template / omit-AA knobs.
//
// BindCraft is x86-only (PyRosetta-based) — the Containerfile fails on
// aarch64 and the agent should not propose it on a non-x86 host.
type bindcraftTool struct {
	mgr           *jobs.Manager
	backend       backends.Backend
	store         *store.Store
	workspaceRoot string
}

// NewBindCraftTool builds the design.bindcraft tool — de novo protein binder
// design against a target with BindCraft. workspaceRoot scopes the relative
// path inputs (starting_pdb, template_pdb).
//
// The signature is held stable so cmd/fova/main.go's registration line is
// unchanged across the bespoke-tool rework.
func NewBindCraftTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *bindcraftTool {
	return &bindcraftTool{
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}

func (*bindcraftTool) Name() string { return "design.bindcraft" }

func (*bindcraftTool) Description() string {
	return "Design de novo protein binders against a target with BindCraft " +
		"(x86-only, PyRosetta-based); runs as an async GPU job. Takes a typed " +
		"target-settings configuration: the target PDB, target chain(s), " +
		"hotspot epitope residues, a binder length range, and optional " +
		"protocol / template / omit-AA knobs. fova compiles this into the " +
		"BindCraft settings.json (zero-value fields are omitted so BindCraft " +
		"applies its own defaults)."
}

// InputSchema advertises every BindCraftParams field with the protocol_name
// enum and minimums on the bounded numerics.
func (*bindcraftTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"binder_name": map[string]any{
				"type":        "string",
				"description": "Optional short name for the binder campaign (used as a prefix in BindCraft's output naming)",
			},
			"starting_pdb": map[string]any{
				"type":        "string",
				"description": "Workspace path to the target .pdb the binder is designed against",
			},
			"chains": map[string]any{
				"type":        "string",
				"description": "Target chain id(s) in the starting_pdb to design against, e.g. \"A\" or \"A,B\"",
			},
			"target_hotspot_residues": map[string]any{
				"type":        "string",
				"description": "Comma-separated epitope residues as chain+number, e.g. \"A30,A33,A47\" — the surface the binder is steered toward",
			},
			"length_min": map[string]any{
				"type":        "integer",
				"description": "Minimum binder length in residues (≥1)",
				"minimum":     1,
			},
			"length_max": map[string]any{
				"type":        "integer",
				"description": "Maximum binder length in residues (≥length_min)",
				"minimum":     1,
			},
			"number_of_final_designs": map[string]any{
				"type":        "integer",
				"description": "Target count of accepted final designs (BindCraft keeps running until it reaches this number or design_runs is exhausted)",
				"minimum":     0,
			},
			"binder_chain": map[string]any{
				"type":        "string",
				"description": "Chain id assigned to the designed binder in the output structures (default \"B\")",
			},
			"design_runs": map[string]any{
				"type":        "integer",
				"description": "Upper bound on total BindCraft design trajectories before stopping",
				"minimum":     0,
			},
			"protocol_name": map[string]any{
				"type":        "string",
				"description": "BindCraft protocol — beta_only (β-sheet biased), ss_only (secondary-structure constrained), or fixed_seq (sequence-pinned)",
				"enum":        bindCraftProtocolNames,
			},
			"template_pdb": map[string]any{
				"type":        "string",
				"description": "Optional workspace path to a binder template .pdb to seed the design trajectory",
			},
			"omit_aas": map[string]any{
				"type":        "string",
				"description": "One-letter amino-acid codes to forbid in the designed binder, e.g. \"C\" to avoid cysteines",
			},
		},
		"required": []string{
			"starting_pdb", "chains", "target_hotspot_residues",
			"length_min", "length_max",
		},
	}
}

// Design jobs are long and GPU-bound — always require user approval.
func (*bindcraftTool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*bindcraftTool) EstimatedCostUSD(json.RawMessage) float64        { return 5.0 }
func (*bindcraftTool) EstimatedDuration(json.RawMessage) time.Duration { return 60 * time.Minute }

// Execute validates the request, resolves the workspace path inputs, submits
// a background job, and returns its ID immediately. The job runs the backend,
// parses the designs, and persists them.
func (t *bindcraftTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var params BindCraftParams
	if err := json.Unmarshal(input, &params); err != nil {
		return tools.Result{}, fmt.Errorf("invalid design.bindcraft request: %w", err)
	}
	if err := params.Validate(); err != nil {
		return tools.Result{}, err
	}
	// Resolve every workspace-relative path input against the workspace root.
	if t.workspaceRoot != "" {
		for _, ref := range []*string{&params.StartingPDB, &params.TemplatePDB} {
			if *ref == "" {
				continue
			}
			resolved, err := tools.ResolveWorkspacePath(t.workspaceRoot, *ref)
			if err != nil {
				return tools.Result{}, fmt.Errorf("design.bindcraft: %w", err)
			}
			if resolved != "" {
				*ref = resolved
			}
		}
	}
	resolved, err := json.Marshal(params)
	if err != nil {
		return tools.Result{}, fmt.Errorf("design.bindcraft: %w", err)
	}
	jobID, err := t.mgr.Submit(jobs.Spec{
		Kind:    domain.JobCompute,
		Tool:    "design.bindcraft",
		Backend: t.backend.Name(),
		Input:   resolved,
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			out, err := t.backend.Run(ctx, "design.bindcraft", resolved, log, progress)
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
		Display: fmt.Sprintf("started design.bindcraft job %s — poll jobs.result for the designs",
			jobID),
		Provenance: domain.NewToolCallRef("design.bindcraft", input),
	}, nil
}

// persist parses the backend's design-list output and writes each design to
// the store. A response with no "designs" array persists nothing.
func (t *bindcraftTool) persist(out []byte) (int, error) {
	var bo backendOutput
	if err := json.Unmarshal(out, &bo); err != nil {
		return 0, fmt.Errorf("design.bindcraft output is not valid JSON: %w", err)
	}
	for _, d := range bo.Designs {
		design := domain.Design{
			ID:            domain.DesignID("d_" + uuid.NewString()),
			ProjectID:     store.DefaultProjectID,
			Created:       time.Now().UTC(),
			Origin:        domain.OriginBindCraft,
			Application:   domain.AppBinder,
			Sequence:      domain.Sequence{Chains: d.Sequence},
			StructureFile: d.StructureFile,
			Scores:        d.Scores,
			Provenance:    []domain.ToolCallRef{domain.NewToolCallRef("design.bindcraft", nil)},
		}
		if err := t.store.InsertDesign(design); err != nil {
			return 0, err
		}
	}
	return len(bo.Designs), nil
}
