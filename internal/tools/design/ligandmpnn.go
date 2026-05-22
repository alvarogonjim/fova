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

// LigandMPNNParams is the agent-facing LigandMPNN run configuration. It is an
// alias of domain.LigandMPNNParams — the type lives in internal/domain so a
// DesignPlan can carry it without an import cycle, and design tools reference
// it here under the friendlier package-local name.
type LigandMPNNParams = domain.LigandMPNNParams

// ligandMPNNModelTypes is the closed set of LigandMPNN run.py model types,
// advertised as the model_type enum.
var ligandMPNNModelTypes = []string{
	"ligand_mpnn", "protein_mpnn", "soluble_mpnn",
	"global_label_membrane_mpnn", "per_residue_label_membrane_mpnn",
}

// ligandMPNNTool is the bespoke design.ligandmpnn tool. Unlike the shared
// designTool wrapper, it advertises LigandMPNN's full run-config surface — the
// five model types, residue selection, amino-acid bias, symmetry, the
// transmembrane labels, and side-chain packing.
type ligandMPNNTool struct {
	mgr           *jobs.Manager
	backend       backends.Backend
	store         *store.Store
	workspaceRoot string
}

// NewLigandMPNNTool builds the design.ligandmpnn tool — LigandMPNN sequence
// design for a fixed backbone, ligand-aware. workspaceRoot scopes the relative
// path inputs (pdb, the per-residue bias/omit JSON files).
//
// The signature is held stable so cmd/fova/main.go's registration line is
// unchanged across the bespoke-tool rework.
func NewLigandMPNNTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *ligandMPNNTool {
	return &ligandMPNNTool{
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}

func (*ligandMPNNTool) Name() string { return "design.ligandmpnn" }

func (*ligandMPNNTool) Description() string {
	return "Design protein sequences for a fixed backbone with LigandMPNN — " +
		"ligand-aware sequence design that conditions on bound small molecules, " +
		"nucleotides, and metals. Runs as an async GPU job. Supports the five " +
		"MPNN model types, per-residue redesign/fix selection, amino-acid bias " +
		"and omission, symmetry, transmembrane labels, and side-chain packing."
}

// InputSchema advertises every LigandMPNNParams field, with the model_type
// enum and minimums on the bounded numerics.
func (*ligandMPNNTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"model_type": map[string]any{
				"type":        "string",
				"description": "MPNN model to run — ligand_mpnn (ligand-bound active sites), protein_mpnn (general backbones), soluble_mpnn (soluble globular proteins), or a membrane model for transmembrane proteins",
				"enum":        ligandMPNNModelTypes,
				"default":     "ligand_mpnn",
			},
			"pdb": map[string]any{
				"type":        "string",
				"description": "Workspace path to the input backbone .pdb whose sequence is designed",
			},
			"num_designs": map[string]any{
				"type":        "integer",
				"description": "Number of design batches to generate (run.py --number_of_batches)",
				"minimum":     1,
			},
			"batch_size": map[string]any{
				"type":        "integer",
				"description": "Sequences sampled per batch",
				"minimum":     1,
			},
			"temperature": map[string]any{
				"type":        "number",
				"description": "Sampling temperature — lower is more conservative (typical 0.1-0.3)",
				"minimum":     0,
			},
			"seed": map[string]any{
				"type":        "integer",
				"description": "Random seed for reproducible sampling (optional)",
			},
			"redesigned_residues": map[string]any{
				"type":        "string",
				"description": "Space-separated residues to redesign, e.g. 'A23 A24 B42D'; all others are fixed",
			},
			"fixed_residues": map[string]any{
				"type":        "string",
				"description": "Space-separated residues to hold fixed, e.g. 'A1 A2'; all others are redesigned",
			},
			"chains_to_design": map[string]any{
				"type":        "string",
				"description": "Comma-separated chain ids to redesign, e.g. 'A,B' (whole chains)",
			},
			"bias_AA": map[string]any{
				"type":        "string",
				"description": "Global amino-acid bias as comma-separated <AA>:<weight>, e.g. 'W:3.0,P:-2.0'",
			},
			"omit_AA": map[string]any{
				"type":        "string",
				"description": "One-letter amino-acid codes to forbid everywhere, e.g. 'CG'",
			},
			"bias_AA_per_residue": map[string]any{
				"type":        "string",
				"description": "Workspace path to a JSON file of per-residue amino-acid bias",
			},
			"omit_AA_per_residue": map[string]any{
				"type":        "string",
				"description": "Workspace path to a JSON file of per-residue amino-acid omissions",
			},
			"ligand_use_atom_context": map[string]any{
				"type":        "boolean",
				"description": "Condition on ligand atom context (ligand_mpnn; default on)",
			},
			"ligand_use_side_chain_context": map[string]any{
				"type":        "boolean",
				"description": "Condition on fixed-residue side-chain atoms (ligand_mpnn)",
			},
			"ligand_cutoff": map[string]any{
				"type":        "number",
				"description": "Distance cutoff in angstroms for ligand context",
				"minimum":     0,
			},
			"symmetry_residues": map[string]any{
				"type":        "string",
				"description": "Pipe-separated groups of comma-separated residues tied to one sequence, e.g. 'A1,A2|A3,A4'",
			},
			"symmetry_weights": map[string]any{
				"type":        "string",
				"description": "Per-group averaging weights aligned with symmetry_residues",
			},
			"homo_oligomer": map[string]any{
				"type":        "boolean",
				"description": "Design all chains as one repeated homo-oligomer sequence",
			},
			"global_transmembrane_label": map[string]any{
				"type":        "integer",
				"description": "Whole-protein membrane label for global_label_membrane_mpnn — 0 (soluble) or 1 (transmembrane)",
				"minimum":     0,
				"maximum":     1,
			},
			"transmembrane_buried": map[string]any{
				"type":        "string",
				"description": "Space-separated residues buried in the membrane (per_residue_label_membrane_mpnn)",
			},
			"transmembrane_interface": map[string]any{
				"type":        "string",
				"description": "Space-separated residues at the membrane interface (per_residue_label_membrane_mpnn)",
			},
			"pack_side_chains": map[string]any{
				"type":        "boolean",
				"description": "Run the side-chain packing model on the designed sequences",
			},
			"number_of_packs_per_design": map[string]any{
				"type":        "integer",
				"description": "Side-chain packing samples per design",
				"minimum":     1,
			},
			"pack_with_ligand_context": map[string]any{
				"type":        "boolean",
				"description": "Include ligand context during side-chain packing",
			},
			"repack_everything": map[string]any{
				"type":        "boolean",
				"description": "Repack all residues, not just the redesigned ones",
			},
		},
		"required": []string{"pdb"},
	}
}

// Design jobs are long and GPU-bound — always require user approval.
func (*ligandMPNNTool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*ligandMPNNTool) EstimatedCostUSD(json.RawMessage) float64        { return 2.0 }
func (*ligandMPNNTool) EstimatedDuration(json.RawMessage) time.Duration { return 15 * time.Minute }

// Execute validates the request, resolves workspace paths, submits a
// background job, and returns its ID. The real body lands in Task A2.
func (t *ligandMPNNTool) Execute(_ context.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{}, nil
}
