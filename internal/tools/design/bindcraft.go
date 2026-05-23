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

// Execute is a stub at this commit (C1). The real body — validate, resolve
// paths, submit, persist — lands in C2.
func (t *bindcraftTool) Execute(_ context.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{}, nil
}
