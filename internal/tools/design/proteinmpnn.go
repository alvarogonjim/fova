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

// ProteinMPNNParams is the agent-facing ProteinMPNN run configuration. It is
// an alias of domain.ProteinMPNNParams — the type lives in internal/domain so a
// DesignPlan can carry it without an import cycle, and design tools reference
// it here under the friendlier package-local name.
type ProteinMPNNParams = domain.ProteinMPNNParams

// proteinMPNNTool is the bespoke design.proteinmpnn tool. Unlike the shared
// designTool wrapper, it advertises ProteinMPNN's full run-config surface —
// chain/position constraints, amino-acid bias/omission, tied positions, and
// the optional per-design score dump.
type proteinMPNNTool struct {
	mgr           *jobs.Manager
	backend       backends.Backend
	store         *store.Store
	workspaceRoot string
}

// NewProteinMPNNTool builds the design.proteinmpnn tool — sequence design for a
// fixed backbone with ProteinMPNN. workspaceRoot scopes the relative path
// inputs (pdb, the per-position/bias/tied JSONL files).
//
// The signature is held stable so cmd/fova/main.go's registration line is
// unchanged across the bespoke-tool rework.
func NewProteinMPNNTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *proteinMPNNTool {
	return &proteinMPNNTool{
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}

func (*proteinMPNNTool) Name() string { return "design.proteinmpnn" }

func (*proteinMPNNTool) Description() string {
	return "Design protein sequences for a fixed backbone with ProteinMPNN; runs as an async GPU job."
}

// InputSchema advertises every ProteinMPNNParams field, with minimums on the
// bounded numerics and a description on every property.
func (*proteinMPNNTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pdb": map[string]any{
				"type":        "string",
				"description": "Workspace path to the input backbone .pdb whose sequence is designed",
			},
			"num_designs": map[string]any{
				"type":        "integer",
				"description": "Number of sequences sampled per target (protein_mpnn_run.py --num_seq_per_target); defaults to 1",
				"minimum":     0,
			},
			"batch_size": map[string]any{
				"type":        "integer",
				"description": "Sequences sampled per batch (--batch_size); defaults to 1",
				"minimum":     0,
			},
			"sampling_temp": map[string]any{
				"type":        "number",
				"description": "Sampling temperature (--sampling_temp); lower is more conservative. Defaults to 0.1.",
				"minimum":     0,
			},
			"seed": map[string]any{
				"type":        "integer",
				"description": "Random seed (--seed); defaults to 37 when unset",
				"minimum":     0,
			},
			"chains_to_design": map[string]any{
				"type":        "string",
				"description": "Comma-separated chain ids to redesign, e.g. 'A,B'; fova generates the --chain_id_jsonl",
			},
			"fixed_positions": map[string]any{
				"type":        "string",
				"description": "Workspace path to a fixed-positions JSONL (--fixed_positions_jsonl)",
			},
			"omit_AAs": map[string]any{
				"type":        "string",
				"description": "One-letter amino-acid codes to forbid everywhere, e.g. 'CG' (--omit_AAs); inline string",
			},
			"bias_AA": map[string]any{
				"type":        "string",
				"description": "Workspace path to a JSONL — file, not inline (unlike LigandMPNN's bias_AA). Passed as --bias_AA_jsonl.",
			},
			"bias_by_residue": map[string]any{
				"type":        "string",
				"description": "Workspace path to a per-residue bias JSONL (--bias_by_res_jsonl)",
			},
			"tied_positions": map[string]any{
				"type":        "string",
				"description": "Workspace path to a tied-positions JSONL (--tied_positions_jsonl)",
			},
			"save_score": map[string]any{
				"type":        "boolean",
				"description": "When true, append --save_score 1 so per-design scores are written alongside the sequences",
			},
		},
		"required": []string{"pdb"},
	}
}

// Design jobs are long and GPU-bound — always require user approval.
func (*proteinMPNNTool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*proteinMPNNTool) EstimatedCostUSD(json.RawMessage) float64        { return 1.0 }
func (*proteinMPNNTool) EstimatedDuration(json.RawMessage) time.Duration { return 10 * time.Minute }

// Execute is implemented in task B2.
func (t *proteinMPNNTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	_ = input
	return tools.Result{}, nil
}
